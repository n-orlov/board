import AxeBuilder from '@axe-core/playwright';
import { expect, test as base, type Page } from '@playwright/test';

// R9/R12: axe-core over the five views, the four dialogs/sheets of the R4 set, the initiative page's Edit and Close dialogs, the New task sheet, the task page and its Add dependency, Milestone and End lease sheets, at the phone and the desktop
// viewport, in the light and the dark theme: 14 states x 2 viewports x 2 themes = 56 scans. No rule is
// disabled and no element is excluded; a serious or critical violation fails the scan with the rule, the
// elements and the contrast data named. Signs in through the mock provider, so no API key rides along.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const themes = ['light', 'dark'] as const;

/** Wait until every running animation or transition has finished, so a scan never reads a mid-fade colour. */
const settle = (page: Page) =>
  page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))).then(() => undefined));

async function scan(page: Page, effectiveTheme: string) {
  await expect(page.locator('html')).toHaveAttribute('data-theme', effectiveTheme);
  await settle(page);
  const { violations } = await new AxeBuilder({ page }).analyze();
  const blocking = violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
  const report = blocking.map((v) => ({
    rule: v.id,
    impact: v.impact,
    help: v.help,
    nodes: v.nodes.map((n) => ({ target: n.target, summary: n.failureSummary })),
  }));
  expect(report, 'serious or critical axe violations').toEqual([]);
}

type State = { name: string; open: (page: Page) => Promise<void> };

