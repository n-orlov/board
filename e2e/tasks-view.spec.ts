import { expect, test, type APIRequestContext, type Locator, type Page, type PlaywrightTestArgs } from '@playwright/test';

// The Tasks view's sort and saved view state (PRD R16, SPEC 11): the table sorts by its columns through the shared header
// (desktop) and the Sort select (phone), the sort rides in the fragment query next to the filters, the whole query is
// saved under `board.view.v1.tasks` and restored on a bare `#/tasks` with one fetch, a fragment with its own query wins,
// Clear filters forgets it all, and an initiative the board no longer has is dropped from a restored query. Each test
// seeds initiatives of its own on the shared board and filters the view down to them.
const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const KEY = 'board.view.v1.tasks';

async function post<T = { ref: string }>(api: APIRequestContext, path: string, data: object, status: number): Promise<T> {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
  return (await response.json()) as T;
}

const apiContext = (playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined) =>
  playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' } });

interface Seed {
  slug: string;
  /** Titles Mango, apple, Zebra, cherry (in that order), then two done tasks that need attention. */
  refs: string[];
  needs: string[];
}

// Four table rows: S-1 Mango (work, open), S-2 apple (watch, open), S-3 Zebra (work, closed), S-4 cherry (work, open); and two done
// tasks, which sit in Needs attention above the table, not in it.
async function seed(api: APIRequestContext, slug: string): Promise<Seed> {
  await post(api, '/api/v1/initiatives', { slug, title: `Tasks view ${slug}` }, 201);
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

// The `GET /tasks` requests a page sends: the table's own, and the Needs attention section's (attention=true).
function taskReads(page: Page) {
  const reads = { table: 0, needs: 0 };
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (url.pathname !== '/api/v1/tasks') return;
    if (url.searchParams.get('attention') === 'true') reads.needs++;
    else reads.table++;
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
      const slug = `e2e-tv-flags-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug, title: `Flags ${slug}` }, 201);
      const task = async (title: string, extra: object = {}) => (await post(api, '/api/v1/tasks', { initiative: slug, title, ...extra }, 201)).ref;
      // Creation order: plain (no flag), leased (active), blocked by the leased one (blocked).
      const plain = await task('Plain');
      const leased = await task('Leased');
      const blocked = await task('Blocked', { blocked_by: [leased] });
      await post(api, '/api/v1/leases', { task: leased, intent: 'flags sort', resources: [`repo:${slug}`] }, 201);

      await page.goto(`/#/tasks?initiative=${slug}&status=all`);
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

    test('two filters and a sort survive Initiatives and back, and a new page of the same context, with one fetch each', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const { slug, refs } = await seed(api, `e2e-tv-restore-${vp.name}`);
      const [mango, apple, , cherry] = refs;

      await page.goto('/#/tasks');
      await expect(page.getByTestId('status-filter')).toHaveValue('not-closed');
      await page.getByTestId('initiative-filter').selectOption(slug);
      await page.getByTestId('status-filter').selectOption('all');
      await expect.poll(() => tableRefs(page)).toEqual([mango, apple, refs[2], cherry]);
      await sortBy(page, phone, 'title', 'desc');
      // Title descending, ignoring case: Zebra, Mango, cherry, apple.
      const expected = [refs[2], mango, cherry, apple];
      await expect.poll(() => tableRefs(page)).toEqual(expected);
      const hash = `#/tasks?status=all&initiative=${slug}&sort=title&dir=desc`;
      await expect(page).toHaveURL(endsWith(hash));
      expect(JSON.parse((await page.evaluate((k) => localStorage.getItem(k), KEY)) as string)).toEqual({ v: 1, query: hash.slice('#/tasks?'.length) });

      // Away through the nav and back through the nav: the bare route restores the state with one fetch of each list.
      await goTo(page, 'Initiatives');
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
      const reads = taskReads(page);
      await goTo(page, 'Tasks');
      await expect(page.getByTestId('task-row').first()).toBeVisible();
      await expect.poll(() => tableRefs(page)).toEqual(expected);
      await expect(page).toHaveURL(endsWith(hash));
      await expect(page.getByTestId('initiative-filter')).toHaveValue(slug);
      await expect(page.getByTestId('status-filter')).toHaveValue('all');
      await expect(th(page, 'title')).toHaveAttribute('aria-sort', 'descending');
      if (phone) await expect(page.getByTestId('sort-select')).toHaveValue('title:desc');
      expect(reads).toEqual({ table: 1, needs: 1 });

      // A new page of the same context opens `#/tasks` bare and gets the same state.
      const second = await page.context().newPage();
      const secondReads = taskReads(second);
      await second.goto('/#/tasks');
      await expect(second.getByTestId('task-row').first()).toBeVisible();
      await expect.poll(() => tableRefs(second)).toEqual(expected);
      await expect(second).toHaveURL(endsWith(hash));
      await expect(second.getByTestId('initiative-filter')).toHaveValue(slug);
      await expect(second.getByTestId('status-filter')).toHaveValue('all');
      await expect(th(second, 'title')).toHaveAttribute('aria-sort', 'descending');
      if (phone) await expect(second.getByTestId('sort-select')).toHaveValue('title:desc');
      expect(secondReads).toEqual({ table: 1, needs: 1 });
      await second.close();
      await api.dispose();
    });

    test('a fragment with its own query overrides the saved one, and Clear filters then nav away and back shows the defaults', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const a = await seed(api, `e2e-tv-own-a-${vp.name}`);
      const b = await seed(api, `e2e-tv-own-b-${vp.name}`);

      // Something saved first: initiative b, sorted by title.
      await page.goto(`/#/tasks?initiative=${b.slug}&sort=title&dir=asc`);
      await expect(page.getByTestId('initiative-filter')).toHaveValue(b.slug);
      await expect.poll(() => tableRefs(page)).toEqual([b.refs[1], b.refs[3], b.refs[0]]); // apple, cherry, Mango (Zebra is closed)

      // A new page opens a link with its own query: that one wins, and it is what is saved from now on.
      const link = await page.context().newPage();
      await link.goto(`/#/tasks?initiative=${a.slug}&kind=watch&status=all&sort=ref&dir=desc`);
      await expect(link.getByTestId('initiative-filter')).toHaveValue(a.slug);
      await expect(link.getByTestId('kind-filter')).toHaveValue('watch');
      await expect(th(link, 'ref')).toHaveAttribute('aria-sort', 'descending');
      await expect.poll(() => tableRefs(link)).toEqual([a.refs[1]]);
      expect(JSON.parse((await link.evaluate((k) => localStorage.getItem(k), KEY)) as string).query).toBe(`initiative=${a.slug}&kind=watch&status=all&sort=ref&dir=desc`) // saved as the link wrote it;

      // Away and back restores the link's query, not the earlier one.
      await goTo(link, 'Initiatives');
      await expect(link.getByTestId('initiative-row').first()).toBeVisible();
      await goTo(link, 'Tasks');
      await expect(link.getByTestId('initiative-filter')).toHaveValue(a.slug);
      await expect(link.getByTestId('kind-filter')).toHaveValue('watch');
      await expect.poll(() => tableRefs(link)).toEqual([a.refs[1]]);

      // Clear filters: the defaults at once, nothing saved, and the defaults again after nav away and back.
      await link.getByTestId('clear-filters').click();
      await expect(link).toHaveURL(/#\/tasks$/);
      await expect(link.getByTestId('initiative-filter')).toHaveValue('');
      await expect(link.getByTestId('kind-filter')).toHaveValue('');
      await expect(link.getByTestId('status-filter')).toHaveValue('not-closed');
      await expect(link.getByTestId('clear-filters')).toHaveCount(0);
      await expect(th(link, 'ref')).toHaveAttribute('aria-sort', 'none');
      expect(await link.evaluate((k) => localStorage.getItem(k), KEY)).toBeNull();
      await goTo(link, 'Initiatives');
      await expect(link.getByTestId('initiative-row').first()).toBeVisible();
      await goTo(link, 'Tasks');
      await expect(link.getByTestId('task-row').first()).toBeVisible();
      await expect(link).toHaveURL(/#\/tasks$/);
      await expect(link.getByTestId('initiative-filter')).toHaveValue('');
      await expect(link.getByTestId('kind-filter')).toHaveValue('');
      await expect(link.getByTestId('status-filter')).toHaveValue('not-closed');
      await expect(th(link, 'ref')).toHaveAttribute('aria-sort', 'none');
      if (phone) await expect(link.getByTestId('sort-select')).toHaveValue('');
      expect(await link.evaluate((k) => localStorage.getItem(k), KEY)).toBeNull();
      await link.close();
      await api.dispose();
    });

    test('a restored initiative the board does not have is dropped, the other restored values stay', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const { slug } = await seed(api, `e2e-tv-drop-${vp.name}`);
      await page.goto('/#/attention');
      await expect(page.getByTestId('section-open_asks')).toBeVisible();
      await page.evaluate(
        ([k, query]) => localStorage.setItem(k, JSON.stringify({ v: 1, query })),
        [KEY, 'initiative=e2e-tv-ghost&kind=watch&status=bogus&sort=title&dir=asc'] as const,
      );

      const fresh = await page.context().newPage();
      const reads = taskReads(fresh);
      await fresh.goto('/#/tasks');
      await expect(fresh.getByTestId('status-filter')).toHaveValue('not-closed'); // the unknown status is the default
      await expect(fresh.getByTestId('initiative-filter')).toHaveValue('');
      await expect(fresh.getByTestId('kind-filter')).toHaveValue('watch');
      await expect(th(fresh, 'title')).toHaveAttribute('aria-sort', 'ascending');
      await expect(fresh).toHaveURL(/#\/tasks\?kind=watch&sort=title&dir=asc$/);
      await expect(fresh.getByTestId('task-row').first()).toBeVisible();
      expect(reads.table).toBe(1);
      expect(JSON.parse((await fresh.evaluate((k) => localStorage.getItem(k), KEY)) as string).query).toBe('kind=watch&sort=title&dir=asc');
      // The seeded initiative is still offered (the select lists real initiatives only).
      await expect(fresh.getByTestId('initiative-filter').locator(`option[value="${slug}"]`)).toHaveCount(1);
      await expect(fresh.getByTestId('initiative-filter').locator('option[value="e2e-tv-ghost"]')).toHaveCount(0);
      await fresh.close();
      await api.dispose();
    });

    test('sorting by a column of each type orders the rows and sets aria-sort, and the Needs attention section keeps its order', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const { slug, refs, needs } = await seed(api, `e2e-tv-sort-${vp.name}`);
      const [mango, apple, zebra, cherry] = refs;
      const byDefault = [mango, apple, zebra, cherry];

      await page.goto(`/#/tasks?initiative=${slug}&status=all`);
      await expect.poll(() => tableRefs(page)).toEqual(byDefault);
      const needsBefore = await needsRefs(page);
      expect([...needsBefore].sort()).toEqual([...needs].sort());
      if (phone) {
        await expect(page.getByTestId('sort-select')).toBeVisible();
      } else {
        await expect(page.getByTestId('sort-select')).toBeHidden();
      }
      for (const column of ['ref', 'title', 'initiative', 'kind', 'status', 'flags', 'holder']) await expect(th(page, column)).toHaveAttribute('aria-sort', 'none');

      // ref (numbers compare as numbers): the first step is ascending, the second descending, the third the default.
      await sortBy(page, phone, 'ref', 'desc');
      await expect(th(page, 'ref')).toHaveAttribute('aria-sort', 'descending');
      await expect.poll(() => tableRefs(page)).toEqual([cherry, zebra, apple, mango]);
      await expect(page).toHaveURL(/sort=ref&dir=desc$/);

      // text: title ascending ignores case.
      await sortBy(page, phone, 'title', 'asc');
      await expect(th(page, 'title')).toHaveAttribute('aria-sort', 'ascending');
      await expect(th(page, 'ref')).toHaveAttribute('aria-sort', 'none');
      await expect.poll(() => tableRefs(page)).toEqual([apple, cherry, mango, zebra]);
      await expect(page).toHaveURL(/sort=title&dir=asc$/);

      // status: open before closed ascending, closed first descending; ties keep the default order.
      await sortBy(page, phone, 'status', 'asc');
      await expect(th(page, 'status')).toHaveAttribute('aria-sort', 'ascending');
      await expect.poll(() => tableRefs(page)).toEqual([mango, apple, cherry, zebra]);
      await sortBy(page, phone, 'status', 'desc');
      await expect(th(page, 'status')).toHaveAttribute('aria-sort', 'descending');
      await expect.poll(() => tableRefs(page)).toEqual([zebra, mango, apple, cherry]);

      // kind: work before watch ascending, watch first descending.
      await sortBy(page, phone, 'kind', 'asc');
      await expect(th(page, 'kind')).toHaveAttribute('aria-sort', 'ascending');
      await expect.poll(() => tableRefs(page)).toEqual([mango, zebra, cherry, apple]);
      await sortBy(page, phone, 'kind', 'desc');
      await expect(th(page, 'kind')).toHaveAttribute('aria-sort', 'descending');
      await expect.poll(() => tableRefs(page)).toEqual([apple, mango, zebra, cherry]);
      expect(await needsRefs(page)).toEqual(needsBefore);

      // Back to the default order, and the sort is gone from the fragment.
      await sortBy(page, phone, 'kind', null);
      await expect.poll(() => tableRefs(page)).toEqual(byDefault);
      await expect(th(page, 'kind')).toHaveAttribute('aria-sort', 'none');
      await expect(page).toHaveURL(endsWith(`#/tasks?status=all&initiative=${slug}`));
      if (phone) await expect(page.getByTestId('sort-select')).toHaveValue('');
      await api.dispose();
    });

    if (!phone) {
      test('a header sorts from the keyboard: Enter and Space, asc then desc then the default', async ({ page, playwright, baseURL }) => {
        const api = await apiContext(playwright, baseURL);
        const { slug, refs } = await seed(api, `e2e-tv-keys-${vp.name}`);
        const [mango, apple, zebra, cherry] = refs;
        await page.goto(`/#/tasks?initiative=${slug}&status=all`);
        await expect.poll(() => tableRefs(page)).toEqual([mango, apple, zebra, cherry]);

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
        await expect.poll(() => tableRefs(page)).toEqual([mango, apple, zebra, cherry]);

        // Tab moves on to the next header's button, which sorts the same way.
        await page.keyboard.press('Tab');
        await expect(page.getByTestId('sort-initiative')).toBeFocused();
        await page.keyboard.press('Tab');
        await expect(page.getByTestId('sort-kind')).toBeFocused();
        await page.keyboard.press('Enter');
        await expect(th(page, 'kind')).toHaveAttribute('aria-sort', 'ascending');
        await expect.poll(() => tableRefs(page)).toEqual([mango, zebra, cherry, apple]);
        await api.dispose();
      });
    }
  });
}
