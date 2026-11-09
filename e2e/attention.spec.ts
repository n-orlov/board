import { DatabaseSync } from 'node:sqlite';
import { join } from 'node:path';
import { expect, test, type APIRequestContext, type Page, type PlaywrightTestArgs } from '@playwright/test';
import { SECTIONS } from '../web/src/lib/attention';

// The Attention view (PRD R9, SPEC 5.3 and 11): five sections in order with their counts and empty states, items with
// their actors and a button that copies the actor's way back, `#/` landing on it, the tag and initiative filters in the
// fragment's query, and a change made through the API showing up without a reload. The board is shared with the other
// specs, so every assertion is scoped to an initiative this test made (the view's own initiative filter does the scoping).
const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const FIVE_SECONDS = { timeout: 5000 };

async function apiAs(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, actor: string, wayBack?: string) {
  return playwright.request.newContext({
    baseURL,
    extraHTTPHeaders: {
      Authorization: `Bearer ${process.env.E2E_API_KEY}`,
      'Board-Actor': actor,
      ...(wayBack === undefined ? {} : { 'Board-Actor-Way-Back': wayBack }),
    },
  });
}

async function post<T = Record<string, never>>(api: APIRequestContext, path: string, data: object, status: number): Promise<T> {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
  return (await response.json()) as T;
}

// Runs a lease out the way the Go scenarios do: its expiry moves into the past in the e2e store, then an API read expires it.
async function expireLease(api: APIRequestContext, ref: string) {
  const db = new DatabaseSync(join(process.env.E2E_WORK!, 'board.db'));
  try {
    db.exec('PRAGMA busy_timeout = 5000');
    const changed = db.prepare('UPDATE leases SET expires_at = ? WHERE n = ?').run(Date.now() - 30_000, Number(ref.slice(1)));
    expect(changed.changes).toBe(1);
  } finally {
    db.close();
  }
  expect((await api.get('/api/v1/leases?state=expired')).status()).toBe(200);
}

interface Seed {
  slug: string;
  tag: string;
  wayBack: string;
  asker: string;
  other: string;
  resource: string;
  ask: string;
  doneTask: string;
  expiredLease: string;
  watchTask: string;
  conflictTasks: string[];
}

// One initiative with one item in each of the five sections, made by named actors with a way back.
async function seed(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, name: string): Promise<Seed> {
  const slug = `e2e-att-${name}`;
  const tag = `att-${name}`;
  const asker = `asker-${name}`;
  const other = `holder-${name}`;
  const wayBack = `ralphctl status ${asker}`;
  const resource = `repo:att-${name}`;
  const a = await apiAs(playwright, baseURL, asker, wayBack);
  const b = await apiAs(playwright, baseURL, other, `ralphctl status ${other}`);
  await post(a, '/api/v1/initiatives', { slug, title: `Attention ${name}`, tags: [tag] }, 201);
  const task = async (title: string, extra: object = {}) => (await post<{ ref: string }>(a, '/api/v1/tasks', { initiative: slug, title, ...extra }, 201)).ref;

  const askTask = await task('Needs an answer');
  const ask = (await post<{ ref: string }>(a, '/api/v1/asks', { question: `Which way for ${name}?`, task: askTask, mode: 'poll', blocking: true }, 201)).ref;

  const doneTask = await task('Finished work');
  await post(a, `/api/v1/tasks/${doneTask}/done`, {}, 200);

  const leaseTask = await task('Ran out of time');
  const lease = await post<{ lease: { ref: string } }>(a, '/api/v1/leases', { task: leaseTask, intent: 'long edit', resources: [`repo:old-${name}`] }, 201);
  await expireLease(a, lease.lease.ref);

  const watchTask = await task('Slow watch', { kind: 'watch', cadence_seconds: 900, next_check_at: '2020-01-01T00:00:00Z' });

  const first = await task('Conflict one');
  const second = await task('Conflict two');
  await post(a, '/api/v1/leases', { task: first, intent: 'first edit', resources: [resource] }, 201);
  await post(b, '/api/v1/leases', { task: second, intent: 'second edit', resources: [resource] }, 201);
  await a.dispose();
  await b.dispose();
  return { slug, tag, wayBack, asker, other, resource, ask, doneTask, expiredLease: lease.lease.ref, watchTask, conflictTasks: [first, second] };
}

