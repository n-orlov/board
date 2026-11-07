import { expect, test as base, type APIRequestContext, type Page } from '@playwright/test';

// Signs in through the mock provider, so no API key rides along (see login.spec.ts).
// The initiative page `#/i/<slug>` and the Initiatives list's links, counts, live leases and status filter,
// at the phone and the desktop size.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;

const row = (page: Page, slug: string) => page.locator(`[data-testid=initiative-row][data-slug=${slug}]`);
const taskRow = (page: Page, ref: string) => page.locator(`[data-testid=task-row][data-ref=${ref}]`);
const shownRefs = (page: Page) => page.getByTestId('task-ref').allTextContents();

async function post(api: APIRequestContext, path: string, data: object, status: number) {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
  return (await response.json()) as { ref: string };
}

// An open initiative with five tasks (open and leased, done, closed, open and blocked by the leased one,
// open with a second lease) and a closed one; returns the slugs.
async function seed(api: APIRequestContext, prefix: string) {
  const open = `${prefix}-open`;
  const closed = `${prefix}-closed`;
  await post(api, '/api/v1/initiatives', { slug: closed, title: `Closed ${prefix}` }, 201);
  await post(api, `/api/v1/initiatives/${closed}/close`, {}, 200);
  await post(
    api,
    '/api/v1/initiatives',
    {
      slug: open,
      title: `Open ${prefix}`,
      description: 'Plan with **bold** words and <b>raw html</b>.\n\n- first point\n- second point',
      tags: ['web', 'e2e'],
      target_start: '2026-03-01',
      target_end: '2026-04-15',
    },
    201,
  );
  const task = (title: string, extra: object = {}) => post(api, '/api/v1/tasks', { initiative: open, title, ...extra }, 201);
  const leased = await task('Leased work');
  const done = await task('Finished work');
  const dropped = await task('Dropped work');
  const blocked = await task('Blocked work', { blocked_by: [leased.ref] });
  await post(api, `/api/v1/leases`, { task: leased.ref, intent: 'working on it' }, 201);
  await post(api, `/api/v1/tasks/${done.ref}/done`, {}, 200);
  await post(api, `/api/v1/tasks/${dropped.ref}/close`, { resolution: 'dropped' }, 200);
  return { open, closed, leased: leased.ref, done: done.ref, dropped: dropped.ref, blocked: blocked.ref };
}

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });
    let seeded: Awaited<ReturnType<typeof seed>>;

    test.beforeAll(async ({ playwright }, info) => {
      const api = await playwright.request.newContext({
        baseURL: info.project.use.baseURL,
        extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
      });
      seeded = await seed(api, `e2e-ip-${vp.name}`);
      await api.dispose();
    });

    test('an initiative opens from the list into its page with the details, counts and tasks', async ({ page }) => {
      await page.goto('/');
      const listed = row(page, seeded.open);
      await expect(listed).toHaveCount(1);
      await expect(listed.getByTestId('initiative-counts')).toHaveText('2 open · 1 done · 1 closed');
      await expect(listed.getByTestId('initiative-leases')).toHaveText('1 live lease');
      await listed.getByRole('link', { name: `Open e2e-ip-${vp.name}` }).click();

      await expect(page).toHaveURL(new RegExp(`#/i/${seeded.open}$`));
      await expect(page.getByRole('heading', { level: 2, name: `Open e2e-ip-${vp.name}` })).toBeVisible();
      await expect(page.getByTestId('initiative-slug')).toHaveText(seeded.open);
      await expect(page.getByTestId('initiative-status')).toHaveText('open');
      await expect(page.getByTestId('tag')).toHaveText(['e2e', 'web']);
      await expect(page.getByTestId('initiative-targets')).toHaveText('Targets: 2026-03-01 to 2026-04-15');
      // The description is rendered Markdown, and raw HTML in it is text.
      const description = page.getByTestId('initiative-description');
      await expect(description.locator('strong')).toHaveText('bold');
      await expect(description.locator('li')).toHaveText(['first point', 'second point']);
      await expect(description.locator('b')).toHaveCount(0);
      await expect(description).toContainText('<b>raw html</b>');
      // The counts cover every task whatever the table shows.
      await expect(page.getByTestId('count-open')).toHaveText('2 open');
      await expect(page.getByTestId('count-done')).toHaveText('1 done');
      await expect(page.getByTestId('count-closed')).toHaveText('1 closed');
      await expect(page.getByTestId('initiative-page').getByTestId('initiative-leases')).toHaveText('1 live lease');

      // The tasks table starts on "not closed": open and done tasks.
      await expect(page.getByTestId('task-filter')).toHaveValue('not-closed');
      await expect(page.getByTestId('task-row')).toHaveCount(3);
      const leased = taskRow(page, seeded.leased);
      await expect(leased.getByTestId('task-title')).toHaveText('Leased work');
      await expect(leased.getByTestId('task-kind')).toHaveText('work');
      await expect(leased.getByTestId('task-status')).toHaveText('open');
      await expect(leased.getByTestId('flag-active')).toHaveText('active');
      await expect(leased.getByTestId('task-holder')).toHaveText('playwright');
      await expect(taskRow(page, seeded.blocked).getByTestId('flag-blocked')).toHaveText('blocked');
      await expect(taskRow(page, seeded.done).getByTestId('task-status')).toHaveText('done');
      await expect(taskRow(page, seeded.dropped)).toHaveCount(0);

      const { scrollWidth, clientWidth } = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      }));
      expect(scrollWidth).toBeLessThanOrEqual(clientWidth);

      // Back returns to the list, and the page survives a reload on its own address.
      await page.getByTestId('back-link').click();
      await expect(page.getByRole('heading', { level: 2, name: 'Initiatives' })).toBeVisible();
      await page.goto(`/#/i/${seeded.open}`);
      await page.reload();
      await expect(page.getByTestId('initiative-title')).toHaveText(`Open e2e-ip-${vp.name}`);
    });

    test('the task filter offers not closed, done, closed and all, and shows the tasks each keeps', async ({ page }) => {
      await page.goto(`/#/i/${seeded.open}`);
      const filter = page.getByRole('combobox', { name: 'Tasks' });
      await expect(filter.locator('option')).toHaveText(['Not closed', 'Done', 'Closed', 'All']);
      expect(await filter.locator('option').evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value))).toEqual([
        'not-closed',
        'done',
        'closed',
        'all',
      ]);

      await expect(page.getByTestId('task-row')).toHaveCount(3);
      expect((await shownRefs(page)).sort()).toEqual([seeded.leased, seeded.done, seeded.blocked].sort());

      await filter.selectOption('done');
      expect(await shownRefs(page)).toEqual([seeded.done]);
      await filter.selectOption('closed');
      expect(await shownRefs(page)).toEqual([seeded.dropped]);
      await expect(taskRow(page, seeded.dropped).getByTestId('task-status')).toHaveText('closed');
      await filter.selectOption('all');
      expect((await shownRefs(page)).sort()).toEqual([seeded.leased, seeded.done, seeded.dropped, seeded.blocked].sort());
      await filter.selectOption('not-closed');
      await expect(page.getByTestId('task-row')).toHaveCount(3);
      // The counts above the table do not follow the filter.
      await expect(page.getByTestId('count-closed')).toHaveText('1 closed');
    });

    test('the Initiatives status filter shows the open ones, then the closed ones, then all', async ({ page }) => {
      await page.goto('/');
      const filter = page.getByRole('combobox', { name: 'Status' });
      expect(await filter.locator('option').evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value))).toEqual(['open', 'closed', 'all']);

      await filter.selectOption('open');
      await expect(row(page, seeded.open)).toHaveCount(1);
      await expect(row(page, seeded.closed)).toHaveCount(0);

      await filter.selectOption('closed');
      await expect(row(page, seeded.closed)).toHaveCount(1);
      await expect(row(page, seeded.open)).toHaveCount(0);
      await expect(row(page, seeded.closed).getByTestId('initiative-status')).toHaveText('closed');
      await expect(row(page, seeded.closed).getByTestId('initiative-counts')).toHaveText('No tasks');

      await filter.selectOption('all');
      await expect(row(page, seeded.open)).toHaveCount(1);
      await expect(row(page, seeded.closed)).toHaveCount(1);
    });
  });
}
