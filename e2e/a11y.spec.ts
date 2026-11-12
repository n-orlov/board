import { DatabaseSync } from 'node:sqlite';
import { join } from 'node:path';
import AxeBuilder from '@axe-core/playwright';
import { expect, test as base, type APIRequestContext, type Page, type PlaywrightTestArgs } from '@playwright/test';

// R9/R12: axe-core over every view and dialog/sheet, at the phone and the desktop viewport, in the light and the dark
// theme. `states` holds the 22 earlier states (the Attention view over an open ask and a conflict, its Answer sheet, the Notes view with a body expanded and its Post note sheet, among them); R12 adds PHASE1_STATES, the finite list of the Phase 1 views `#/tasks`,
// `#/i/<slug>` and `#/t/<ref>` and the 9 dialogs edit initiative, close initiative, new task, edit task, close task,
// milestone, record check, add dependency and end lease, over a seeded watch task with links, a milestone, a check, an
// expired and a live lease, a blocking ask, a note with its body expanded, a blocker, a dependent and a conflict. R13 adds PHASE2_STATES, the 2 views `#/attention` (all five sections filled) and `#/notes` and the six dialogs answer ask, withdraw ask, post note, close from attention, acknowledge (its confirmation sheet) and record check from attention, and the live indicator reading live, offline and reconnecting. The 34 earlier states and these 8 + 3 = 11 make 45 x 2 viewports x 2 themes = 180 scans. No
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
    name: 'Answer sheet',
    open: async (page) => {
      await page.goto('/#/attention?initiative=e2e-a11y');
      await page.getByTestId('action-answer').first().click();
      await expect(page.getByRole('dialog')).toBeVisible();
      await expect(page.getByTestId('answer-question')).toContainText('Is the page readable?');
    },
  },
  {
    name: 'Notes view',
    open: async (page) => {
      await page.goto('/#/notes?initiative=e2e-a11y');
      await expect(page.getByTestId('notes-list').getByTestId('note')).toHaveCount(1);
      await page.getByRole('button', { name: /Show body of/ }).click();
      await expect(page.getByTestId('note-body')).toBeVisible();
    },
  },
  {
    name: 'Post note sheet',
    open: async (page) => {
      await page.goto('/#/notes?initiative=e2e-a11y');
      await page.getByTestId('notes-view').getByRole('button', { name: 'Post note' }).click();
      await expect(page.getByRole('dialog', { name: 'Post note' })).toBeVisible();
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
      await expect(page.locator('[data-testid=task-row], [data-testid=needs-row]')).toHaveCount(1); // the task is in Needs attention or the table
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
      await expect(page.locator('[data-testid=task-row], [data-testid=needs-row]').first()).toBeVisible();
    },
  },
  {
    name: 'Tasks view with the Now panel open and a conflict',
    open: async (page) => {
      await page.goto('/#/tasks?initiative=e2e-a11y');
      await expect(page.locator('[data-testid=task-row], [data-testid=needs-row]').first()).toBeVisible();
      const toggle = page.getByTestId('now-toggle');
      if (await toggle.isVisible()) await toggle.click();
      await expect(page.locator('[data-testid=now-conflict][data-resource="repo:a11y"]')).toBeVisible();
    },
  },
  {
    name: 'new-task sheet',
    open: async (page) => {
      await page.goto('/#/tasks?initiative=e2e-a11y');
      await expect(page.locator('[data-testid=task-row], [data-testid=needs-row]').first()).toBeVisible();
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
  await send(api, '/api/v1/asks', { task: watch, question: 'Keep watching?', details: 'Say **yes** or no.', options: ['yes', 'no'], mode: 'poll', blocking: true }, 201);
  await send(api, '/api/v1/notes', { task: watch, summary: 'A note with a body', body: 'The **body** of the note.', resource: 'host:a11y' }, 201);
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
  await expect(page.getByTestId('blocked-reason')).toContainText('Keep watching?'); // the blocking ask and the note are on the page
  await page.getByRole('button', { name: /Show body of/ }).click();
  await expect(page.getByTestId('note-body')).toBeVisible();
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
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}], [data-testid=needs-row][data-ref=${seed.watch}]`)).toBeVisible();
    },
  },
  {
    name: 'view #/i/<slug>',
    open: async (page, seed) => {
      await initiativePage(page);
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}], [data-testid=needs-row][data-ref=${seed.watch}]`)).toBeVisible();
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
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}], [data-testid=needs-row][data-ref=${seed.watch}]`)).toBeVisible();
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

// ---- Phase 2 (R13) -------------------------------------------------------------------------------------------------

const P2_SLUG = 'e2e-a11y-p2';
const P2_RESOURCE = 'repo:a11y-p2';
const P2_ACTOR = 'a11y-p2-a';
const P2_HOLDER = 'a11y-p2-b';

type Seed2 = { ask: string; doneTask: string; watch: string; note: string };
let seeded2: Promise<Seed2> | undefined;

async function actorApi(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, actor: string) {
  return playwright.request.newContext({
    baseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': actor, 'Board-Actor-Way-Back': `ralphctl status ${actor}` },
  });
}

// One initiative with one item in each of the five Attention sections (an open blocking ask with options, a done task, an expired
// lease, an overdue watch and a conflict between two actors) and a note on a task, on the initiative and as a broadcast.
async function seedPhase2(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined): Promise<Seed2> {
  const a = await actorApi(playwright, baseURL, P2_ACTOR);
  const b = await actorApi(playwright, baseURL, P2_HOLDER);
  // A 409 is a second seeding by a worker the runner restarted after a failure: the initiative stays, its items are made again.
  const created = await a.post('/api/v1/initiatives', { data: { slug: P2_SLUG, title: 'Phase 2 axe sweep', tags: ['a11y'] } });
  expect([201, 409], `seed ${P2_SLUG}: ${created.status()}`).toContain(created.status());
  const task = async (title: string, extra: object = {}) => (await send(a, '/api/v1/tasks', { initiative: P2_SLUG, title, ...extra }, 201)).ref as string;

  const askTask = await task('A11y p2 ask subject');
  const ask = (await send(a, '/api/v1/asks', { question: 'Is this readable?', details: 'Scan it.', options: ['Yes', 'No', 'Maybe'], task: askTask, mode: 'poll', blocking: true }, 201)).ref as string;
  const doneTask = await task('A11y p2 done');
  await send(a, `/api/v1/tasks/${doneTask}/done`, {}, 200);
  const leaseTask = await task('A11y p2 lease');
  const old = await send(a, '/api/v1/leases', { task: leaseTask, intent: 'will expire', resources: ['repo:a11y-p2-old'] }, 201);
  await expireLease(a, old.lease.ref);
  const watch = await task('A11y p2 watch', { kind: 'watch', cadence_seconds: 900, next_check_at: '2020-01-01T00:00:00Z' });
  const first = await task('A11y p2 first holder');
  const second = await task('A11y p2 second holder');
  await send(a, '/api/v1/leases', { task: first, intent: 'holding', resources: [P2_RESOURCE] }, 201);
  await send(b, '/api/v1/leases', { task: second, intent: 'also holding', resources: [P2_RESOURCE] }, 201);

  const onTask = (await send(a, '/api/v1/notes', { task: askTask, summary: 'A note on the task', body: 'Body of the **note**.' }, 201)).ref as string;
  await send(a, '/api/v1/notes', { initiative: P2_SLUG, summary: 'A note on the initiative', body: 'More body.' }, 201);
  await send(a, '/api/v1/notes', { summary: 'A broadcast', body: 'To everyone.', resource: P2_RESOURCE }, 201);
  await a.dispose();
  await b.dispose();
  return { ask, doneTask, watch, note: onTask };
}

// The item that names `text` as a whole ref: `A1` is not `A11`, nor the "A11" inside "A11y" or "e2e-a11y-p2-1" inside "...-p2-10".
const attentionItem = (page: Page, text: string) =>
  page.getByTestId('attention-item').filter({ hasText: new RegExp(`(?<![\\w-])${text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?![\\w-])`) });

const openAttention = async (page: Page, seed: Seed2) => {
  await page.goto(`/#/attention?initiative=${P2_SLUG}`);
  await expect(page.getByTestId('section-count')).toHaveCount(5);
  await expect(attentionItem(page, seed.ask)).toBeVisible();
};

