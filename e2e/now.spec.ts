import { expect, test, type Page } from '@playwright/test';
import { BANDS_KEY, band, seedNeedsMe } from './now';

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