const states: State[] = [
  {
    name: 'Initiatives view',
    open: async (page) => {
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
    },
  },
  {
    name: 'Initiative page',
    open: async (page) => {
      await page.getByTestId('initiative-row').locator('[data-testid=initiative-link]').getByText('Accessibility').click();
      await expect(page.getByTestId('task-row')).toHaveCount(1);
      await expect(page.getByTestId('initiative-description').getByRole('link')).toHaveCount(1);
    },
  },
  {
    name: 'edit-initiative sheet',
    open: async (page) => {
      await page.getByTestId('initiative-row').locator('[data-testid=initiative-link]').getByText('Accessibility').click();
      await page.getByRole('button', { name: 'Edit' }).click();
      await expect(page.getByRole('dialog', { name: 'Edit initiative' })).toBeVisible();
    },
  },
  {
    name: 'close-initiative sheet',
    open: async (page) => {
      await page.getByTestId('initiative-row').locator('[data-testid=initiative-link]').getByText('Accessibility').click();
      await page.getByRole('button', { name: 'Close', exact: true }).click();
      await expect(page.getByRole('dialog', { name: 'Close initiative' })).toBeVisible();
    },
  },
  {
    name: 'Tasks view',
    open: async (page) => {
      await page.goto('/#/tasks?initiative=e2e-a11y');
      await expect(page.getByTestId('task-row').first()).toBeVisible();
    },
  },
  {
    name: 'Tasks view with the Now panel open and a conflict',
    open: async (page) => {
      await page.goto('/#/tasks?initiative=e2e-a11y');
      await expect(page.getByTestId('task-row').first()).toBeVisible();
      const toggle = page.getByTestId('now-toggle');
      if (await toggle.isVisible()) await toggle.click();
      await expect(page.locator('[data-testid=now-conflict][data-resource="repo:a11y"]')).toBeVisible();
    },
  },
  {
    name: 'new-task sheet',
    open: async (page) => {
      await page.goto('/#/tasks?initiative=e2e-a11y');
      await expect(page.getByTestId('task-row').first()).toBeVisible();
      await page.getByRole('button', { name: 'New task' }).locator('visible=true').click();
      const dialog = page.getByRole('dialog', { name: 'New task' });
      await expect(dialog).toBeVisible();
      await dialog.getByLabel('Kind').selectOption('watch'); // the longest form: the cadence field shows
      await expect(dialog.getByLabel('Cadence')).toBeVisible();
      await dialog.getByLabel('Find an open task').fill('scan');
      await expect(dialog.getByTestId('blocker-options')).toBeVisible();
    },
  },
  {
    name: 'task page',
    open: async (page) => {
      await page.goto('/#/t/e2e-a11y-1');
      await expect(page.getByTestId('task-title')).toHaveText('Scan the page');
      await expect(page.getByRole('button', { name: /^End lease L\d+$/ })).toBeVisible(); // the live lease and its End lease button
    },
  },
  {
    name: 'task page with a conflict',
    open: async (page) => {
      await page.goto('/#/t/e2e-a11y-1');
      await expect(page.getByTestId('task-title')).toHaveText('Scan the page');
      await expect(page.getByTestId('conflict')).toHaveCount(1);
    },
  },
  {
    name: 'add-dependency sheet',
    open: async (page) => {
      await page.goto('/#/t/e2e-a11y-1');
      await expect(page.getByTestId('task-title')).toHaveText('Scan the page');
      await page.getByRole('button', { name: 'Add dependency' }).click();
      const dialog = page.getByRole('dialog', { name: 'Add dependency' });
      await expect(dialog).toBeVisible();
      await dialog.getByLabel('Find an open task').fill('possible blocker');
      await expect(dialog.getByTestId('dep-options')).toBeVisible();
    },
  },
  {
    name: 'milestone sheet',
    open: async (page) => {
      await page.goto('/#/t/e2e-a11y-1');
      await expect(page.getByTestId('task-title')).toHaveText('Scan the page');
      await page.getByRole('button', { name: 'Milestone' }).click();
      const dialog = page.getByRole('dialog', { name: 'Record milestone' });
      await expect(dialog).toBeVisible();
      await dialog.getByRole('button', { name: 'Record milestone' }).click(); // the refusal shows in the sheet
      await expect(dialog.getByTestId('dialog-error')).toBeVisible();
    },
  },
  {
    name: 'end-lease sheet',
    open: async (page) => {
      await page.goto('/#/t/e2e-a11y-1');
      await expect(page.getByTestId('task-title')).toHaveText('Scan the page');
      await page.getByRole('button', { name: /^End lease L\d+$/ }).click();
      await expect(page.getByRole('dialog', { name: 'End lease' })).toBeVisible();
    },
  },
  {
    name: 'Settings -> API keys view',
    open: async (page) => {
      await page.goto('/#/settings/keys');
      await expect(page.getByTestId('key-row').first()).toBeVisible();
    },
  },
  {
    name: 'Signed out view',
    open: async (page) => {
      await page.getByRole('button', { name: 'Account menu' }).click();
      await page.getByRole('dialog', { name: 'Account' }).getByRole('button', { name: 'Sign out' }).click();
      await expect(page).toHaveURL(/#\/signed-out$/);
      await expect(page.getByTestId('signed-out')).toBeVisible();
    },
  },
  {
    name: 'new-initiative sheet',
    open: async (page) => {
      await page.getByRole('button', { name: 'New initiative' }).locator('visible=true').click();
      await expect(page.getByRole('dialog', { name: 'New initiative' })).toBeVisible();
    },
  },
  {
    name: 'new-key sheet',
    open: async (page) => {
      await page.goto('/#/settings/keys');
      await expect(page.getByTestId('key-row').first()).toBeVisible();
      await page.getByRole('button', { name: 'New API key' }).locator('visible=true').click();
      await expect(page.getByRole('dialog', { name: 'New API key' })).toBeVisible();
    },
  },
  {
    name: 'revoke confirmation',
    open: async (page) => {
      await page.goto('/#/settings/keys');
      await expect(page.getByTestId('key-row').first()).toBeVisible();
      await page.getByTestId('key-row').getByRole('button', { name: 'Revoke' }).locator('visible=true').first().click();
      await expect(page.getByRole('dialog', { name: 'Revoke API key' })).toBeVisible();
    },
  },
  {
    name: 'account menu',
    open: async (page) => {
      await page.getByRole('button', { name: 'Account menu' }).click();
      await expect(page.getByRole('dialog', { name: 'Account' })).toBeVisible();
    },
  },
];

for (const vp of viewports) {
  for (const theme of themes) {
    test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}, ${theme} theme`, () => {
      test.use({ viewport: vp.size, colorScheme: theme });

      test.beforeEach(async ({ page, playwright, baseURL }) => {
        const seed = await playwright.request.newContext({
          baseURL,
          extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
        });
        const res = await seed.post('/api/v1/initiatives', {
          data: { slug: 'e2e-a11y', title: 'Accessibility', tags: ['a11y', 'axe'], description: '## Scope\n\nSee [the docs](https://example.com/docs).' },
        });
        expect([201, 409]).toContain(res.status());
        if (res.status() === 201) {
          const task = await seed.post('/api/v1/tasks', { data: { initiative: 'e2e-a11y', title: 'Scan the page' } });
          expect(task.status()).toBe(201);
          const { ref } = (await task.json()) as { ref: string };
          expect((await seed.post('/api/v1/leases', { data: { task: ref, intent: 'scanning', resources: ['repo:a11y'] } })).status()).toBe(201);
          // Something the Add dependency sheet can offer as a blocker.
          expect((await seed.post('/api/v1/initiatives', { data: { slug: 'e2e-a11y-blockers', title: 'Blockers' } })).status()).toBe(201);
          const blocker = await seed.post('/api/v1/tasks', { data: { initiative: 'e2e-a11y-blockers', title: 'Possible blocker' } });
          expect(blocker.status()).toBe(201);
          // A second actor holds the same resource, so the Now panel and the task page show a conflict.
          const other = ((await blocker.json()) as { ref: string }).ref;
          const lease = await seed.post('/api/v1/leases', {
            data: { task: other, intent: 'also scanning', resources: ['repo:a11y'] },
            headers: { 'Board-Actor': 'a11y-other' },
          });
          expect(lease.status()).toBe(201);
        }
        await seed.dispose();
        await page.goto('/');
        await expect(page.getByTestId('initiative-row').first()).toBeVisible();
      });

      for (const state of states) {
        test(`${state.name} has no serious or critical axe violation`, async ({ page }) => {
          await state.open(page);
          await scan(page, theme);
        });
      }
    });
  }
}
