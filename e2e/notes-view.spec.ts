import { expect, test, type APIRequestContext, type Page, type PlaywrightTestArgs } from '@playwright/test';

// The Notes view's saved filters (PRD R16, SPEC 11): the six filters are saved under `board.view.v1.notes` when they
// are applied, a bare `#/notes` restores them before the first fetch (one notes read), a fragment with its own query
// wins, and Clear filters forgets them.
const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const KEY = 'board.view.v1.notes';
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
const saved = async (page: Page) => JSON.parse((await page.evaluate((k) => localStorage.getItem(k), KEY)) as string) as { v: number; query: string };
const stored = (page: Page) => page.evaluate((k) => localStorage.getItem(k), KEY);
const filters = (page: Page) => page.getByTestId('notes-filters');

// The `GET /notes` requests a page sends.
function notesReads(page: Page) {
  const reads = { count: 0, queries: [] as string[] };
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (url.pathname !== '/api/v1/notes') return;
    reads.count++;
    reads.queries.push(url.search);
  });
  return reads;
}

async function apply(page: Page, fields: { author?: string; resource?: string; broadcast?: boolean }) {
  const form = filters(page);
  if (fields.broadcast === true) await form.getByTestId('broadcast-filter').click();
  if (fields.author !== undefined) await form.getByLabel('Author', { exact: true }).fill(fields.author);
  if (fields.resource !== undefined) await form.getByLabel('Resource', { exact: true }).fill(fields.resource);
  await page.getByTestId('apply-filters').click();
}

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('applied filters survive Initiatives and back, and a new page of the same context, with one notes read each', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const author = `nv-restore-${vp.name}`;
      const resource = `host:nv-restore-${vp.name}`;
      await post(api, '/api/v1/notes', { summary: `Restore note ${vp.name}`, resource }, 201);

      await page.goto('/#/notes');
      await expect(page.getByTestId('notes-view')).toBeVisible(FIVE_SECONDS);
      await apply(page, { broadcast: true, resource, author });
      const query = `broadcast=only&resource=${encodeURIComponent(resource)}&author=${author}`;
      await expect(page).toHaveURL(new RegExp(`#/notes\\?${query.replace(/[?.]/g, '\\$&')}$`));
      expect(await saved(page)).toEqual({ v: 1, query });

      // Away through the nav and back through the nav: the bare route restores the filters with one read.
      await goTo(page, 'Initiatives');
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
      const reads = notesReads(page);
      await goTo(page, 'Notes');
      await expect(filters(page).getByLabel('Author', { exact: true })).toHaveValue(author, FIVE_SECONDS);
      await expect(filters(page).getByLabel('Resource', { exact: true })).toHaveValue(resource);
      await expect(filters(page).getByTestId('broadcast-filter')).toHaveAttribute('aria-pressed', 'true');
      await expect(page).toHaveURL(new RegExp(`#/notes\\?${query.replace(/[?.]/g, '\\$&')}$`));
      await expect(page.getByTestId('no-notes').or(page.getByTestId('notes-list'))).toBeVisible(FIVE_SECONDS);
      expect(reads.count).toBe(1);
      expect(new URLSearchParams(reads.queries[0]).get('author')).toBe(author);
      expect(new URLSearchParams(reads.queries[0]).get('resource')).toBe(resource);
      expect(new URLSearchParams(reads.queries[0]).get('broadcast')).toBe('only');

      // A new page of the same context opens `#/notes` bare and gets the same filters, again with one read.
      const second = await page.context().newPage();
      const secondReads = notesReads(second);
      await second.goto('/#/notes');
      await expect(filters(second).getByLabel('Author', { exact: true })).toHaveValue(author, FIVE_SECONDS);
      await expect(second.getByTestId('no-notes').or(second.getByTestId('notes-list'))).toBeVisible(FIVE_SECONDS);
      await expect(second).toHaveURL(new RegExp(`#/notes\\?${query.replace(/[?.]/g, '\\$&')}$`));
      expect(secondReads.count).toBe(1);
      expect(new URLSearchParams(secondReads.queries[0]).get('author')).toBe(author);
      await second.close();
      await api.dispose();
    });

    test('a fragment with its own query overrides the saved one, and Clear filters then nav away and back shows the defaults', async ({ page }) => {
      await page.goto('/#/notes');
      await expect(page.getByTestId('notes-view')).toBeVisible(FIVE_SECONDS);
      await apply(page, { author: `nv-own-a-${vp.name}` });
      expect(await saved(page)).toEqual({ v: 1, query: `author=nv-own-a-${vp.name}` });

      // A link with its own query wins and replaces the saved one.
      await page.goto(`/#/notes?author=nv-own-b-${vp.name}`);
      await expect(filters(page).getByLabel('Author', { exact: true })).toHaveValue(`nv-own-b-${vp.name}`, FIVE_SECONDS);
      expect(await saved(page)).toEqual({ v: 1, query: `author=nv-own-b-${vp.name}` });

      // Clear filters: the defaults at once, nothing saved, and the defaults again after nav away and back.
      await filters(page).getByTestId('clear-filters').click();
      await expect(page).toHaveURL(/#\/notes$/);
      await expect(filters(page).getByLabel('Author', { exact: true })).toHaveValue('');
      await expect(filters(page).getByTestId('clear-filters')).toHaveCount(0);
      expect(await stored(page)).toBeNull();
      await goTo(page, 'Initiatives');
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
      await goTo(page, 'Notes');
      await expect(page.getByTestId('notes-view')).toBeVisible();
      await expect(page).toHaveURL(/#\/notes$/);
      await expect(filters(page).getByLabel('Author', { exact: true })).toHaveValue('');
      await expect(filters(page).getByTestId('clear-filters')).toHaveCount(0);
    });
  });
}
