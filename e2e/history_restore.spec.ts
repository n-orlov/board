import { expect, test, type APIRequestContext, type Locator, type Page, type PlaywrightTestArgs } from '@playwright/test';

// Back and Forward restore a page the way it was left (PRD R7, SPEC 11): the Attention view (filters), the Initiatives
// list (status filter and sort) and the initiative page (task filter and sort) each come back with the scroll position
// of the history entry, the filters in the fragment, the sort column and direction (the header's aria-sort at desktop,
// the Sort select at phone, the fragment's `sort` and `dir`) and the row order they had when a link was followed. Forward
// and then Back again restores the same. Every test seeds rows of its own (slugs start with `e2e-hr-`) and reads only those.
const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const FIVE_SECONDS = { timeout: 5000 };
const SCROLL_TO = 300;

async function post<T = { ref: string }>(api: APIRequestContext, path: string, data: object, status: number): Promise<T> {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
  return (await response.json()) as T;
}

const apiContext = (playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined) =>
  playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' } });

const th = (page: Page, column: string): Locator => page.locator('th', { has: page.getByTestId(`sort-${column}`) });
const chip = (page: Page, name: string) => page.getByRole('button', { name, exact: true });
const scrollY = (page: Page) => page.evaluate(() => window.scrollY);
const fragment = (page: Page) => new URL(page.url()).hash;
const queryOf = (page: Page) => new URLSearchParams(fragment(page).split('?')[1] ?? '');

async function sortBy(page: Page, phone: boolean, column: string, dir: 'asc' | 'desc') {
  if (phone) {
    await page.getByTestId('sort-select').selectOption(`${column}:${dir}`);
    return;
  }
  // The header cycles none -> ascending -> descending: click until the target is reached.
  const clicks = dir === 'asc' ? 1 : 2;
  for (let i = 0; i < clicks; i++) await page.getByTestId(`sort-${column}`).click();
}

/** The sort the page shows, the way a person reads it: the header's aria-sort at desktop, the Sort select at phone. */
async function shownSort(page: Page, phone: boolean, column: string): Promise<string> {
  return phone ? page.getByTestId('sort-select').inputValue() : ((await th(page, column).getAttribute('aria-sort')) ?? '');
}

/** Scrolls the page and waits until it stays there. */
async function scrollDown(page: Page) {
  const room = await page.evaluate(() => document.documentElement.scrollHeight - window.innerHeight);
  expect(room, 'the seeded rows must make the page scroll').toBeGreaterThan(SCROLL_TO);
  await page.evaluate((y) => window.scrollTo(0, y), SCROLL_TO);
  await expect.poll(() => scrollY(page)).toBe(SCROLL_TO);
}

/** Follows the first link of `links` that is fully inside the viewport, clear of the fixed bars: no scroll happens on the way. */
async function followVisibleLink(page: Page, links: Locator): Promise<number> {
  const at = await links.evaluateAll((els) => els.findIndex((el) => {
    const r = el.getBoundingClientRect();
    return r.top > 120 && r.bottom < window.innerHeight - 120 && r.width > 0;
  }));
  expect(at, 'a link inside the viewport after scrolling').toBeGreaterThanOrEqual(0);
  const before = await scrollY(page);
  await links.nth(at).click();
  return before;
}

interface Left {
  scrollY: number;
  fragment: string;
  rows: string[];
  sort: string | undefined;
}

interface Route {
  /** The page's own rows, in the order shown (other specs' rows left out). */
  rows: (page: Page) => Promise<string[]>;
  /** What must hold for the page to be shown as it was left. */
  shown: (page: Page, left: Left) => Promise<void>;
  /** The links of the page that leave it. */
  links: (page: Page) => Locator;
  /** Where the link followed lands. */
  landing: RegExp;
  sortColumn?: string;
}

/** Back, then Forward and Back again: both returns must show `left`. */
async function backAndForward(page: Page, route: Route, left: Left, phone: boolean) {
  const restored = async () => {
    await expect(page).toHaveURL((url) => url.hash === left.fragment, FIVE_SECONDS);
    await route.shown(page, left);
    await expect.poll(() => route.rows(page), FIVE_SECONDS).toEqual(left.rows);
    await expect.poll(async () => Math.abs((await scrollY(page)) - left.scrollY), FIVE_SECONDS).toBeLessThanOrEqual(2);
    if (route.sortColumn !== undefined) {
      expect(await shownSort(page, phone, route.sortColumn)).toBe(left.sort);
      expect(queryOf(page).get('sort')).toBe(route.sortColumn);
      expect(queryOf(page).get('dir')).toBe('desc');
    }
  };
  await page.goBack();
  await restored();
  await page.goForward();
  await expect(page).toHaveURL(route.landing, FIVE_SECONDS);
  await page.goBack();
  await restored();
}

/** Fills the route in, records how it looks, follows a link away and checks both returns. */
async function leaveAndReturn(page: Page, route: Route, phone: boolean) {
  await scrollDown(page);
  const rows = await route.rows(page);
  const sort = route.sortColumn === undefined ? undefined : await shownSort(page, phone, route.sortColumn);
  const left: Left = { scrollY: await scrollY(page), fragment: fragment(page), rows, sort };
  const before = await followVisibleLink(page, route.links(page));
  expect(Math.abs(before - left.scrollY)).toBeLessThanOrEqual(2);
  await expect(page).toHaveURL(route.landing, FIVE_SECONDS);
  await backAndForward(page, route, left, phone);
}

