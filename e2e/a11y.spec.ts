import { DatabaseSync } from 'node:sqlite';
import { join } from 'node:path';
import AxeBuilder from '@axe-core/playwright';
import { expect, test as base, type APIRequestContext, type Page, type PlaywrightTestArgs } from '@playwright/test';

// R9/R12: axe-core over every view and dialog/sheet, at the phone and the desktop viewport, in the light and the dark
// theme. `states` holds the 19 earlier states (the Attention view over an open ask and a conflict among them); R12 adds PHASE1_STATES, the finite list of the Phase 1 views `#/tasks`,
// `#/i/<slug>` and `#/t/<ref>` and the 9 dialogs edit initiative, close initiative, new task, edit task, close task,
// milestone, record check, add dependency and end lease, over a seeded watch task with links, a milestone, a check, an
// expired and a live lease, a blocker, a dependent and a conflict. 31 states x 2 viewports x 2 themes = 124 scans. No
// rule is disabled and no element is excluded; a serious or critical violation fails the scan with the rule, the
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
    name: 'Attention view',
    open: async (page) => {
      await page.goto('/#/attention?initiative=e2e-a11y');
      await expect(page.getByTestId('copy-way-back').first()).toBeVisible();
      await expect(page.getByTestId('section-conflicts').getByTestId('attention-item')).toHaveCount(1);
    },
  },
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

// ---- Phase 1 (R12) -------------------------------------------------------------------------------------------------

const P1_SLUG = 'e2e-a11y-p1';
const P1_RESOURCE = 'repo:a11y-p1';

type Seed = { watch: string; blocker: string; lease: string };
let seeded: Promise<Seed> | undefined;

async function apiContext(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, actor = 'playwright') {
  return playwright.request.newContext({
    baseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': actor },
  });
}

async function send(api: APIRequestContext, path: string, data: object, status: number) {
  const res = await api.post(path, { data });
  expect(res.status(), `${path} ${await res.text()}`).toBe(status);
  return (await res.json()) as Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any -- seed responses are read for one field each
}

// Runs a lease out the way the Go scenarios do: its expiry moves into the past in the e2e store, then an API read expires it.
async function expireLease(api: APIRequestContext, ref: string) {
  const db = new DatabaseSync(join(process.env.E2E_WORK!, 'board.db'));
  try {
    db.exec('PRAGMA busy_timeout = 5000');
    const changed = db.prepare('UPDATE leases SET expires_at = ? WHERE n = ?').run(Date.now() - 30_000, Number(ref.slice(1)));
    expect(changed.changes).toBe(1);
  } finally {
    db.close();
  }
  expect((await api.get('/api/v1/leases?state=expired')).status()).toBe(200);
}

// One initiative with a watch task (the subject: a link, a blocker, a dependent, a milestone, a check, an expired lease and a
// live lease) and a second actor holding the same resource on the dependent, so the views show a conflict.
async function seedPhase1(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined): Promise<Seed> {
  const api = await apiContext(playwright, baseURL);
  const other = await apiContext(playwright, baseURL, 'a11y-p1-other');
  const created = await api.post('/api/v1/initiatives', {
    data: { slug: P1_SLUG, title: 'Phase 1 axe sweep', description: '## Scope\n\nSee [the docs](https://example.com/docs).', tags: ['a11y', 'axe'] },
  });
  expect([201, 409], `seed ${P1_SLUG}: ${created.status()}`).toContain(created.status());
  const task = async (title: string, extra: object = {}) => (await send(api, '/api/v1/tasks', { initiative: P1_SLUG, title, ...extra }, 201)).ref as string;
  const watch = await task('A11y watch', {
    kind: 'watch',
    cadence_seconds: 900,
    description: 'A watch task to scan.\n\n- one\n- two with [a link](https://example.com/x)',
    external_refs: [{ label: 'PR', url: 'https://example.com/pr/1' }],
  });
  const blocker = await task('A11y p1 blocker');
  const dependent = await task('A11y p1 dependent');
  await send(api, `/api/v1/tasks/${watch}/deps/${blocker}`, {}, 200);
  await send(api, `/api/v1/tasks/${dependent}/deps/${watch}`, {}, 200);
  await send(api, `/api/v1/tasks/${watch}/milestones`, { summary: 'Halfway there' }, 200);
  await send(api, `/api/v1/tasks/${watch}/checks`, { summary: 'Nothing changed' }, 200);
  const old = await send(api, '/api/v1/leases', { task: watch, intent: 'first try', resources: [P1_RESOURCE] }, 201);
  await expireLease(api, old.lease.ref);
  const live = await send(api, '/api/v1/leases', { task: watch, intent: 'second try', resources: [P1_RESOURCE, 'repo:a11y-p1-b'] }, 201);
  await send(other, '/api/v1/leases', { task: dependent, intent: 'also here', resources: [P1_RESOURCE] }, 201);
  await api.dispose();
  await other.dispose();
  return { watch, blocker, lease: live.lease.ref as string };
}

// A visible button of the page: on the phone the action bar sits above the tab bar.
const button = (page: Page, name: string | RegExp, exact = false) => page.getByRole('button', { name, exact }).locator('visible=true').first();

const openDialog = async (page: Page, name: string) => {
  const dialog = page.getByRole('dialog', { name });
  await expect(dialog).toBeVisible();
  return dialog;
};

