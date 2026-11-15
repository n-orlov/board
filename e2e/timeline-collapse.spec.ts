import { DatabaseSync } from 'node:sqlite';
import { join } from 'node:path';
import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// Which initiatives are collapsed on the Timeline survives Back, Forward and a reload (PRD R8, SPEC 11): it is kept with
// the history entry, so a visit that has no entry of its own (the nav, a new tab) opens with every initiative expanded.
// Each test seeds two initiatives of its own and filters the view to them.
const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const FIVE_SECONDS = { timeout: 5000 };
const DAY = 86_400_000;

async function post<T = Record<string, never>>(api: APIRequestContext, path: string, data: object, status: number): Promise<T> {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
  return (await response.json()) as T;
}

function dateTask(slug: string, ref: string, createdAt: number) {
  const seq = Number(ref.slice(slug.length + 1));
  const db = new DatabaseSync(join(process.env.E2E_WORK!, 'board.db'));
  try {
    db.exec('PRAGMA busy_timeout = 5000');
    const changed = db.prepare('UPDATE tasks SET created_at = ? WHERE seq = ? AND initiative_id = (SELECT id FROM initiatives WHERE slug = ?)').run(createdAt, seq, slug);
    expect(changed.changes).toBe(1);
  } finally {
    db.close();
  }
}

const taskRows = (page: Page) => page.locator('.vis-labelset .vis-label.tl-task');
const initiativeLabel = (page: Page, title: string) => page.locator('.vis-labelset .vis-label.tl-initiative', { hasText: title });
const main = (page: Page) => page.getByRole('navigation', { name: 'Main' });

/** A link of the nav: the phone tab bar keeps only some of them, the rest are under More. */
async function goTo(page: Page, label: string) {
  const link = main(page).getByRole('link', { name: label });
  if (await link.isVisible()) {
    await link.click();
    return;
  }
  await page.getByRole('button', { name: 'More' }).click();
  await page.getByRole('dialog', { name: 'More' }).getByRole('link', { name: label }).click();
}

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('a collapsed initiative stays collapsed over Tasks and Back and a reload, an expanded one stays expanded, and a fresh visit opens everything expanded', async ({ page, playwright, baseURL }) => {
      const first = `e2e-tlc-${vp.name}-a`;
      const second = `e2e-tlc-${vp.name}-b`;
      const api = await playwright.request.newContext({
        baseURL,
        extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': `tlc-${vp.name}` },
      });
      const now = Date.now();
      for (const [slug, title] of [[first, `Collapse ${vp.name} first`], [second, `Collapse ${vp.name} second`]] as const) {
        await post(api, '/api/v1/initiatives', { slug, title }, 201);
        for (let i = 0; i < 2; i++) {
          const { ref } = await post<{ ref: string }>(api, '/api/v1/tasks', { initiative: slug, title: `${title} task ${i}` }, 201);
          dateTask(slug, ref, now - (4 - i) * DAY);
        }
      }

      const expanded = async () => {
        await expect(taskRows(page)).toHaveCount(4, FIVE_SECONDS);
        await expect(initiativeLabel(page, `Collapse ${vp.name} first`)).not.toHaveClass(/collapsed/);
        await expect(initiativeLabel(page, `Collapse ${vp.name} second`)).not.toHaveClass(/collapsed/);
      };
      // The first initiative is collapsed: its two task rows are gone and the second's remain.
      const collapsed = async () => {
        await expect(taskRows(page)).toHaveCount(2, FIVE_SECONDS);
        await expect(taskRows(page).filter({ hasText: `Collapse ${vp.name} second task` })).toHaveCount(2);
        await expect(initiativeLabel(page, `Collapse ${vp.name} first`)).toHaveClass(/collapsed/);
        await expect(initiativeLabel(page, `Collapse ${vp.name} second`)).not.toHaveClass(/collapsed/);
      };

      await page.goto(`/#/timeline?initiative=${first}&initiative=${second}`);
      await expect(page.getByTestId('timeline-view')).toBeVisible();
      await expanded();

      await initiativeLabel(page, `Collapse ${vp.name} first`).click();
      await collapsed();

      // Tasks and Back: the entry the Timeline was left at is shown again, still collapsed.
      await goTo(page, 'Tasks');
      await expect(page).toHaveURL(/#\/tasks/);
      await expect(page.getByTestId('timeline-view')).toHaveCount(0);
      await page.goBack();
      await expect(page.getByTestId('timeline-view')).toBeVisible();
      await collapsed();

      // Forward to Tasks and Back again does the same.
      await page.goForward();
      await expect(page).toHaveURL(/#\/tasks/);
      await page.goBack();
      await collapsed();

      // A reload keeps the entry, so it stays collapsed.
      await page.reload();
      await expect(page.getByTestId('timeline-view')).toBeVisible();
      await collapsed();

      // Expanding it is kept the same way: a reload shows it expanded.
      await initiativeLabel(page, `Collapse ${vp.name} first`).click();
      await expanded();
      await page.reload();
      await expect(page.getByTestId('timeline-view')).toBeVisible();
      await expanded();

      // Collapse it again, leave and come back through the nav: a visit with no state of its own opens expanded.
      await initiativeLabel(page, `Collapse ${vp.name} first`).click();
      await collapsed();
      await goTo(page, 'Tasks');
      await expect(page).toHaveURL(/#\/tasks/);
      await goTo(page, 'Timeline');
      await expect(page.getByTestId('timeline-view')).toBeVisible();
      await expanded();

      // So does a new tab at the bare route, and the first tab's own entry is still collapsed after Back.
      const tab = await page.context().newPage();
      await tab.goto(`/#/timeline?initiative=${first}&initiative=${second}`);
      await expect(tab.getByTestId('timeline-view')).toBeVisible();
      await expect(taskRows(tab)).toHaveCount(4, FIVE_SECONDS);
      await expect(initiativeLabel(tab, `Collapse ${vp.name} first`)).not.toHaveClass(/collapsed/);
      await tab.close();
      await page.goBack();
      await page.goBack();
      await expect(page.getByTestId('timeline-view')).toBeVisible();
      await collapsed();
      await api.dispose();
    });
  });
}