const section = (page: Page, key: string) => page.getByTestId(`section-${key}`);

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('the five sections come in order with their counts, items, actors and way backs', async ({ page, playwright, baseURL }) => {
      const s = await seed(playwright, baseURL, `five-${vp.name}`);
      await page.goto(`/#/attention?initiative=${s.slug}`);
      await expect(page.getByRole('heading', { level: 2, name: 'Attention' })).toBeVisible();

      await expect(page.getByRole('heading', { level: 3 })).toHaveText(SECTIONS.map((x) => new RegExp(`^${x.title}`)), FIVE_SECONDS);
      for (const key of ['open_asks', 'done_not_closed', 'expired_leases', 'overdue_watches', 'conflicts']) {
        await expect(section(page, key).getByTestId('section-count')).toHaveText('1', FIVE_SECONDS);
        await expect(section(page, key).getByTestId('section-empty')).toHaveCount(0);
        await expect(section(page, key).getByTestId('attention-item')).toHaveCount(1);
      }

      const asks = section(page, 'open_asks').getByTestId('attention-item');
      await expect(asks).toContainText(s.ask);
      await expect(asks).toContainText(`Which way for five-${vp.name}?`);
      await expect(asks).toContainText('asker not waiting');
      await expect(asks).toContainText('blocking');
      await expect(asks.getByTestId('actor-handle')).toHaveText(s.asker);
      await expect(asks.getByTestId('actor-way-back')).toHaveText(s.wayBack);
      await expect(asks.getByTestId('item-link')).toHaveAttribute('href', /^#\/t\//);

      const done = section(page, 'done_not_closed').getByTestId('attention-item');
      await expect(done).toContainText('Finished work');
      await expect(done.getByTestId('item-link')).toHaveAttribute('href', `#/t/${s.doneTask}`);
      await expect(done.getByTestId('actor-handle').first()).toHaveText(s.asker);

      const lease = section(page, 'expired_leases').getByTestId('attention-item');
      await expect(lease).toContainText(s.expiredLease);
      await expect(lease).toContainText('long edit');
      await expect(lease.getByTestId('actor-handle')).toHaveText(s.asker);

      const watch = section(page, 'overdue_watches').getByTestId('attention-item');
      await expect(watch).toContainText('Slow watch');
      await expect(watch.getByTestId('item-link')).toHaveAttribute('href', `#/t/${s.watchTask}`);
      await expect(watch.getByTestId('actor-handle')).toHaveText(s.asker);

      const conflict = section(page, 'conflicts').getByTestId('attention-item');
      await expect(conflict).toContainText(s.resource);
      await expect(conflict.getByTestId('actor-handle')).toHaveText([s.asker, s.other]);
      await expect(conflict.getByTestId('copy-way-back')).toHaveCount(2);

      expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
    });

    test('a filter that keeps nothing shows the five empty states and zero counts', async ({ page, playwright, baseURL }) => {
      const api = await apiAs(playwright, baseURL, `quiet-${vp.name}`);
      const slug = `e2e-att-quiet-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug, title: 'Nothing to see' }, 201);
      await api.dispose();
      await page.goto(`/#/attention?initiative=${slug}`);
      await expect(page.getByTestId('section-empty')).toHaveCount(5, FIVE_SECONDS);
      await expect(page.getByTestId('section-empty')).toHaveText(SECTIONS.map((x) => x.empty));
      await expect(page.getByTestId('section-count')).toHaveText(['0', '0', '0', '0', '0']);
      await expect(page.getByTestId('attention-item')).toHaveCount(0);
    });

    test('copy way back puts the actor\'s way back on the clipboard', async ({ page, context, playwright, baseURL }) => {
      await context.grantPermissions(['clipboard-read', 'clipboard-write']);
      const s = await seed(playwright, baseURL, `copy-${vp.name}`);
      await page.goto(`/#/attention?initiative=${s.slug}`);
      const ask = section(page, 'open_asks').getByTestId('attention-item');
      await expect(ask).toBeVisible(FIVE_SECONDS);
      await ask.getByRole('button', { name: `Copy way back of ${s.asker}` }).click();
      await expect(page.getByTestId('toast')).toHaveText(`Copied the way back of ${s.asker}`, FIVE_SECONDS);
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(s.wayBack);
    });

    test('the tag and initiative filters live in the fragment and survive a reload', async ({ page, playwright, baseURL }) => {
      const s = await seed(playwright, baseURL, `filt-${vp.name}`);
      await page.goto('/#/attention');
      const tag = page.getByRole('button', { name: s.tag, exact: true });
      await expect(tag).toBeVisible(FIVE_SECONDS);
      await tag.click();
      await expect(page).toHaveURL(new RegExp(`#/attention\\?tag=${s.tag}$`));
      await expect(tag).toHaveAttribute('aria-pressed', 'true');
      await expect(section(page, 'open_asks').getByTestId('attention-item')).toHaveCount(1, FIVE_SECONDS);

      await page.getByRole('button', { name: s.slug, exact: true }).click();
      await expect(page).toHaveURL(new RegExp(`#/attention\\?tag=${s.tag}&initiative=${s.slug}$`));

      await page.reload();
      await expect(page.getByRole('button', { name: s.tag, exact: true })).toHaveAttribute('aria-pressed', 'true', FIVE_SECONDS);
      await expect(page.getByRole('button', { name: s.slug, exact: true })).toHaveAttribute('aria-pressed', 'true');
      await expect(page).toHaveURL(new RegExp(`#/attention\\?tag=${s.tag}&initiative=${s.slug}$`));
      await expect(section(page, 'open_asks').getByTestId('attention-item')).toHaveCount(1, FIVE_SECONDS);
      await expect(section(page, 'conflicts').getByTestId('attention-item')).toHaveCount(1);

      await page.getByTestId('clear-filters').click();
      await expect(page).toHaveURL(/#\/attention$/);
      await expect(page.getByRole('button', { name: s.tag, exact: true })).toHaveAttribute('aria-pressed', 'false');
    });

    for (const entry of ['/', '/#/']) {
      test(`${entry} opens the Attention view`, async ({ page }) => {
        await page.goto(entry);
        await expect(page.getByRole('heading', { level: 2, name: 'Attention' })).toBeVisible();
        await expect(page.getByTestId('attention-view')).toBeVisible();
        const nav = page.getByRole('navigation', { name: 'Main' }).locator('visible=true');
        await expect(nav.getByRole('link', { name: 'Attention' })).toHaveAttribute('aria-current', 'page');
        await expect(nav.getByRole('link').first()).toHaveText('Attention');
      });
    }

    test('a change through the API shows up within 5 s without a reload', async ({ page, playwright, baseURL }) => {
      const api = await apiAs(playwright, baseURL, `live-${vp.name}`, `ralphctl status live-${vp.name}`);
      const slug = `e2e-att-live-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug, title: 'Live attention' }, 201);
      await page.goto(`/#/attention?initiative=${slug}`);
      const asks = section(page, 'open_asks');
      await expect(asks.getByTestId('section-empty')).toBeVisible(FIVE_SECONDS);
      await expect(asks.getByTestId('section-count')).toHaveText('0');

      const ask = await post<{ ref: string }>(api, '/api/v1/asks', { question: 'Raised while you watch', initiative: slug }, 201);
      await expect(asks.getByTestId('attention-item')).toContainText(ask.ref, FIVE_SECONDS);
      await expect(asks.getByTestId('section-count')).toHaveText('1', FIVE_SECONDS);

      await post(api, `/api/v1/asks/${ask.ref}/answer`, { answer: 'Left' }, 200);
      await expect(asks.getByTestId('attention-item')).toHaveCount(0, FIVE_SECONDS);
      await expect(asks.getByTestId('section-empty')).toBeVisible(FIVE_SECONDS);
      await expect(asks.getByTestId('section-count')).toHaveText('0', FIVE_SECONDS);
      await api.dispose();
    });
  });
}