for (const vp of viewports) {
  const phone = vp.name === 'phone';
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('the Attention view comes back with its tag and initiative filters and its scroll', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const slug = `e2e-hr-att-${vp.name}`;
      const tag = `hr-att-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug, title: `History restore attention ${vp.name}`, tags: [tag] }, 201);
      // Finished tasks wait for a review: each is a card of its own in the view.
      for (let i = 0; i < 16; i++) {
        const task = await post(api, '/api/v1/tasks', { initiative: slug, title: `Review ${String(i).padStart(2, '0')}` }, 201);
        await post(api, `/api/v1/tasks/${task.ref}/done`, {}, 200);
      }

      await page.goto('/#/attention');
      await expect(chip(page, tag)).toBeVisible(FIVE_SECONDS);
      await chip(page, tag).click();
      await chip(page, slug).click();
      await expect(page).toHaveURL((url) => url.hash === `#/attention?tag=${tag}&initiative=${slug}`);
      await expect(page.getByTestId('attention-item').first()).toBeVisible(FIVE_SECONDS);
      await expect.poll(() => page.getByTestId('attention-item').count(), FIVE_SECONDS).toBeGreaterThanOrEqual(16);

      const route: Route = {
        rows: (p) => p.getByTestId('item-link').allTextContents(),
        shown: async (p) => {
          await expect(chip(p, tag)).toHaveAttribute('aria-pressed', 'true', FIVE_SECONDS);
          await expect(chip(p, slug)).toHaveAttribute('aria-pressed', 'true');
        },
        links: (p) => p.getByTestId('item-link'),
        landing: /#\/t\//,
      };
      await leaveAndReturn(page, route, phone);
    });

    test('the Initiatives list comes back with its status filter, sort, row order and scroll', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const prefix = `e2e-hr-ini-${vp.name}`;
      // Created in this order; no title sort gives it.
      const titles = ['Mango', 'apple', 'Zebra', 'cherry', 'Lemon', 'banana', 'Quince', 'date', 'Fig', 'grape', 'Kiwi', 'olive', 'Pear', 'jam', 'Nut', 'ice'];
      const slugs = titles.map((title, i) => `${prefix}-${String(i).padStart(2, '0')}`);
      for (const [i, title] of titles.entries()) await post(api, '/api/v1/initiatives', { slug: slugs[i], title: `${title} ${prefix}` }, 201);
      await post(api, `/api/v1/initiatives/${slugs[2]}/close`, {}, 200);
      const own = async (p: Page) =>
        (await p.getByTestId('initiative-row').evaluateAll((rows) => rows.map((row) => row.getAttribute('data-slug') ?? ''))).filter((s) => s.startsWith(`${prefix}-`));

      await page.goto('/#/initiatives');
      await page.getByTestId('initiative-filter').selectOption('all');
      await expect.poll(() => own(page), FIVE_SECONDS).toHaveLength(16);
      const byDefault = await own(page);
      await sortBy(page, phone, 'title', 'desc');
      await expect(page).toHaveURL((url) => url.hash === '#/initiatives?status=all&sort=title&dir=desc');
      const sorted = await own(page);
      expect(sorted, 'the picked sort must reorder the rows').not.toEqual(byDefault);
      expect(sorted).toEqual([...slugs].sort((a, b) => titles[slugs.indexOf(b)].toLowerCase().localeCompare(titles[slugs.indexOf(a)].toLowerCase())));

      const route: Route = {
        rows: own,
        shown: async (p) => {
          await expect(p.getByTestId('initiative-filter')).toHaveValue('all', FIVE_SECONDS);
        },
        links: (p) => p.getByTestId('initiative-row').locator('a'),
        landing: /#\/i\//,
        sortColumn: 'title',
      };
      await leaveAndReturn(page, route, phone);
    });

    test('the initiative page comes back with its task filter, sort, row order and scroll', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const slug = `e2e-hr-page-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug, title: `History restore page ${vp.name}` }, 201);
      const titles = ['Mango', 'apple', 'Zebra', 'cherry', 'Lemon', 'banana', 'Quince', 'date', 'Fig', 'grape', 'Kiwi', 'olive', 'Pear', 'jam', 'Nut', 'ice', 'Rye', 'oat', 'Tea', 'bean'];
      const refs: string[] = [];
      for (const title of titles) refs.push((await post(api, '/api/v1/tasks', { initiative: slug, title: `${title} ${slug}` }, 201)).ref);
      // Closed tasks only show under the All filter.
      for (const ref of refs.slice(2, 4)) {
        await post(api, `/api/v1/tasks/${ref}/done`, {}, 200);
        await post(api, `/api/v1/tasks/${ref}/close`, { resolution: 'completed' }, 200);
      }
      const own = (p: Page) => p.getByTestId('task-ref').allTextContents();

      await page.goto(`/#/i/${slug}`);
      await expect.poll(() => own(page), FIVE_SECONDS).toHaveLength(18);
      const byDefault = await own(page);
      await page.getByTestId('task-filter').selectOption('all');
      await expect.poll(() => own(page), FIVE_SECONDS).toHaveLength(20);
      await sortBy(page, phone, 'title', 'desc');
      await expect(page).toHaveURL((url) => url.hash === `#/i/${slug}?status=all&sort=title&dir=desc`);
      const sorted = await own(page);
      expect(sorted, 'the picked sort must reorder the rows').not.toEqual([...byDefault, ...refs.slice(2, 4)]);
      expect(sorted).toHaveLength(20);

      const route: Route = {
        rows: own,
        shown: async (p) => {
          await expect(p.getByTestId('task-filter')).toHaveValue('all', FIVE_SECONDS);
        },
        links: (p) => p.getByTestId('task-ref').locator('a'),
        landing: /#\/t\//,
        sortColumn: 'title',
      };
      await leaveAndReturn(page, route, phone);
    });
  });
}
