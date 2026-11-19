import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { BANDS_KEY, FLIGHT_KEY, band, created, seedFlight, seedNeedsMe } from './now';

// The Now view's Needs me bands (PRD R1, SPEC 11): the four bands in order with their counts, every band open on the desktop
// but Done, not closed, an accordion on the phone (Open asks and Expired leases and conflicts open when they hold something,
// the others closed), `#/attention` and `#/` landing on `#/now` with any filter kept, and the open state of a band remembered
// across a reload, apart for wide and narrow screens. The board is shared, so each test filters to an initiative of its own.
const FIVE_SECONDS = { timeout: 5000 };
const PHONE = { width: 390, height: 844 };
const DESKTOP = { width: 1440, height: 900 };
const KEYS = ['asks', 'expired', 'watches', 'done'] as const;

const expanded = async (page: Page) => Promise.all(KEYS.map((key) => band(page, key).getByTestId('band-toggle').getAttribute('aria-expanded')));
const stored = (page: Page) => page.evaluate((key) => JSON.parse(localStorage.getItem(key) ?? 'null') as unknown, BANDS_KEY);

test.describe('desktop 1440x900', () => {
  test.use({ viewport: DESKTOP });

  test('the bands come in order with their counts, every one open but Done, not closed', async ({ page, playwright, baseURL }) => {
    const s = await seedNeedsMe(playwright, baseURL, 'order-desktop');
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(page.getByRole('heading', { level: 2, name: 'Now' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 3, name: 'Needs me' })).toBeVisible();
    await expect(page.getByTestId('band-toggle')).toHaveText([/^\s*Open asks\s*1\s*$/, /^\s*Expired leases and conflicts\s*1\s*$/, /^\s*Overdue watches\s*1\s*$/, /^\s*Done, not closed\s*1\s*$/], FIVE_SECONDS);
    expect(await expanded(page)).toEqual(['true', 'true', 'true', 'false']);

    // Top to bottom in band order, Done, not closed last; each open band shows its item, the closed one hides it.
    const tops = await Promise.all(KEYS.map(async (key) => (await band(page, key).boundingBox())!.y));
    expect([...tops].sort((x, y) => x - y)).toEqual(tops);
    await expect(band(page, 'asks').getByTestId('attention-item')).toContainText(s.ask);
    await expect(band(page, 'expired').getByTestId('attention-item')).toHaveCount(1);
    await expect(band(page, 'watches').getByTestId('attention-item')).toContainText('Slow watch');
    await expect(band(page, 'done').getByTestId('attention-item')).toHaveCount(1);
    await expect(band(page, 'done').getByTestId('attention-item')).toBeHidden();
    // Not dimmed: the last band is as opaque as the first.
    const opacity = (key: (typeof KEYS)[number]) => band(page, key).evaluate((el) => getComputedStyle(el).opacity);
    expect(await opacity('done')).toBe(await opacity('asks'));
  });

  test('an empty board filter leaves every band open but Done, not closed', async ({ page, playwright, baseURL }) => {
    const api = await playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'now-quiet' } });
    expect((await api.post('/api/v1/initiatives', { data: { slug: 'e2e-now-quiet-desktop', title: 'Quiet' } })).status()).toBe(201);
    await api.dispose();
    await page.goto('/#/now?initiative=e2e-now-quiet-desktop');
    await expect(page.getByTestId('band-count')).toHaveText(['0', '0', '0', '0'], FIVE_SECONDS);
    expect(await expanded(page)).toEqual(['true', 'true', 'true', 'false']);
    await expect(page.getByTestId('band-empty')).toHaveCount(4);
  });

  test('a band toggled closed stays closed after a reload, and Done, not closed stays open once opened', async ({ page, playwright, baseURL }) => {
    const s = await seedNeedsMe(playwright, baseURL, 'reload-desktop');
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(band(page, 'asks').getByTestId('attention-item')).toBeVisible(FIVE_SECONDS);
    await band(page, 'asks').getByTestId('band-toggle').click();
    await band(page, 'done').getByTestId('band-toggle').click();
    expect(await expanded(page)).toEqual(['false', 'true', 'true', 'true']);
    await expect(band(page, 'asks').getByTestId('attention-item')).toBeHidden();
    await expect(band(page, 'done').getByTestId('attention-item')).toBeVisible();
    expect(await stored(page)).toEqual({ wide: { asks: false, done: true }, narrow: {} });

    await page.reload();
    await expect(band(page, 'watches').getByTestId('attention-item')).toBeVisible(FIVE_SECONDS);
    expect(await expanded(page)).toEqual(['false', 'true', 'true', 'true']);
    await expect(band(page, 'asks').getByTestId('attention-item')).toBeHidden();
    await expect(band(page, 'done').getByTestId('attention-item')).toBeVisible();
  });

  test('the open state is remembered apart for wide and narrow screens', async ({ page, playwright, baseURL }) => {
    const s = await seedNeedsMe(playwright, baseURL, 'apart-desktop');
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(band(page, 'asks').getByTestId('attention-item')).toBeVisible(FIVE_SECONDS);
    await band(page, 'asks').getByTestId('band-toggle').click(); // wide: closed
    expect(await expanded(page)).toEqual(['false', 'true', 'true', 'false']);

    await page.setViewportSize(PHONE); // narrow: its own defaults, the wide choice untouched
    await expect.poll(() => expanded(page)).toEqual(['true', 'true', 'false', 'false']);
    await band(page, 'watches').getByTestId('band-toggle').click(); // narrow: opened
    expect(await stored(page)).toEqual({ wide: { asks: false }, narrow: { watches: true } });

    await page.setViewportSize(DESKTOP);
    await expect.poll(() => expanded(page)).toEqual(['false', 'true', 'true', 'false']);
    await page.setViewportSize(PHONE);
    await expect.poll(() => expanded(page)).toEqual(['true', 'true', 'true', 'false']);
  });

  test('a bad stored value falls back to the defaults', async ({ page, playwright, baseURL }) => {
    const s = await seedNeedsMe(playwright, baseURL, 'bad-desktop');
    await page.addInitScript((key) => localStorage.setItem(key, '{"wide":7,"narrow":["x"]'), BANDS_KEY);
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(band(page, 'asks').getByTestId('attention-item')).toBeVisible(FIVE_SECONDS);
    expect(await expanded(page)).toEqual(['true', 'true', 'true', 'false']);
  });
});