type Phase2State = { name: string; open: (page: Page, seed: Seed2, api: APIRequestContext) => Promise<void> };

// The finite list of Phase 2 states this spec scans: 2 views and the six dialogs. Acknowledge's state is its confirmation
// sheet, opened over a lease of its own.
const PHASE2_STATES: Phase2State[] = [
  {
    name: 'view #/attention',
    open: async (page, seed) => {
      await openAttention(page, seed);
      for (const key of ['open_asks', 'done_not_closed', 'expired_leases', 'overdue_watches', 'conflicts']) {
        await expect(page.getByTestId(`section-${key}`).getByTestId('attention-item').first()).toBeVisible();
      }
      await expect(page.getByTestId('copy-way-back').first()).toBeVisible();
    },
  },
  {
    name: 'view #/notes',
    open: async (page, seed) => {
      await page.goto(`/#/notes?initiative=${P2_SLUG}`);
      await expect(page.getByTestId('notes-list').getByTestId('note').first()).toBeVisible();
      await page.getByRole('button', { name: `Show body of ${seed.note}` }).click();
      await expect(page.getByTestId('note-body').first()).toBeVisible();
    },
  },
  {
    name: 'dialog answer ask',
    open: async (page, seed) => {
      await openAttention(page, seed);
      await attentionItem(page, seed.ask).getByRole('button', { name: `Answer ${seed.ask}` }).click();
      const dialog = await openDialog(page, `Answer ${seed.ask}`);
      await expect(page.getByTestId('answer-option')).toHaveCount(3);
      await dialog.getByRole('textbox', { name: 'Answer' }).fill('Yes');
    },
  },
  {
    name: 'dialog withdraw ask',
    open: async (page, seed) => {
      await openAttention(page, seed);
      await attentionItem(page, seed.ask).getByRole('button', { name: `Withdraw ${seed.ask}` }).click();
      await openDialog(page, `Withdraw ${seed.ask}`);
    },
  },
  {
    name: 'dialog post note',
    open: async (page) => {
      await page.goto(`/#/notes?initiative=${P2_SLUG}`);
      await expect(page.getByTestId('notes-list').getByTestId('note').first()).toBeVisible();
      await button(page, 'Post note', true).click();
      await openDialog(page, 'Post note');
    },
  },
  {
    name: 'dialog close from attention',
    open: async (page, seed) => {
      await openAttention(page, seed);
      await attentionItem(page, seed.doneTask).getByRole('button', { name: `Close ${seed.doneTask}` }).click();
      await openDialog(page, 'Close task');
    },
  },
  {
    name: 'dialog acknowledge',
    open: async (page, seed, api) => {
      // Each scan opens the confirmation over an expired lease of its own, so no other state depends on it.
      const task = (await send(api, '/api/v1/tasks', { initiative: P2_SLUG, title: 'A11y p2 acknowledged lease' }, 201)).ref as string;
      const lease = (await send(api, '/api/v1/leases', { task, intent: 'to acknowledge', resources: ['repo:a11y-p2-ack'] }, 201)).lease.ref as string;
      await expireLease(api, lease);
      await openAttention(page, seed);
      await attentionItem(page, lease).getByRole('button', { name: `Acknowledge ${lease}` }).click();
      await openDialog(page, `Acknowledge ${lease}`);
    },
  },
  {
    name: 'dialog record check from attention',
    open: async (page, seed) => {
      await openAttention(page, seed);
      await attentionItem(page, seed.watch).getByRole('button', { name: `Record check ${seed.watch}` }).click();
      await openDialog(page, 'Record check');
    },
  },
];