const taskPage = async (page: Page, seed: Seed) => {
  await page.goto(`/#/t/${seed.watch}`);
  await expect(page.getByTestId('task-title')).toHaveText('A11y watch');
  await expect(page.getByTestId('lease')).toHaveCount(1);
  await expect(page.getByTestId('expired-lease')).toHaveCount(1);
  await expect(page.getByTestId('conflict').first()).toBeVisible(); // the second actor holds the same resource
};

const initiativePage = async (page: Page) => {
  await page.goto(`/#/i/${P1_SLUG}`);
  await expect(page.getByTestId('initiative-title')).toHaveText('Phase 1 axe sweep');
};

type Phase1State = { name: string; open: (page: Page, seed: Seed) => Promise<void> };

// The finite list of Phase 1 states this spec scans: 3 views and 9 dialogs.
const PHASE1_STATES: Phase1State[] = [
  {
    name: 'view #/tasks',
    open: async (page, seed) => {
      await page.goto(`/#/tasks?initiative=${P1_SLUG}`);
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
    },
  },
  {
    name: 'view #/i/<slug>',
    open: async (page, seed) => {
      await initiativePage(page);
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
    },
  },
  { name: 'view #/t/<ref>', open: taskPage },
  {
    name: 'dialog edit initiative',
    open: async (page) => {
      await initiativePage(page);
      await button(page, 'Edit', true).click();
      await openDialog(page, 'Edit initiative');
    },
  },
  {
    name: 'dialog close initiative',
    open: async (page) => {
      await initiativePage(page);
      await button(page, 'Close', true).click();
      await openDialog(page, 'Close initiative');
    },
  },
  {
    name: 'dialog new task',
    open: async (page, seed) => {
      await page.goto(`/#/tasks?initiative=${P1_SLUG}`);
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
      await button(page, 'New task').click();
      const dialog = await openDialog(page, 'New task');
      await dialog.getByLabel('Kind').selectOption('watch'); // the longest form: the cadence field shows
      await expect(dialog.getByLabel('Cadence')).toBeVisible();
      await dialog.getByLabel('Find an open task').fill(seed.blocker);
      await dialog.getByTestId('blocker-options').getByRole('button').first().click(); // a chosen blocker has its remove button
      await expect(dialog.getByTestId('blockers')).toBeVisible();
      await dialog.getByLabel('Link label').fill('PR');
      await dialog.getByLabel('Link URL').fill('https://example.com/1');
      await dialog.getByRole('button', { name: 'Add link' }).click();
      await expect(dialog.getByTestId('links')).toBeVisible();
    },
  },
  {
    name: 'dialog edit task',
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, 'Edit', true).click();
      await openDialog(page, 'Edit task');
    },
  },
  {
    name: 'dialog close task',
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, 'Close', true).click();
      await openDialog(page, 'Close task');
    },
  },
  {
    name: 'dialog milestone',
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, 'Milestone', true).click();
      const dialog = await openDialog(page, 'Record milestone');
      await dialog.getByRole('button', { name: 'Record milestone' }).click(); // the refusal shows in the sheet
      await expect(dialog.getByTestId('dialog-error')).toBeVisible();
    },
  },
  {
    name: 'dialog record check',
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, 'Record check', true).click();
      await openDialog(page, 'Record check');
    },
  },
  {
    name: 'dialog add dependency',
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, 'Add dependency', true).click();
      const dialog = await openDialog(page, 'Add dependency');
      await dialog.getByLabel('Find an open task').fill(P1_SLUG);
      await expect(dialog.getByTestId('dep-options')).toBeVisible();
    },
  },
  {
    name: 'dialog end lease',
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, `End lease ${seed.lease}`, true).click();
      await openDialog(page, 'End lease');
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
          // An open ask by an actor with a way back, for the Attention view.
          const ask = await seed.post('/api/v1/asks', {
            data: { question: 'Is the page readable?', task: ref, blocking: true },
            headers: { 'Board-Actor': 'a11y-asker', 'Board-Actor-Way-Back': 'ralphctl status a11y-asker' },
          });
          expect(ask.status()).toBe(201);
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
        await page.goto('/#/initiatives');
        await expect(page.getByTestId('initiative-row').first()).toBeVisible();
      });

      for (const state of states) {
        test(`${state.name} has no serious or critical axe violation`, async ({ page }) => {
          await state.open(page);
          await scan(page, theme);
        });
      }

      for (const state of PHASE1_STATES) {
        test(`${state.name} has no serious or critical axe violation`, async ({ page, playwright, baseURL }) => {
          seeded ??= seedPhase1(playwright, baseURL);
          await state.open(page, await seeded);
          await scan(page, theme);
        });
      }
    });
  }
}

test('the Phase 1 list names the 3 views and the 9 dialogs', () => {
  const names = PHASE1_STATES.map((s) => s.name);
  expect(names.filter((n) => n.startsWith('view '))).toEqual(['view #/tasks', 'view #/i/<slug>', 'view #/t/<ref>']);
  expect(names.filter((n) => n.startsWith('dialog ')).map((n) => n.slice('dialog '.length))).toEqual([
    'edit initiative',
    'close initiative',
    'new task',
    'edit task',
    'close task',
    'milestone',
    'record check',
    'add dependency',
    'end lease',
  ]);
});