test.describe('phone 390x844', () => {
  test.use({ viewport: PHONE });

  test('the bands are an accordion: Open asks and Expired open when they hold something, the others closed', async ({ page, playwright, baseURL }) => {
    const s = await seedNeedsMe(playwright, baseURL, 'accordion-phone');
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(page.getByTestId('band-count')).toHaveText(['1', '1', '1', '1'], FIVE_SECONDS);
    expect(await expanded(page)).toEqual(['true', 'true', 'false', 'false']);
    await expect(band(page, 'asks').getByTestId('attention-item')).toBeVisible();
    await expect(band(page, 'watches').getByTestId('attention-item')).toBeHidden();
    await expect(band(page, 'done').getByTestId('attention-item')).toBeHidden();

    // One line per band: a 44 px target that names the band and its count; opening one shows its item.
    for (const key of KEYS) {
      const toggle = band(page, key).getByTestId('band-toggle');
      expect((await toggle.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    }
    await band(page, 'watches').getByTestId('band-toggle').click();
    await expect(band(page, 'watches').getByTestId('attention-item')).toBeVisible();
    await band(page, 'done').getByTestId('band-toggle').click();
    await expect(band(page, 'done').getByTestId('attention-item')).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  });

  test('empty bands all start closed on the phone', async ({ page, playwright, baseURL }) => {
    const api = await playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'now-quiet' } });
    expect((await api.post('/api/v1/initiatives', { data: { slug: 'e2e-now-quiet-phone', title: 'Quiet' } })).status()).toBe(201);
    await api.dispose();
    await page.goto('/#/now?initiative=e2e-now-quiet-phone');
    await expect(page.getByTestId('band-count')).toHaveText(['0', '0', '0', '0'], FIVE_SECONDS);
    expect(await expanded(page)).toEqual(['false', 'false', 'false', 'false']);
  });

  test('a band opened on the phone stays open after a reload, and the keyboard toggles it', async ({ page, playwright, baseURL }) => {
    const s = await seedNeedsMe(playwright, baseURL, 'reload-phone');
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(band(page, 'asks').getByTestId('attention-item')).toBeVisible(FIVE_SECONDS);
    const done = band(page, 'done').getByTestId('band-toggle');
    await done.focus();
    await page.keyboard.press('Enter');
    await expect(band(page, 'done').getByTestId('attention-item')).toBeVisible();
    await band(page, 'asks').getByTestId('band-toggle').click();
    await page.reload();
    await expect(band(page, 'done').getByTestId('attention-item')).toBeVisible(FIVE_SECONDS);
    expect(await expanded(page)).toEqual(['false', 'true', 'false', 'true']);
  });
});

