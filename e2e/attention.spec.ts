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


interface Item {
  slug: string;
  api: APIRequestContext;
}

// A fresh initiative of this test's own, filtered to by the view, and an API client of its own actor.
async function ground(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, name: string): Promise<Item> {
  const slug = `e2e-att-act-${name}`;
  const api = await apiAs(playwright, baseURL, `act-${name}`, `ralphctl status act-${name}`);
  await post(api, '/api/v1/initiatives', { slug, title: `Actions ${name}` }, 201);
  return { slug, api };
}

const dialog = (page: Page) => page.getByTestId('dialog');

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

    test('Answer with an option button fills the answer, sends it and the ask leaves its section', async ({ page, playwright, baseURL }) => {
      const { slug, api } = await ground(playwright, baseURL, `opt-${vp.name}`);
      const task = (await post<{ ref: string }>(api, '/api/v1/tasks', { initiative: slug, title: 'Choosing' }, 201)).ref;
      const ask = await post<{ ref: string }>(
        api,
        '/api/v1/asks',
        { question: 'Left or right?', details: 'Pick **one** <script>window.pwned = 1</script>', options: ['Left', 'Right'], task },
        201,
      );
      await page.goto(`/#/attention?initiative=${slug}`);
      const item = section(page, 'open_asks').getByTestId('attention-item');
      await expect(item).toContainText(ask.ref, FIVE_SECONDS);
      await expect(item.getByTestId('item-link')).toHaveAttribute('href', `#/t/${task}`);
      await expect(item.getByTestId('initiative-link')).toHaveAttribute('href', `#/i/${slug}`);

      await item.getByRole('button', { name: `Answer ${ask.ref}` }).click();
      await expect(dialog(page)).toBeVisible();
      await expect(page.getByTestId('answer-question')).toHaveText(`${ask.ref}: Left or right?`);
      await expect(page.getByTestId('answer-details').locator('strong')).toHaveText('one');
      await expect(page.getByTestId('answer-details')).toContainText('<script>window.pwned = 1</script>');
      expect(await page.evaluate(() => (window as unknown as { pwned?: number }).pwned)).toBeUndefined();
      await expect(page.getByTestId('answer-option')).toHaveText(['Left', 'Right']);

      await page.getByTestId('answer-option').filter({ hasText: 'Right' }).click();
      await expect(dialog(page).getByRole('textbox', { name: 'Answer' })).toHaveValue('Right');
      await dialog(page).getByRole('button', { name: 'Send answer' }).click();

      await expect(page.getByTestId('toast')).toHaveText(`Answered ${ask.ref}`, FIVE_SECONDS);
      await expect(dialog(page)).toHaveCount(0);
      await expect(section(page, 'open_asks').getByTestId('attention-item')).toHaveCount(0, FIVE_SECONDS);
      await expect(section(page, 'open_asks').getByTestId('section-count')).toHaveText('0', FIVE_SECONDS);
      const got = await api.get(`/api/v1/asks/${ask.ref}`);
      expect(await got.json()).toMatchObject({ state: 'answered', answer: 'Right', channel: 'ui' });
      await api.dispose();
    });

    test('Answer with free text sends what was typed', async ({ page, playwright, baseURL }) => {
      const { slug, api } = await ground(playwright, baseURL, `free-${vp.name}`);
      const ask = await post<{ ref: string }>(api, '/api/v1/asks', { question: 'What now?', initiative: slug }, 201);
      await page.goto(`/#/attention?initiative=${slug}`);
      const item = section(page, 'open_asks').getByTestId('attention-item');
      await item.getByRole('button', { name: `Answer ${ask.ref}` }).click();
      await expect(dialog(page)).toBeVisible(FIVE_SECONDS);
      await expect(page.getByTestId('answer-option')).toHaveCount(0);
      await expect(page.getByTestId('answer-details')).toHaveCount(0);
      await expect(page.getByTestId('answer-initiative')).toHaveAttribute('href', `#/i/${slug}`);
      await dialog(page).getByRole('textbox', { name: 'Answer' }).fill('Ship it on Friday');
      await dialog(page).getByRole('button', { name: 'Send answer' }).click();
      await expect(page.getByTestId('toast')).toHaveText(`Answered ${ask.ref}`, FIVE_SECONDS);
      await expect(section(page, 'open_asks').getByTestId('attention-item')).toHaveCount(0, FIVE_SECONDS);
      expect(await (await api.get(`/api/v1/asks/${ask.ref}`)).json()).toMatchObject({ state: 'answered', answer: 'Ship it on Friday' });
      await api.dispose();
    });

    test('a refused answer is shown inside the dialog and the dialog stays', async ({ page, playwright, baseURL }) => {
      const { slug, api } = await ground(playwright, baseURL, `refuse-${vp.name}`);
      const ask = await post<{ ref: string }>(api, '/api/v1/asks', { question: 'Too late?', initiative: slug }, 201);
      await page.goto(`/#/attention?initiative=${slug}`);
      const item = section(page, 'open_asks').getByTestId('attention-item');
      await item.getByRole('button', { name: `Answer ${ask.ref}` }).click();
      await expect(dialog(page)).toBeVisible(FIVE_SECONDS);
      await dialog(page).getByRole('button', { name: 'Send answer' }).click();
      await expect(page.getByTestId('dialog-error')).toContainText('Write an answer', FIVE_SECONDS);

      await post(api, `/api/v1/asks/${ask.ref}/answer`, { answer: 'Somebody else was quicker' }, 200);
      await dialog(page).getByRole('textbox', { name: 'Answer' }).fill('Mine');
      await dialog(page).getByRole('button', { name: 'Send answer' }).click();
      await expect(page.getByTestId('dialog-error')).toContainText(ask.ref, FIVE_SECONDS);
      await expect(page.getByTestId('dialog-error')).not.toContainText('Write an answer');
      await expect(dialog(page)).toBeVisible();
      await expect(page.getByTestId('toast')).toHaveCount(0);
      await api.dispose();
    });

    test('Withdraw takes a reason and the ask leaves its section', async ({ page, playwright, baseURL }) => {
      const { slug, api } = await ground(playwright, baseURL, `wd-${vp.name}`);
      const ask = await post<{ ref: string }>(api, '/api/v1/asks', { question: 'Never mind?', initiative: slug }, 201);
      await page.goto(`/#/attention?initiative=${slug}`);
      const item = section(page, 'open_asks').getByTestId('attention-item');
      await item.getByRole('button', { name: `Withdraw ${ask.ref}` }).click();
      await expect(dialog(page)).toBeVisible(FIVE_SECONDS);
      await dialog(page).getByRole('textbox', { name: 'Reason (optional)' }).fill('Found it myself');
      await dialog(page).getByRole('button', { name: 'Withdraw ask' }).click();
      await expect(page.getByTestId('toast')).toHaveText(`Withdrew ${ask.ref}`, FIVE_SECONDS);
      await expect(section(page, 'open_asks').getByTestId('attention-item')).toHaveCount(0, FIVE_SECONDS);
      expect(await (await api.get(`/api/v1/asks/${ask.ref}`)).json()).toMatchObject({ state: 'withdrawn' });
      await api.dispose();
    });

    test('Close takes a resolution and the task leaves Done, not closed', async ({ page, playwright, baseURL }) => {
      const { slug, api } = await ground(playwright, baseURL, `close-${vp.name}`);
      const task = (await post<{ ref: string }>(api, '/api/v1/tasks', { initiative: slug, title: 'Finished for good' }, 201)).ref;
      await post(api, `/api/v1/tasks/${task}/done`, {}, 200);
      await page.goto(`/#/attention?initiative=${slug}`);
      const item = section(page, 'done_not_closed').getByTestId('attention-item');
      await expect(item).toContainText('Finished for good', FIVE_SECONDS);
      await expect(item.getByTestId('initiative-link')).toHaveAttribute('href', `#/i/${slug}`);
      await item.getByRole('button', { name: `Close ${task}` }).click();
      await expect(dialog(page)).toBeVisible();
      await dialog(page).getByLabel('Resolution').selectOption('duplicate');
      await dialog(page).getByRole('button', { name: 'Close task' }).click();
      await expect(page.getByTestId('toast')).toHaveText(`Task ${task} closed as duplicate`, FIVE_SECONDS);
      await expect(section(page, 'done_not_closed').getByTestId('attention-item')).toHaveCount(0, FIVE_SECONDS);
      expect(await (await api.get(`/api/v1/tasks/${task}`)).json()).toMatchObject({ status: 'closed', resolution: 'duplicate' });
      await api.dispose();
    });

    test('Acknowledge clears an expired lease at once', async ({ page, playwright, baseURL }) => {
      const { slug, api } = await ground(playwright, baseURL, `ack-${vp.name}`);
      const task = (await post<{ ref: string }>(api, '/api/v1/tasks', { initiative: slug, title: 'Ran out' }, 201)).ref;
      const lease = (await post<{ lease: { ref: string } }>(api, '/api/v1/leases', { task, intent: 'slow edit', resources: [`repo:ack-${vp.name}`] }, 201)).lease.ref;
      await expireLease(api, lease);
      await page.goto(`/#/attention?initiative=${slug}`);
      const item = section(page, 'expired_leases').getByTestId('attention-item');
      await expect(item).toContainText(lease, FIVE_SECONDS);
      await item.getByRole('button', { name: `Acknowledge ${lease}` }).click();
      await expect(page.getByTestId('toast')).toHaveText(`Lease ${lease} acknowledged`, FIVE_SECONDS);
      await expect(section(page, 'expired_leases').getByTestId('attention-item')).toHaveCount(0, FIVE_SECONDS);
      expect(await (await api.get(`/api/v1/leases/${lease}`)).json()).toMatchObject({ acked_by: 'operator' });
      await api.dispose();
    });

    test('Record check takes a summary and the watch leaves Overdue watches', async ({ page, playwright, baseURL }) => {
      const { slug, api } = await ground(playwright, baseURL, `check-${vp.name}`);
      const task = (
        await post<{ ref: string }>(api, '/api/v1/tasks', { initiative: slug, title: 'Late watch', kind: 'watch', cadence_seconds: 900, next_check_at: '2020-01-01T00:00:00Z' }, 201)
      ).ref;
      await page.goto(`/#/attention?initiative=${slug}`);
      const item = section(page, 'overdue_watches').getByTestId('attention-item');
      await expect(item).toContainText('Late watch', FIVE_SECONDS);
      await item.getByRole('button', { name: `Record check ${task}` }).click();
      await expect(dialog(page)).toBeVisible();
      await dialog(page).getByRole('textbox', { name: 'Summary' }).fill('Nothing moved');
      await dialog(page).getByRole('button', { name: 'Record check' }).click();
      await expect(page.getByTestId('toast')).toHaveText(`Check recorded on ${task}`, FIVE_SECONDS);
      await expect(section(page, 'overdue_watches').getByTestId('attention-item')).toHaveCount(0, FIVE_SECONDS);
      await api.dispose();
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
