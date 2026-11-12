import { expect, test, type APIRequestContext, type Page, type PlaywrightTestArgs } from '@playwright/test';

// The Attention view's saved filters (PRD R16, SPEC 11): the tag and initiative filters are saved under
// `board.view.v1.attention`, a bare `#/attention` restores them before the first fetch (one `GET /attention`), a fragment
// with its own query wins, Clear filters forgets them, and a restored initiative or tag the board does not have is dropped.
const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const KEY = 'board.view.v1.attention';
const FIVE_SECONDS = { timeout: 5000 };

async function post(api: APIRequestContext, path: string, data: object, status: number) {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
}

const apiContext = (playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined) =>
  playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' } });

const nav = (page: Page) => page.getByRole('navigation', { name: 'Main' });
const goTo = async (page: Page, label: string) => {
  await nav(page).getByRole('link', { name: label }).click();
};
const chip = (page: Page, name: string) => page.getByRole('button', { name, exact: true });
const saved = async (page: Page) => JSON.parse((await page.evaluate((k) => localStorage.getItem(k), KEY)) as string) as { v: number; query: string };

// The `GET /attention` requests a page sends.
function attentionReads(page: Page) {
  const reads = { count: 0, queries: [] as string[] };
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (url.pathname !== '/api/v1/attention') return;
    reads.count++;
    reads.queries.push(url.search);
  });
  return reads;
}

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('a tag and an initiative filter survive Initiatives and back, and a new page of the same context, with one GET /attention each', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const slug = `e2e-av-restore-${vp.name}`;
      const tag = `av-restore-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug, title: `Attention view ${vp.name}`, tags: [tag] }, 201);

      await page.goto('/#/attention');
      await expect(chip(page, tag)).toBeVisible(FIVE_SECONDS);
      await chip(page, tag).click();
      await chip(page, slug).click();
      const query = `tag=${tag}&initiative=${slug}`;
      await expect(page).toHaveURL(new RegExp(`#/attention\\?${query}$`));
      expect(await saved(page)).toEqual({ v: 1, query });

      // Away through the nav and back through the nav: the bare route restores the filters with one fetch.
      await goTo(page, 'Initiatives');
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
      const reads = attentionReads(page);
      await goTo(page, 'Attention');
      await expect(page).toHaveURL(new RegExp(`#/attention\\?${query}$`));
      await expect(chip(page, tag)).toHaveAttribute('aria-pressed', 'true', FIVE_SECONDS);
      await expect(chip(page, slug)).toHaveAttribute('aria-pressed', 'true');
      await expect(page.getByTestId('attention-sections')).toBeVisible(FIVE_SECONDS);
      expect(reads.count).toBe(1);
      expect(reads.queries).toEqual([`?${query}`]);

      // A new page of the same context opens `#/attention` bare and gets the same filters, again with one fetch.
      const second = await page.context().newPage();
      const secondReads = attentionReads(second);
      await second.goto('/#/attention');
      await expect(chip(second, tag)).toHaveAttribute('aria-pressed', 'true', FIVE_SECONDS);
      await expect(chip(second, slug)).toHaveAttribute('aria-pressed', 'true');
      await expect(second.getByTestId('attention-sections')).toBeVisible(FIVE_SECONDS);
      await expect(second).toHaveURL(new RegExp(`#/attention\\?${query}$`));
      expect(secondReads.count).toBe(1);
      expect(secondReads.queries).toEqual([`?${query}`]);
      await second.close();
      await api.dispose();
    });

    test('a fragment with its own query overrides the saved one, and Clear filters then nav away and back shows the defaults', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const a = `e2e-av-own-a-${vp.name}`;
      const b = `e2e-av-own-b-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug: a, title: `Own a ${vp.name}` }, 201);
      await post(api, '/api/v1/initiatives', { slug: b, title: `Own b ${vp.name}` }, 201);

      await page.goto(`/#/attention?initiative=${b}`);
      await expect(chip(page, b)).toHaveAttribute('aria-pressed', 'true', FIVE_SECONDS);
      expect((await saved(page)).query).toBe(`initiative=${b}`);

      // A new page opens a link with its own query: that one wins and is what is saved from now on.
      const link = await page.context().newPage();
      await link.goto(`/#/attention?initiative=${a}`);
      await expect(chip(link, a)).toHaveAttribute('aria-pressed', 'true', FIVE_SECONDS);
      await expect(chip(link, b)).toHaveAttribute('aria-pressed', 'false');
      expect((await saved(link)).query).toBe(`initiative=${a}`);

      await goTo(link, 'Initiatives');
      await expect(link.getByTestId('initiative-row').first()).toBeVisible();
      await goTo(link, 'Attention');
      await expect(chip(link, a)).toHaveAttribute('aria-pressed', 'true', FIVE_SECONDS);
      await expect(chip(link, b)).toHaveAttribute('aria-pressed', 'false');

      // Clear filters: the defaults at once, nothing saved, and the defaults again after nav away and back.
      await link.getByTestId('clear-filters').click();
      await expect(link).toHaveURL(/#\/attention$/);
      await expect(chip(link, a)).toHaveAttribute('aria-pressed', 'false');
      await expect(link.getByTestId('clear-filters')).toHaveCount(0);
      expect(await link.evaluate((k) => localStorage.getItem(k), KEY)).toBeNull();
      await goTo(link, 'Initiatives');
      await expect(link.getByTestId('initiative-row').first()).toBeVisible();
      const reads = attentionReads(link);
      await goTo(link, 'Attention');
      await expect(link.getByTestId('attention-sections')).toBeVisible(FIVE_SECONDS);
      await expect(link).toHaveURL(/#\/attention$/);
      await expect(chip(link, a)).toHaveAttribute('aria-pressed', 'false');
      await expect(link.getByTestId('clear-filters')).toHaveCount(0);
      expect(reads.queries).toEqual(['']);
      expect(await link.evaluate((k) => localStorage.getItem(k), KEY)).toBeNull();
      await link.close();
      await api.dispose();
    });

    test('a restored initiative and tag the board does not have are dropped, the known ones stay', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const slug = `e2e-av-drop-${vp.name}`;
      const tag = `av-drop-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug, title: `Drop ${vp.name}`, tags: [tag] }, 201);
      await page.goto('/#/initiatives');
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
      const query = `tag=${tag}&tag=av-ghost-tag&initiative=e2e-av-ghost&initiative=${slug}`;
      await page.evaluate(([k, q]) => localStorage.setItem(k, JSON.stringify({ v: 1, query: q })), [KEY, query] as const);

      const fresh = await page.context().newPage();
      const reads = attentionReads(fresh);
      await fresh.goto('/#/attention');
      const kept = `tag=${tag}&initiative=${slug}`;
      await expect(fresh).toHaveURL(new RegExp(`#/attention\\?${kept}$`));
      await expect(chip(fresh, tag)).toHaveAttribute('aria-pressed', 'true', FIVE_SECONDS);
      await expect(chip(fresh, slug)).toHaveAttribute('aria-pressed', 'true');
      await expect(fresh.getByTestId('attention-sections')).toBeVisible(FIVE_SECONDS);
      await expect(chip(fresh, 'av-ghost-tag')).toHaveCount(0);
      await expect(chip(fresh, 'e2e-av-ghost')).toHaveCount(0);
      // The unknown values never reached the server, and the saved query was rewritten without them.
      expect(reads.queries).toEqual([`?${kept}`]);
      expect((await saved(fresh)).query).toBe(kept);
      await fresh.close();
      await api.dispose();
    });
  });
}