test.describe('landing', () => {
  test.use({ viewport: DESKTOP });

  for (const entry of ['/#/attention', '/', '/#/']) {
    test(`${entry} lands on #/now`, async ({ page }) => {
      await page.goto(entry);
      await expect(page).toHaveURL(/#\/now$/, FIVE_SECONDS);
      await expect(page.getByTestId('now-view')).toBeVisible();
      await expect(page.getByRole('navigation', { name: 'Main' }).locator('visible=true').getByRole('link', { name: 'Now' })).toHaveAttribute('aria-current', 'page');
    });
  }

  test('#/attention keeps the filter in the fragment', async ({ page, playwright, baseURL }) => {
    const s = await seedNeedsMe(playwright, baseURL, 'land-filter');
    await page.goto(`/#/attention?initiative=${s.slug}`);
    await expect(page).toHaveURL(new RegExp(`#/now\\?initiative=${s.slug}$`), FIVE_SECONDS);
    await expect(page.getByRole('button', { name: s.slug, exact: true })).toHaveAttribute('aria-pressed', 'true', FIVE_SECONDS);
    await expect(band(page, 'asks').getByTestId('attention-item')).toHaveCount(1, FIVE_SECONDS);
  });

  test('the nav says Now and a hash change to #/attention lands on it too', async ({ page }) => {
    await page.goto('/#/tasks');
    await expect(page.getByTestId('tasks-view')).toBeVisible(FIVE_SECONDS);
    await page.evaluate(() => (window.location.hash = '#/attention'));
    await expect(page).toHaveURL(/#\/now$/, FIVE_SECONDS);
    await expect(page.getByTestId('now-view')).toBeVisible();
  });
});

// The right column (PRD R1, SPEC 11): In flight (a row per task with a live lease, its leases nested with holder, intent and time
// left) and Recently touched (tasks with no live lease touched within the window, each with its last event), both from the one
// `GET /attention/now` read, so the page never asks `GET /api/v1/events`. The window switch (8h, 24h, 3d; 24h by default) is
// remembered under `vh.now.window.v1`.
const flight = (page: Page) => page.getByTestId('now-flight');
const flightRow = (page: Page, ref: string) => page.locator(`[data-testid=flight-row][data-task="${ref}"]`);
const recentRow = (page: Page, ref: string) => page.locator(`[data-testid=recent-row][data-task="${ref}"]`);
const windowChoice = (page: Page, label: string) => page.getByTestId('now-window').getByRole('radio', { name: label });
const storedWindow = (page: Page) => page.evaluate((key) => localStorage.getItem(key), FLIGHT_KEY);

test.describe('the right column on the desktop', () => {
  test.use({ viewport: DESKTOP });

  test('two columns: Needs me on the left, In flight with nested leases and Recently touched on the right', async ({ page, playwright, baseURL }) => {
    const s = await seedFlight(playwright, baseURL, 'columns');
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(flightRow(page, s.live)).toBeVisible(FIVE_SECONDS);
    const needs = (await page.getByTestId('now-needs').boundingBox())!;
    const right = (await flight(page).boundingBox())!;
    expect(right.x).toBeGreaterThanOrEqual(needs.x + needs.width);
    expect(Math.abs(right.y - needs.y)).toBeLessThanOrEqual(2);
    await expect(page.getByRole('heading', { level: 3, name: /^In flight/ })).toBeVisible();
    await expect(page.getByRole('heading', { level: 3, name: /^Recently touched/ })).toBeVisible();

    // One row for the task, its two leases nested: holder, intent and the time left.
    await expect(page.getByTestId('flight-row')).toHaveCount(1);
    const leases = flightRow(page, s.live).getByTestId('flight-lease');
    await expect(leases).toHaveCount(2);
    await expect(leases.nth(0).getByTestId('lease-holder')).toHaveText(s.holders[0]);
    await expect(leases.nth(0).getByTestId('lease-intent')).toHaveText('edit columns');
    await expect(leases.nth(0).getByTestId('lease-ttl')).toHaveText(/^expires in (\d+h )?\d+[hm]$/);
    await expect(leases.nth(1).getByTestId('lease-holder')).toHaveText(s.holders[1]);
    await expect(leases.nth(1).getByTestId('lease-intent')).toHaveText('review columns');
    await expect(flightRow(page, s.live).getByTestId('flight-task')).toHaveAttribute('href', `#/t/${s.live}`);

    // The task whose lease ended is Recently touched with its last event, and is not In flight.
    await expect(flightRow(page, s.ended)).toHaveCount(0);
    await expect(page.getByTestId('recent-row')).toHaveCount(1);
    await expect(recentRow(page, s.ended).getByTestId('recent-last')).toContainText(/^lease_ended by flight-a-columns /);
    await expect(recentRow(page, s.ended).getByTestId('recent-task')).toHaveAttribute('href', `#/t/${s.ended}`);
    await expect(recentRow(page, s.live)).toHaveCount(0);
    await s.api.dispose();
  });

  test('a lease started through the API appears in In flight without a reload, and leaves it when it ends', async ({ page, playwright, baseURL }) => {
    const s = await seedFlight(playwright, baseURL, 'live');
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(page.getByTestId('flight-row')).toHaveCount(1, FIVE_SECONDS);
    await page.evaluate(() => ((window as unknown as { __kept: boolean }).__kept = true));
    const fresh = (await created(s.api, '/api/v1/tasks', { initiative: s.slug, title: 'Fresh work' })).ref;
    const lease = await created<{ lease: { ref: string } }>(s.api, '/api/v1/leases', { task: fresh, intent: 'started while watching' });
    await expect(page.getByTestId('flight-row')).toHaveCount(2, FIVE_SECONDS);
    await expect(flightRow(page, fresh).getByTestId('lease-intent')).toHaveText('started while watching');
    expect(await page.evaluate(() => (window as unknown as { __kept?: boolean }).__kept)).toBe(true);

    expect((await s.api.post(`/api/v1/leases/${lease.lease.ref}/end`, { data: {} })).status()).toBe(200);
    await expect(page.getByTestId('flight-row')).toHaveCount(1, FIVE_SECONDS);
    await expect(recentRow(page, fresh)).toBeVisible();
    await s.api.dispose();
  });

  test('Now builds the column from one read and never asks GET /api/v1/events', async ({ page, playwright, baseURL }) => {
    const s = await seedFlight(playwright, baseURL, 'noevents');
    const paths: string[] = [];
    page.on('request', (request) => paths.push(new URL(request.url()).pathname));
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(flightRow(page, s.live)).toBeVisible(FIVE_SECONDS);
    await expect(recentRow(page, s.ended)).toBeVisible();
    await windowChoice(page, '3d').check({ force: true });
    await expect.poll(() => paths.filter((path) => path === '/api/v1/attention/now').length).toBeGreaterThanOrEqual(2);
    expect(paths.filter((path) => path === '/api/v1/events')).toEqual([]);
    await s.api.dispose();
  });

  test('the window switch defaults to 24h, is remembered and changes what is read', async ({ page, playwright, baseURL }) => {
    const s = await seedFlight(playwright, baseURL, 'window');
    const windows: (string | null)[] = [];
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (url.pathname === '/api/v1/attention/now') windows.push(url.searchParams.get('window'));
    });
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(recentRow(page, s.ended)).toBeVisible(FIVE_SECONDS);
    await expect(page.getByTestId('now-window').getByRole('radio')).toHaveCount(3);
    await expect(windowChoice(page, '24h')).toBeChecked();
    expect(await storedWindow(page)).toBeNull();
    expect(windows[0]).toBeNull(); // the default is the server's own

    await windowChoice(page, '8h').check({ force: true });
    await expect.poll(() => windows).toContain('8h');
    expect(await storedWindow(page)).toBe('8h');
    await expect(recentRow(page, s.ended)).toBeVisible(); // just touched: inside 8h too

    await page.reload();
    await expect(windowChoice(page, '8h')).toBeChecked(FIVE_SECONDS);
    await expect(recentRow(page, s.ended)).toBeVisible();
    expect(windows[windows.length - 1]).toBe('8h');
    await s.api.dispose();
  });

  test('a bad stored window falls back to 24h', async ({ page, playwright, baseURL }) => {
    const s = await seedFlight(playwright, baseURL, 'badwindow');
    await page.addInitScript((key) => localStorage.setItem(key, '99y'), FLIGHT_KEY);
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(recentRow(page, s.ended)).toBeVisible(FIVE_SECONDS);
    await expect(windowChoice(page, '24h')).toBeChecked();
    await s.api.dispose();
  });

  test('an initiative with nothing live says so', async ({ page, playwright, baseURL }) => {
    const s = await seedFlight(playwright, baseURL, 'quiet');
    const quiet = 'e2e-flight-quiet-empty';
    await created(s.api, '/api/v1/initiatives', { slug: quiet, title: 'Quiet flight' });
    await page.goto(`/#/now?initiative=${quiet}`);
    await expect(page.getByTestId('flight-empty')).toBeVisible(FIVE_SECONDS);
    await expect(page.getByTestId('recent-empty')).toBeVisible();
    await s.api.dispose();
  });
});

