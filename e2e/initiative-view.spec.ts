import { expect, test, type APIRequestContext, type Locator, type Page, type PlaywrightTestArgs } from '@playwright/test';

// The initiative page's Tasks table sort and saved view state (PRD R16, SPEC 11): the table sorts by Ref (ref), Title (text),
// Kind (kind), Status (status), Flags (text) and Lease holder (text) through the shared header (desktop, with the keyboard) and the Sort
// select (phone); the sort rides in the fragment query next to the task filter; the whole query is saved under the ONE key
// every initiative shares, `board.view.v1.initiative`, so a sort set on one initiative is restored on nav back and on a second
// initiative; a fragment with its own query wins; Clear filters forgets it all. The table has no time column. Each test seeds
// initiatives of its own and reads only their rows.
const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const KEY = 'board.view.v1.initiative';

async function post<T = { ref: string }>(api: APIRequestContext, path: string, data: object, status: number): Promise<T> {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
  return (await response.json()) as T;
}

const apiContext = (playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined) =>
  playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' } });

interface Seed {
  slug: string;
  /** The table's tasks in creation order: Mango (work, open), apple (watch, open), Zebra (work, closed), cherry (work, open). */
  refs: string[];
  /** Two done tasks, which sit in Needs attention above the table, not in it. */
  needs: string[];
}

async function seed(api: APIRequestContext, slug: string): Promise<Seed> {
  await post(api, '/api/v1/initiatives', { slug, title: `Initiative view ${slug}` }, 201);
  const task = async (title: string, extra: object = {}) => (await post(api, '/api/v1/tasks', { initiative: slug, title, ...extra }, 201)).ref;
  const mango = await task('Mango');
  const apple = await task('apple', { kind: 'watch', cadence_seconds: 3600 });
  const zebra = await task('Zebra');
  const cherry = await task('cherry');
  const beta = await task('Beta');
  const gamma = await task('Gamma');
  await post(api, `/api/v1/tasks/${zebra}/done`, {}, 200);
  await post(api, `/api/v1/tasks/${zebra}/close`, { resolution: 'completed' }, 200);
  await post(api, `/api/v1/tasks/${beta}/done`, {}, 200);
  await post(api, `/api/v1/tasks/${gamma}/done`, {}, 200);
  return { slug, refs: [mango, apple, zebra, cherry], needs: [beta, gamma] };
}

const tableRefs = (page: Page) => page.getByTestId('task-ref').allTextContents();
const needsRefs = (page: Page) => page.getByTestId('needs-ref').allTextContents();
const nav = (page: Page) => page.getByRole('navigation', { name: 'Main' });
const goTo = async (page: Page, label: string) => {
  await nav(page).getByRole('link', { name: label }).click();
};
const endsWith = (fragment: string) => new RegExp(`${fragment.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`);
const th = (page: Page, column: string): Locator => page.locator('th', { has: page.getByTestId(`sort-${column}`) });
const saved = async (page: Page) => JSON.parse((await page.evaluate((k) => localStorage.getItem(k), KEY)) as string) as { v: number; query: string };

// Opens an initiative's page through the Initiatives list, the way a person does: the link has no query.
async function openFromList(page: Page, slug: string) {
  await goTo(page, 'Initiatives');
  await page.getByTestId('initiative-filter').selectOption('all');
  await page.locator(`[data-testid=initiative-row][data-slug=${slug}] a`).first().click();
  await expect(page.getByTestId('initiative-title')).toBeVisible();
}

// The reads of one initiative's tasks a page sends for its table (the Needs attention section's read carries `attention=true`).
function taskReads(page: Page, slug: string) {
  const reads = { count: 0 };
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (url.pathname === '/api/v1/tasks' && request.method() === 'GET' && url.searchParams.get('initiative') === slug && !url.searchParams.has('attention')) reads.count++;
  });
  return reads;
}

async function sortBy(page: Page, phone: boolean, column: string, dir: 'asc' | 'desc' | null) {
  if (phone) {
    await page.getByTestId('sort-select').selectOption(dir === null ? '' : `${column}:${dir}`);
  } else {
    // The header cycles none -> ascending -> descending -> none: click until the target is reached.
    const cycle = ['none', 'ascending', 'descending'];
    const now = cycle.indexOf((await th(page, column).getAttribute('aria-sort')) ?? 'none');
    const target = cycle.indexOf(dir === 'asc' ? 'ascending' : dir === 'desc' ? 'descending' : 'none');
    for (let i = 0; i < (target - now + 3) % 3; i++) await page.getByTestId(`sort-${column}`).click();
  }
}