const LIVE_STATES = ['live', 'offline', 'reconnecting'] as const;

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
          // A note with a body, for the Notes view.
          expect((await seed.post('/api/v1/notes', { data: { task: ref, summary: 'Scan started', body: 'Using **axe**.' } })).status()).toBe(201);
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

      for (const state of PHASE2_STATES) {
        test(`${state.name} has no serious or critical axe violation`, async ({ page, playwright, baseURL }) => {
          seeded2 ??= seedPhase2(playwright, baseURL);
          const seed = await seeded2;
          const api = await actorApi(playwright, baseURL, P2_ACTOR);
          try {
            await state.open(page, seed, api);
            await scan(page, theme);
          } finally {
            await api.dispose();
          }
        });
      }

      for (const word of LIVE_STATES) {
        test(`the live indicator reading ${word} has no serious or critical axe violation`, async ({ page, context }) => {
          if (word === 'reconnecting') {
            // The stream never opens: the indicator stays on `reconnecting`.
            await page.addInitScript(() => {
              class Silent {
                readyState = 0;
                onopen = null;
                onerror = null;
                addEventListener() {}
                close() {}
              }
              (window as unknown as { EventSource: unknown }).EventSource = Silent;
            });
          }
          await page.goto('/#/attention');
          if (word === 'reconnecting') await page.reload(); // beforeEach already loaded the app: the stand-in applies to a fresh document
          const indicator = page.getByRole('status', { name: /^Live updates:/ });
          await expect(indicator).toBeVisible();
          if (word === 'offline') await context.setOffline(true);
          await expect(indicator).toHaveAccessibleName(`Live updates: ${word}`);
          await expect(indicator).toHaveText(word);
          await scan(page, theme);
          await context.setOffline(false);
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

test('the Phase 2 list names the 2 views, the six dialogs and the three live indicator states', () => {
  const names = PHASE2_STATES.map((s) => s.name);
  expect(names.filter((n) => n.startsWith('view '))).toEqual(['view #/attention', 'view #/notes']);
  expect(names.filter((n) => n.startsWith('dialog ')).map((n) => n.slice('dialog '.length))).toEqual([
    'answer ask',
    'withdraw ask',
    'post note',
    'close from attention',
    'acknowledge',
    'record check from attention',
  ]);
  expect(LIVE_STATES).toEqual(['live', 'offline', 'reconnecting']);
});