test.describe('the right column on the phone', () => {
  test.use({ viewport: PHONE, hasTouch: true });

  test('the column stacks under Needs me with 44 px targets and no horizontal scroll', async ({ page, playwright, baseURL }) => {
    const s = await seedFlight(playwright, baseURL, 'phone');
    await page.goto(`/#/now?initiative=${s.slug}`);
    await expect(flightRow(page, s.live)).toBeVisible(FIVE_SECONDS);
    const needs = (await page.getByTestId('now-needs').boundingBox())!;
    const right = (await flight(page).boundingBox())!;
    expect(right.y).toBeGreaterThanOrEqual(needs.y + needs.height);
    for (const target of [page.getByTestId('flight-task').first(), page.getByTestId('recent-task').first(), ...(await windowRadios(page))]) {
      const box = (await target.boundingBox())!;
      expect(box.height).toBeGreaterThanOrEqual(44 - 1 / 32);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
    await s.api.dispose();
  });
});

async function windowRadios(page: Page) {
  return page.getByTestId('now-window').locator('label').all();
}

for (const vp of [
  { name: 'phone', size: PHONE },
  { name: 'desktop', size: DESKTOP },
]) {
  for (const theme of ['light', 'dark'] as const) {
    for (const density of ['compact', 'sparse'] as const) {
      test.describe(`axe ${vp.name} ${theme} ${density}`, () => {
        test.use({ viewport: vp.size });

        test('Now with the right column passes axe', async ({ page, playwright, baseURL }) => {
          const s = await seedFlight(playwright, baseURL, `axe-${vp.name}-${theme}-${density}`);
          await page.addInitScript(
            ([t, d]) => {
              localStorage.setItem('vh.theme', t!);
              localStorage.setItem('vh.density.v1', d!);
            },
            [theme, density],
          );
          await page.goto(`/#/now?initiative=${s.slug}`);
          await expect(flightRow(page, s.live)).toBeVisible(FIVE_SECONDS);
          await expect(recentRow(page, s.ended)).toBeVisible();
          await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
          await expect(page.locator('html')).toHaveAttribute('data-density', density);
          await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))).then(() => undefined));
          const { violations } = await new AxeBuilder({ page }).analyze();
          const found = violations
            .filter((v) => v.impact === 'serious' || v.impact === 'critical')
            .map((v) => ({ rule: v.id, impact: v.impact, nodes: v.nodes.map((n) => ({ target: n.target, summary: n.failureSummary })) }));
          expect(found, 'serious or critical axe violations').toEqual([]);
          await s.api.dispose();
        });
      });
    }
  }
}