for (const vp of viewports) {
  const phone = vp.name === 'phone';
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('sorting by Flags orders the rows by their badges, the unflagged row last in both directions, and the sort is kept in the fragment', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const slug = `e2e-ip-flags-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug, title: `Flags ${slug}` }, 201);
      const task = async (title: string, extra: object = {}) => (await post(api, '/api/v1/tasks', { initiative: slug, title, ...extra }, 201)).ref;
      // Creation order: plain (no flag), leased (active), blocked by the leased one (blocked).
      const plain = await task('Plain');
      const leased = await task('Leased');
      const blocked = await task('Blocked', { blocked_by: [leased] });
      await post(api, '/api/v1/leases', { task: leased, intent: 'flags sort', resources: [`repo:${slug}`] }, 201);

      await page.goto(`/#/i/${slug}`);
      await expect.poll(() => tableRefs(page)).toEqual([plain, leased, blocked]);
      await expect(th(page, 'flags')).toHaveAttribute('aria-sort', 'none');
      await sortBy(page, phone, 'flags', 'asc');
      await expect(th(page, 'flags')).toHaveAttribute('aria-sort', 'ascending');
      await expect.poll(() => tableRefs(page)).toEqual([leased, blocked, plain]);
      await expect(page).toHaveURL(/sort=flags&dir=asc$/);
      await sortBy(page, phone, 'flags', 'desc');
      await expect(th(page, 'flags')).toHaveAttribute('aria-sort', 'descending');
      await expect.poll(() => tableRefs(page)).toEqual([blocked, leased, plain]);
      await expect(page).toHaveURL(/sort=flags&dir=desc$/);
      if (phone) await expect(page.getByTestId('sort-select')).toHaveValue('flags:desc');
      await page.reload();
      await expect.poll(() => tableRefs(page)).toEqual([blocked, leased, plain]);
      await expect(th(page, 'flags')).toHaveAttribute('aria-sort', 'descending');
      await sortBy(page, phone, 'flags', null);
      await expect(th(page, 'flags')).toHaveAttribute('aria-sort', 'none');
      await expect.poll(() => tableRefs(page)).toEqual([plain, leased, blocked]);
      await api.dispose();
    });

    test('sorting by one column of each type (ref, text, kind, status) orders the rows and sets aria-sort, and the third step is the default', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const { slug, refs, needs } = await seed(api, `e2e-ip-sort-${vp.name}`);
      const [mango, apple, zebra, cherry] = refs as [string, string, string, string];

      await page.goto(`/#/i/${slug}?status=all`);
      await expect.poll(() => tableRefs(page)).toEqual(refs);
      const byDefault = await tableRefs(page);
      const needsByDefault = await needsRefs(page);
      expect([...needsByDefault].sort()).toEqual([...needs].sort());
      if (phone) await expect(page.getByTestId('sort-select')).toBeVisible();
      else await expect(page.getByTestId('sort-select')).toBeHidden();
      for (const column of ['ref', 'title', 'kind', 'status', 'flags', 'holder']) await expect(th(page, column)).toHaveAttribute('aria-sort', 'none');

      // ref: numerically, so descending reverses the creation order.
      await sortBy(page, phone, 'ref', 'desc');
      await expect(th(page, 'ref')).toHaveAttribute('aria-sort', 'descending');
      await expect.poll(() => tableRefs(page)).toEqual([cherry, zebra, apple, mango]);
      await sortBy(page, phone, 'ref', 'asc');
      await expect(th(page, 'ref')).toHaveAttribute('aria-sort', 'ascending');
      await expect.poll(() => tableRefs(page)).toEqual([mango, apple, zebra, cherry]);
      await expect(page).toHaveURL(endsWith(`#/i/${slug}?status=all&sort=ref&dir=asc`));

      // text: title ascending ignores case, descending reverses it.
      await sortBy(page, phone, 'title', 'asc');
      await expect(th(page, 'title')).toHaveAttribute('aria-sort', 'ascending');
      await expect(th(page, 'ref')).toHaveAttribute('aria-sort', 'none');
      await expect.poll(() => tableRefs(page)).toEqual([apple, cherry, mango, zebra]);
      await sortBy(page, phone, 'title', 'desc');
      await expect(th(page, 'title')).toHaveAttribute('aria-sort', 'descending');
      await expect.poll(() => tableRefs(page)).toEqual([zebra, mango, cherry, apple]);

      // kind: work before watch ascending (ties keep the default order), watch first descending.
      await sortBy(page, phone, 'kind', 'asc');
      await expect(th(page, 'kind')).toHaveAttribute('aria-sort', 'ascending');
      await expect.poll(() => tableRefs(page)).toEqual([mango, zebra, cherry, apple]);
      await sortBy(page, phone, 'kind', 'desc');
      await expect(th(page, 'kind')).toHaveAttribute('aria-sort', 'descending');
      await expect.poll(() => tableRefs(page)).toEqual([apple, mango, zebra, cherry]);

      // status: open before closed ascending, closed first descending.
      await sortBy(page, phone, 'status', 'asc');
      await expect(th(page, 'status')).toHaveAttribute('aria-sort', 'ascending');
      await expect.poll(() => tableRefs(page)).toEqual([mango, apple, cherry, zebra]);
      await sortBy(page, phone, 'status', 'desc');
      await expect(th(page, 'status')).toHaveAttribute('aria-sort', 'descending');
      await expect.poll(() => tableRefs(page)).toEqual([zebra, mango, apple, cherry]);
      await expect(page).toHaveURL(endsWith(`#/i/${slug}?status=all&sort=status&dir=desc`));

      // Needs attention keeps its own order whatever the table does.
      expect(await needsRefs(page)).toEqual(needsByDefault);

      // Third step: back to the default order; the sort is gone from the fragment and from the saved query.
      await sortBy(page, phone, 'status', null);
      await expect(th(page, 'status')).toHaveAttribute('aria-sort', 'none');
      await expect.poll(() => tableRefs(page)).toEqual(byDefault);
      await expect(page).toHaveURL(endsWith(`#/i/${slug}?status=all`));
      expect((await saved(page)).query).toBe('status=all');
      if (phone) await expect(page.getByTestId('sort-select')).toHaveValue('');
      await api.dispose();
    });

    test('a sort set on one initiative is restored on nav back and on a second initiative, with one read each', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const a = await seed(api, `e2e-ip-a-${vp.name}`);
      const b = await seed(api, `e2e-ip-b-${vp.name}`);
      const [mangoA, appleA, zebraA, cherryA] = a.refs as [string, string, string, string];
      const [mangoB, appleB, zebraB, cherryB] = b.refs as [string, string, string, string];

      await page.goto(`/#/i/${a.slug}`);
      await expect(page.getByTestId('task-filter')).toHaveValue('not-closed');
      await page.getByTestId('task-filter').selectOption('all');
      await expect.poll(() => tableRefs(page)).toEqual(a.refs);
      await sortBy(page, phone, 'title', 'desc');
      await expect.poll(() => tableRefs(page)).toEqual([zebraA, mangoA, cherryA, appleA]);
      const query = 'status=all&sort=title&dir=desc';
      await expect(page).toHaveURL(endsWith(`#/i/${a.slug}?${query}`));
      expect(await saved(page)).toEqual({ v: 1, query });

      // Nav away and back: the Initiatives list's link carries no query, and the page opens as it was left.
      await goTo(page, 'Tasks');
      await expect(page.getByRole('heading', { level: 2, name: 'Tasks' })).toBeVisible();
      await goTo(page, 'Initiatives');
      const readsA = taskReads(page, a.slug);
      await page.locator(`[data-testid=initiative-row][data-slug=${a.slug}] a`).first().click();
      await expect(page.getByTestId('initiative-title')).toBeVisible();
      await expect.poll(() => tableRefs(page)).toEqual([zebraA, mangoA, cherryA, appleA]);
      await expect(page).toHaveURL(endsWith(`#/i/${a.slug}?${query}`));
      await expect(page.getByTestId('task-filter')).toHaveValue('all');
      await expect(th(page, 'title')).toHaveAttribute('aria-sort', 'descending');
      if (phone) await expect(page.getByTestId('sort-select')).toHaveValue('title:desc');
      expect(readsA.count).toBe(1);

      // A second initiative opens with the same sort and filter (the one key every initiative shares).
      await goTo(page, 'Initiatives');
      const readsB = taskReads(page, b.slug);
      await page.getByTestId('initiative-filter').selectOption('all');
      await page.locator(`[data-testid=initiative-row][data-slug=${b.slug}] a`).first().click();
      await expect(page.getByTestId('initiative-title')).toBeVisible();
      await expect.poll(() => tableRefs(page)).toEqual([zebraB, mangoB, cherryB, appleB]);
      await expect(page).toHaveURL(endsWith(`#/i/${b.slug}?${query}`));
      await expect(page.getByTestId('task-filter')).toHaveValue('all');
      await expect(th(page, 'title')).toHaveAttribute('aria-sort', 'descending');
      expect(readsB.count).toBe(1);

      // A change on the second initiative is what the first one then opens with.
      await sortBy(page, phone, 'kind', 'desc');
      await expect.poll(() => tableRefs(page)).toEqual([appleB, mangoB, zebraB, cherryB]);
      await openFromList(page, a.slug);
      await expect.poll(() => tableRefs(page)).toEqual([appleA, mangoA, zebraA, cherryA]);
      await expect(th(page, 'kind')).toHaveAttribute('aria-sort', 'descending');

      // A new page of the same context opens the bare fragment and gets the same state.
      const second = await page.context().newPage();
      await second.goto(`/#/i/${b.slug}`);
      await expect(second.getByTestId('initiative-title')).toBeVisible();
      await expect.poll(() => tableRefs(second)).toEqual([appleB, mangoB, zebraB, cherryB]);
      await expect(second).toHaveURL(endsWith(`#/i/${b.slug}?status=all&sort=kind&dir=desc`));
      await expect(th(second, 'kind')).toHaveAttribute('aria-sort', 'descending');
      await second.close();
      await api.dispose();
    });

    test('a fragment with its own query overrides the saved one, and Clear filters then nav away and back shows the defaults', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const s = await seed(api, `e2e-ip-own-${vp.name}`);
      const [mango, apple, zebra, cherry] = s.refs as [string, string, string, string];

      await page.goto(`/#/i/${s.slug}?status=all&sort=title&dir=asc`);
      await expect.poll(() => tableRefs(page)).toEqual([apple, cherry, mango, zebra]);

      // A link with its own query wins and is what is saved from now on.
      const link = await page.context().newPage();
      await link.goto(`/#/i/${s.slug}?status=closed&sort=ref&dir=desc`);
      await expect(link.getByTestId('task-filter')).toHaveValue('closed');
      await expect(th(link, 'ref')).toHaveAttribute('aria-sort', 'descending');
      await expect.poll(() => tableRefs(link)).toEqual([zebra]);
      expect((await saved(link)).query).toBe('status=closed&sort=ref&dir=desc');

      // Clear filters: the defaults at once, nothing saved, and the defaults again after nav away and back.
      await link.getByTestId('clear-filters').click();
      await expect(link).toHaveURL(new RegExp(`#/i/${s.slug}$`));
      await expect(link.getByTestId('task-filter')).toHaveValue('not-closed');
      await expect(th(link, 'ref')).toHaveAttribute('aria-sort', 'none');
      await expect(link.getByTestId('clear-filters')).toHaveCount(0);
      expect(await link.evaluate((k) => localStorage.getItem(k), KEY)).toBeNull();
      await expect.poll(() => tableRefs(link)).toEqual([mango, apple, cherry]);
      await openFromList(link, s.slug);
      await expect(link).toHaveURL(new RegExp(`#/i/${s.slug}$`));
      await expect(link.getByTestId('task-filter')).toHaveValue('not-closed');
      await expect(th(link, 'ref')).toHaveAttribute('aria-sort', 'none');
      if (phone) await expect(link.getByTestId('sort-select')).toHaveValue('');
      expect(await link.evaluate((k) => localStorage.getItem(k), KEY)).toBeNull();
      await link.close();
      await api.dispose();
    });

    test('a saved query with an unknown status or column opens at the defaults', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const s = await seed(api, `e2e-ip-bad-${vp.name}`);
      await page.goto('/#/attention');
      await expect(page.getByTestId('section-open_asks')).toBeVisible();
      await page.evaluate(([k, query]) => localStorage.setItem(k, JSON.stringify({ v: 1, query })), [KEY, 'status=bogus&sort=bogus&dir=asc'] as const);
      const fresh = await page.context().newPage();
      await fresh.goto(`/#/i/${s.slug}`);
      await expect.poll(() => tableRefs(fresh)).toHaveLength(3);
      await expect(fresh.getByTestId('task-filter')).toHaveValue('not-closed');
      for (const column of ['ref', 'title', 'kind', 'status', 'flags', 'holder']) await expect(th(fresh, column)).toHaveAttribute('aria-sort', 'none');
      await fresh.close();
      await api.dispose();
    });

    if (!phone) {
      test('a header sorts from the keyboard: Enter and Space, asc then desc then the default', async ({ page, playwright, baseURL }) => {
        const api = await apiContext(playwright, baseURL);
        const s = await seed(api, `e2e-ip-keys-${vp.name}`);
        const [mango, apple, zebra, cherry] = s.refs as [string, string, string, string];
        await page.goto(`/#/i/${s.slug}?status=all`);
        await expect.poll(() => tableRefs(page)).toHaveLength(4);
        const byDefault = await tableRefs(page);

        const title = page.getByTestId('sort-title');
        await title.focus();
        await expect(title).toBeFocused();
        await page.keyboard.press('Enter');
        await expect(th(page, 'title')).toHaveAttribute('aria-sort', 'ascending');
        await expect.poll(() => tableRefs(page)).toEqual([apple, cherry, mango, zebra]);
        await expect(title).toBeFocused();
        await page.keyboard.press('Space');
        await expect(th(page, 'title')).toHaveAttribute('aria-sort', 'descending');
        await expect.poll(() => tableRefs(page)).toEqual([zebra, mango, cherry, apple]);
        await page.keyboard.press('Enter');
        await expect(th(page, 'title')).toHaveAttribute('aria-sort', 'none');
        await expect.poll(() => tableRefs(page)).toEqual(byDefault);
        await api.dispose();
      });
    }
  });
}
