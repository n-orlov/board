import { DatabaseSync } from 'node:sqlite';
import { join } from 'node:path';
import { expect, test as base, type APIRequestContext, type Page, type PlaywrightTestArgs } from '@playwright/test';

// R3: at 390, 820, 1024 and 1440 px the page never scrolls sideways, whatever text the API accepts. Signs in through the mock provider.
// Every case seeds the longest unbroken text the server takes (no space to wrap at): a 200-character
// title, a 40-character slug, 20 tags of 40 characters, a 64-character key name; wide glyphs (W) and
// narrow ones (i) both, since they stress the line breaking differently.
// R12 extends it to the Phase 1 views and dialogs as a finite, named list of states (PHASE1_STATES below): the views
// `#/tasks`, `#/i/<slug>` and `#/t/<ref>`, and the 9 dialogs edit initiative, close initiative, new task, edit task,
// close task, milestone, record check, add dependency and end lease, each filled with the longest values the API takes.
// R13 extends it to the Phase 2 views and dialogs the same way (PHASE2_STATES below): `#/attention` with its five
// sections filled, `#/notes`, the six dialogs answer ask, withdraw ask, post note, close from attention, acknowledge
// and record check from attention, and the live indicator in its three states.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'tablet', size: { width: 820, height: 1180 } },
  { name: 'small-desktop', size: { width: 1024, height: 768 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;

const wide = (n: number, ch = 'W') => ch.repeat(n);
const tag = (i: number) => `t${String(i).padStart(2, '0')}${'w'.repeat(37)}`;
const tags = Array.from({ length: 20 }, (_, i) => tag(i));

const initiatives = [
  { slug: 'ovf-wide-title', title: wide(200), tags: ['ovf'] },
  { slug: 'ovf-narrow-title', title: wide(200, 'i'), tags: ['ovf'] },
  { slug: `ovf${"w".repeat(37)}`, title: 'Slug of the longest length', tags: ['ovf'] },
  { slug: 'ovf-max-tags', title: 'Twenty tags of forty characters', tags },
  { slug: 'ovf-url-title', title: `https://example.invalid/${'a'.repeat(176)}`, tags: ['ovf'] },
  { slug: 'ovf-mixed', title: `${wide(100)} ${wide(98, 'M')}`, tags },
];
const keyNames = [`ovf${wide(61)}`, `ovf${wide(61, 'i')}`, `ovf.${'_-.'.repeat(19)}x`];

async function overflow(page: Page) {
  return page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
    bodyScrollWidth: document.body.scrollWidth,
  }));
}

async function expectNoSidewaysScroll(page: Page) {
  const m = await overflow(page);
  expect(m.scrollWidth).toBeLessThanOrEqual(m.clientWidth);
  expect(m.bodyScrollWidth).toBeLessThanOrEqual(m.clientWidth);
}

// Nothing visible reaches past the right edge of the viewport.
async function expectNothingPastRight(page: Page, selector: string, width: number) {
  const rights = await page.locator(selector).evaluateAll((els) =>
    els.filter((el) => (el as HTMLElement).checkVisibility()).map((el) => el.getBoundingClientRect().right),
  );
  expect(rights.length).toBeGreaterThan(0);
  for (const right of rights) expect(right).toBeLessThanOrEqual(width + 0.5);
}

// ---- Phase 1 (R12) -------------------------------------------------------------------------------------------------

const P1_SLUG = `ovp${'w'.repeat(37)}`; // a 40-character slug
const P1_RESOURCE = `repo:${'r'.repeat(95)}`; // a 100-character resource
const P1_ACTOR = `ovfactor${'W'.repeat(32)}`;
const longUrl = `https://example.invalid/${'a'.repeat(170)}`;
const MAX_DESCRIPTION = 20000; // internal/domain MaxDescription, the longest accepted initiative or task description
// A description of exactly the longest accepted length: one unbroken run of wide letters, then a long URL.
const longDescription = (ch: string, url: string) => `${wide(MAX_DESCRIPTION - url.length - 1, ch)} ${url}`;
const INITIATIVE_DESCRIPTION = longDescription('W', `https://example.invalid/${'b'.repeat(400)}`);
const TASK_DESCRIPTION = longDescription('M', `https://example.invalid/${'c'.repeat(400)}`);

type Seed = { watch: string; blocker: string; dependent: string; lease: string };
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

// One initiative with the longest slug, title, description (20000 characters) and tags and four tasks: a watch task (the subject) with a
// 200-character title, a description of the longest accepted length, two long links, a blocker and a dependent (both with 200-character titles), a
// milestone and a check of 200 characters, an expired lease and a live lease with a 200-character intent and 20
// resources of 100 characters; a second actor holds the same resource on the dependent, so the page shows a conflict.
async function seedPhase1(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined): Promise<Seed> {
  const api = await apiContext(playwright, baseURL);
  const other = await apiContext(playwright, baseURL, P1_ACTOR);
  const created = await api.post('/api/v1/initiatives', {
    data: { slug: P1_SLUG, title: wide(200), description: INITIATIVE_DESCRIPTION, tags },
  });
  expect([201, 409], `seed ${P1_SLUG}: ${created.status()}`).toContain(created.status());
  const task = async (title: string, extra: object = {}) =>
    (await send(api, '/api/v1/tasks', { initiative: P1_SLUG, title, ...extra }, 201)).ref as string;
  const watch = await task(wide(200), {
    kind: 'watch',
    cadence_seconds: 900,
    description: TASK_DESCRIPTION,
    external_refs: [
      { label: wide(80), url: longUrl },
      { label: wide(80, 'i'), url: `https://example.invalid/${'i'.repeat(170)}` },
    ],
  });
  const blocker = await task(wide(200, 'i'));
  const dependent = await task(`${wide(100, 'M')} ${wide(98, 'W')}`);
  await send(api, `/api/v1/tasks/${watch}/deps/${blocker}`, {}, 200);
  await send(api, `/api/v1/tasks/${dependent}/deps/${watch}`, {}, 200);
  await send(api, `/api/v1/tasks/${watch}/milestones`, { summary: wide(200) }, 200);
  await send(api, `/api/v1/tasks/${watch}/checks`, { summary: wide(200, 'i') }, 200);
  const resources = [P1_RESOURCE, ...Array.from({ length: 19 }, (_, i) => `res${String(i).padStart(2, '0')}:${'x'.repeat(93)}`)];
  const old = await send(api, '/api/v1/leases', { task: watch, intent: wide(200, 'M'), resources: [P1_RESOURCE] }, 201);
  await expireLease(api, old.lease.ref);
  const live = await send(api, '/api/v1/leases', { task: watch, intent: wide(200), resources }, 201);
  await send(other, '/api/v1/leases', { task: dependent, intent: wide(200, 'i'), resources: [P1_RESOURCE] }, 201);
  await api.dispose();
  await other.dispose();
  return { watch, blocker, dependent, lease: live.lease.ref as string };
}

// A visible button of the page: on the phone the action bar sits above the tab bar, on the desktop in the header.
const button = (page: Page, name: string | RegExp, exact = false) => page.getByRole('button', { name, exact }).locator('visible=true').first();

const openDialog = async (page: Page, name: string) => {
  const dialog = page.getByRole('dialog', { name });
  await expect(dialog).toBeVisible();
  return dialog;
};

type Phase1State = { name: string; open: (page: Page, seed: Seed) => Promise<void>; within: string };

// The finite list of Phase 1 states this spec holds to the no-sideways-scroll bar: 3 views and 9 dialogs.
const PHASE1_STATES: Phase1State[] = [
  {
    name: 'view #/tasks',
    within: '[data-testid=task-row], [data-testid=task-row] *, [data-testid=now-conflict], [data-testid=now-conflict] *, [data-testid=now-lease], [data-testid=now-lease] *',
    open: async (page, seed) => {
      await page.goto(`/#/tasks?initiative=${P1_SLUG}`);
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
      const toggle = page.getByTestId('now-toggle');
      if (await toggle.isVisible()) await toggle.click();
      await expect(page.locator(`[data-testid=now-conflict][data-resource="${P1_RESOURCE}"]`)).toBeVisible();
    },
  },
  {
    name: 'view #/i/<slug>',
    within: '[data-testid=initiative-page] *',
    open: async (page, seed) => {
      await page.goto(`/#/i/${P1_SLUG}`);
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
      await expect(page.getByTestId('initiative-description')).toContainText(INITIATIVE_DESCRIPTION);
    },
  },
  {
    name: 'view #/t/<ref>',
    within: '[data-testid=task-page] *',
    open: async (page, seed) => {
      await page.goto(`/#/t/${seed.watch}`);
      await expect(page.getByTestId('task-title')).toHaveText(wide(200));
      await expect(page.getByTestId('lease')).toHaveCount(1);
      await expect(page.getByTestId('conflict')).toHaveCount(1);
      await expect(page.getByTestId('expired-lease')).toHaveCount(1);
      await expect(page.getByTestId('task-links').getByRole('link')).toHaveCount(2);
      await expect(page.getByTestId('event')).toHaveCount(7);
      await expect(page.getByTestId('task-description')).toContainText(TASK_DESCRIPTION);
    },
  },
  {
    name: 'dialog edit initiative',
    within: '[role=dialog] *',
    open: async (page) => {
      await page.goto(`/#/i/${P1_SLUG}`);
      await button(page, 'Edit', true).click();
      const dialog = await openDialog(page, 'Edit initiative');
      await dialog.getByRole('textbox', { name: 'Title' }).fill(wide(200));
      await dialog.getByLabel('Description').fill(wide(MAX_DESCRIPTION));
      await expect(dialog.getByLabel('Description')).toHaveValue(wide(MAX_DESCRIPTION));
      await dialog.getByLabel('Tags').fill(tags.join(', '));
    },
  },
  {
    name: 'dialog close initiative',
    within: '[role=dialog] *',
    open: async (page) => {
      await page.goto(`/#/i/${P1_SLUG}`);
      await button(page, 'Close', true).click();
      const dialog = await openDialog(page, 'Close initiative');
      await expect(dialog.getByRole('button', { name: /^Close and drop \d+ tasks$/ })).toBeVisible();
    },
  },
  {
    name: 'dialog new task',
    within: '[role=dialog] *',
    open: async (page, seed) => {
      await page.goto(`/#/tasks?initiative=${P1_SLUG}`);
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
      await button(page, 'New task').click();
      const dialog = await openDialog(page, 'New task');
      await dialog.getByLabel('Initiative').selectOption(P1_SLUG);
      await dialog.getByLabel('Kind').selectOption('watch');
      await dialog.getByRole('textbox', { name: 'Title' }).fill(wide(200));
      await dialog.getByLabel('Description').fill(TASK_DESCRIPTION);
      await expect(dialog.getByLabel('Description')).toHaveValue(TASK_DESCRIPTION);
      await dialog.getByLabel('Cadence').fill('1d');
      await dialog.getByLabel('Find an open task').fill(seed.blocker);
      await dialog.getByTestId('blocker-options').getByRole('button').first().click();
      await dialog.getByLabel('Link label').fill(wide(80));
      await dialog.getByLabel('Link URL').fill(longUrl);
      await dialog.getByRole('button', { name: 'Add link' }).click();
      await expect(dialog.getByTestId('links')).toBeVisible();
      await dialog.getByLabel('Find an open task').fill(seed.watch);
    },
  },
  {
    name: 'dialog edit task',
    within: '[role=dialog] *',
    open: async (page, seed) => {
      await page.goto(`/#/t/${seed.watch}`);
      await expect(page.getByTestId('task-title')).toHaveText(wide(200));
      await button(page, 'Edit', true).click();
      const dialog = await openDialog(page, 'Edit task');
      await dialog.getByRole('textbox', { name: 'Title' }).fill(wide(200));
      await expect(dialog.getByLabel('Description')).toHaveValue(TASK_DESCRIPTION);
      await dialog.getByLabel('Description').fill(wide(MAX_DESCRIPTION, 'M'));
      await expect(dialog.getByLabel('Description')).toHaveValue(wide(MAX_DESCRIPTION, 'M'));
      await dialog.getByLabel('Cadence').fill('365d');
    },
  },
  {
    name: 'dialog close task',
    within: '[role=dialog] *',
    open: async (page, seed) => {
      await page.goto(`/#/t/${seed.watch}`);
      await expect(page.getByTestId('task-title')).toHaveText(wide(200));
      await button(page, 'Close', true).click();
      const dialog = await openDialog(page, 'Close task');
      await expect(dialog.getByTestId('dialog-body')).toContainText(seed.watch);
    },
  },
  {
    name: 'dialog milestone',
    within: '[role=dialog] *',
    open: async (page, seed) => {
      await page.goto(`/#/t/${seed.watch}`);
      await expect(page.getByTestId('task-title')).toHaveText(wide(200));
      await button(page, 'Milestone', true).click();
      const dialog = await openDialog(page, 'Record milestone');
      await dialog.getByLabel('Summary').fill(wide(200));
    },
  },
  {
    name: 'dialog record check',
    within: '[role=dialog] *',
    open: async (page, seed) => {
      await page.goto(`/#/t/${seed.watch}`);
      await expect(page.getByTestId('task-title')).toHaveText(wide(200));
      await button(page, 'Record check', true).click();
      const dialog = await openDialog(page, 'Record check');
      await dialog.getByLabel('Summary').fill(wide(200, 'i'));
    },
  },
  {
    name: 'dialog add dependency',
    within: '[role=dialog] *',
    open: async (page, seed) => {
      await page.goto(`/#/t/${seed.watch}`);
      await expect(page.getByTestId('task-title')).toHaveText(wide(200));
      await button(page, 'Add dependency', true).click();
      const dialog = await openDialog(page, 'Add dependency');
      await dialog.getByLabel('Find an open task').fill(P1_SLUG);
      await expect(dialog.getByTestId('dep-options')).toBeVisible();
    },
  },
  {
    name: 'dialog end lease',
    within: '[role=dialog] *',
    open: async (page, seed) => {
      await page.goto(`/#/t/${seed.watch}`);
      await expect(page.getByTestId('task-title')).toHaveText(wide(200));
      await button(page, `End lease ${seed.lease}`, true).click();
      const dialog = await openDialog(page, 'End lease');
      await dialog.getByLabel('Outcome (optional)').fill(wide(200));
    },
  },
];

// ---- Phase 2 (R13) -------------------------------------------------------------------------------------------------

const P2_SLUG = `ovq${'w'.repeat(37)}`; // a 40-character slug
const P2_ACTOR = `ovqactor${'W'.repeat(32)}`; // the actor who asks, finishes, holds and posts
const P2_HOLDER = `ovqholder${'i'.repeat(31)}`; // the second actor of the conflict
const P2_WAY_BACK = `ralphctl status ${'W'.repeat(184)}`; // 200 characters, nowhere to break
const P2_HOLDER_WAY_BACK = `ralphctl status ${'i'.repeat(184)}`;
const P2_RESOURCE = `repo:${'q'.repeat(95)}`; // a 100-character resource
const P2_RESOURCES = [P2_RESOURCE, ...Array.from({ length: 19 }, (_, i) => `res${String(i).padStart(2, '0')}:${'y'.repeat(93)}`)];
const P2_OPTIONS = Array.from({ length: 10 }, (_, i) => `${i}${'W'.repeat(79)}`); // 10 options of 80 characters
const P2_MENTIONS = Array.from({ length: 20 }, (_, i) => `m${String(i).padStart(2, '0')}${'W'.repeat(97)}`); // 20 handles of 100 characters
const ASK_DETAILS = longDescription('W', `https://example.invalid/${'d'.repeat(400)}`);
const NOTE_BODY = longDescription('M', `https://example.invalid/${'n'.repeat(400)}`);

type Seed2 = { ask: string; askTask: string; doneTask: string; lease: string; watch: string; note: string };
let seeded2: Promise<Seed2> | undefined;

async function actorContext(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, actor: string, wayBack: string) {
  return playwright.request.newContext({
    baseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': actor, 'Board-Actor-Way-Back': wayBack },
  });
}

// One initiative with the longest slug, title and tags and one item in each of the five Attention sections: an open blocking ask with a
// 200-character question, details of the longest accepted length and 10 options of 80 characters; a done task with a 200-character title;
// an expired lease with a 200-character intent and 20 resources of 100 characters; an overdue watch with a 200-character title; and a
// conflict on a 100-character resource between two actors with 40-character handles and 200-character ways back. Notes of the longest
// accepted summary, body, resource and mentions, on a task, on the initiative and as a broadcast.
async function seedPhase2(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined): Promise<Seed2> {
  const a = await actorContext(playwright, baseURL, P2_ACTOR, P2_WAY_BACK);
  const b = await actorContext(playwright, baseURL, P2_HOLDER, P2_HOLDER_WAY_BACK);
  // A 409 is a second seeding by a worker the runner restarted after a failure: the initiative stays, its items are made again.
  const created = await a.post('/api/v1/initiatives', { data: { slug: P2_SLUG, title: wide(200), tags } });
  expect([201, 409], `seed ${P2_SLUG}: ${created.status()}`).toContain(created.status());
  const task = async (title: string, extra: object = {}) => (await send(a, '/api/v1/tasks', { initiative: P2_SLUG, title, ...extra }, 201)).ref as string;

  const askTask = await task(wide(200));
  const ask = (await send(a, '/api/v1/asks', { question: wide(200), details: ASK_DETAILS, options: P2_OPTIONS, task: askTask, mode: 'poll', blocking: true }, 201)).ref as string;

  const doneTask = await task(wide(200, 'i'));
  await send(a, `/api/v1/tasks/${doneTask}/done`, {}, 200);

  const leaseTask = await task(`${wide(100, 'M')} ${wide(98, 'W')}`);
  const old = await send(a, '/api/v1/leases', { task: leaseTask, intent: wide(200, 'M'), resources: P2_RESOURCES }, 201);
  await expireLease(a, old.lease.ref);

  const watch = await task(wide(200, 'M'), { kind: 'watch', cadence_seconds: 900, next_check_at: '2020-01-01T00:00:00Z' });

  const first = await task(wide(200));
  const second = await task(wide(200, 'i'));
  await send(a, '/api/v1/leases', { task: first, intent: wide(200), resources: [P2_RESOURCE] }, 201);
  await send(b, '/api/v1/leases', { task: second, intent: wide(200, 'i'), resources: [P2_RESOURCE] }, 201);

  const note = async (extra: object) =>
    (await send(a, '/api/v1/notes', { summary: wide(200), body: NOTE_BODY, resource: P2_RESOURCE, mentions: P2_MENTIONS, ...extra }, 201)).ref as string;
  const onTask = await note({ task: askTask });
  await note({ initiative: P2_SLUG, summary: wide(200, 'i') });
  await send(a, '/api/v1/notes', { summary: wide(200, 'M'), body: NOTE_BODY, resource: P2_RESOURCE, mentions: P2_MENTIONS }, 201);
  await a.dispose();
  await b.dispose();
  return { ask, askTask, doneTask, lease: old.lease.ref as string, watch, note: onTask };
}

const attentionItem = (page: Page, text: string) => page.getByTestId('attention-item').filter({ hasText: text });

const openAttention = async (page: Page, seed: Seed2) => {
  await page.goto(`/#/attention?initiative=${P2_SLUG}`);
  await expect(page.getByTestId('section-count')).toHaveCount(5);
  await expect(attentionItem(page, seed.ask)).toBeVisible();
};

const ATTENTION_WITHIN = '[data-testid=attention-view], [data-testid=attention-view] *';
const DIALOG_WITHIN = '[role=dialog] *';

type Phase2State = { name: string; open: (page: Page, seed: Seed2) => Promise<void>; within: string };

// The finite list of Phase 2 states this spec holds to the no-sideways-scroll bar: 2 views, 6 dialogs and the live indicator.
const PHASE2_STATES: Phase2State[] = [
  {
    name: 'view #/attention',
    within: ATTENTION_WITHIN,
    open: async (page, seed) => {
      await openAttention(page, seed);
      for (const key of ['open_asks', 'done_not_closed', 'expired_leases', 'overdue_watches', 'conflicts']) {
        await expect(page.getByTestId(`section-${key}`).getByTestId('attention-item').first()).toBeVisible();
        await expect(page.getByTestId(`section-${key}`).getByTestId('section-empty')).toHaveCount(0);
      }
      await expect(page.getByTestId('actor-handle').filter({ hasText: P2_ACTOR }).first()).toBeVisible();
      await expect(page.getByTestId('actor-way-back').filter({ hasText: P2_WAY_BACK }).first()).toBeVisible();
      await expect(page.getByTestId('actor-handle').filter({ hasText: P2_HOLDER }).first()).toBeVisible();
    },
  },
  {
    name: 'view #/notes',
    within: '[data-testid=notes-view], [data-testid=notes-view] *',
    open: async (page, seed) => {
      await page.goto(`/#/notes?initiative=${P2_SLUG}`);
      await expect(page.getByTestId('notes-list').getByTestId('note').first()).toBeVisible();
      await page.getByRole('button', { name: `Show body of ${seed.note}` }).click();
      await expect(page.getByTestId('note-body').first()).toContainText(NOTE_BODY);
      const filters = page.getByTestId('notes-filters');
      for (const label of ['Task', 'Initiative', 'Resource', 'Author', 'Mention']) await filters.getByLabel(label, { exact: true }).fill(wide(100));
    },
  },
  {
    name: 'view #/notes broadcasts',
    within: '[data-testid=notes-view], [data-testid=notes-view] *',
    open: async (page) => {
      await page.goto(`/#/notes?broadcast=only&resource=${P2_RESOURCE}`);
      await expect(page.getByTestId('notes-list').getByTestId('note-scope').first()).toHaveText('broadcast');
      await page.getByRole('button', { name: /^Show body of / }).first().click();
      await expect(page.getByTestId('note-body').first()).toContainText(NOTE_BODY);
    },
  },
  {
    name: 'dialog answer ask',
    within: DIALOG_WITHIN,
    open: async (page, seed) => {
      await openAttention(page, seed);
      await attentionItem(page, seed.ask).getByRole('button', { name: `Answer ${seed.ask}` }).click();
      const dialog = await openDialog(page, `Answer ${seed.ask}`);
      await expect(page.getByTestId('answer-option')).toHaveCount(10);
      await expect(page.getByTestId('answer-details')).toContainText(ASK_DETAILS);
      await dialog.getByRole('textbox', { name: 'Answer' }).fill(wide(MAX_DESCRIPTION));
    },
  },
  {
    name: 'dialog withdraw ask',
    within: DIALOG_WITHIN,
    open: async (page, seed) => {
      await openAttention(page, seed);
      await attentionItem(page, seed.ask).getByRole('button', { name: `Withdraw ${seed.ask}` }).click();
      const dialog = await openDialog(page, `Withdraw ${seed.ask}`);
      await dialog.getByRole('textbox', { name: 'Reason (optional)' }).fill(wide(200));
    },
  },
  {
    name: 'dialog post note',
    within: DIALOG_WITHIN,
    open: async (page, seed) => {
      await page.goto(`/#/notes?initiative=${P2_SLUG}`);
      await expect(page.getByTestId('notes-list').getByTestId('note').first()).toBeVisible();
      await button(page, 'Post note', true).click();
      const dialog = await openDialog(page, 'Post note');
      await dialog.getByLabel('Task (optional)').fill(seed.askTask);
      await dialog.getByLabel('Initiative (optional)').fill(P2_SLUG);
      await dialog.getByLabel('Summary').fill(wide(200));
      await dialog.getByLabel('Body (optional, Markdown)').fill(NOTE_BODY);
      await dialog.getByLabel('Resource (optional)').fill(P2_RESOURCE);
      await dialog.getByLabel('Mentions (optional)').fill(P2_MENTIONS.join(' '));
    },
  },
  {
    name: 'dialog close from attention',
    within: DIALOG_WITHIN,
    open: async (page, seed) => {
      await openAttention(page, seed);
      await attentionItem(page, seed.doneTask).getByRole('button', { name: `Close ${seed.doneTask}` }).click();
      const dialog = await openDialog(page, 'Close task');
      await dialog.getByLabel('Resolution').selectOption('duplicate');
    },
  },
  {
    name: 'dialog record check from attention',
    within: DIALOG_WITHIN,
    open: async (page, seed) => {
      await openAttention(page, seed);
      await attentionItem(page, seed.watch).getByRole('button', { name: `Record check ${seed.watch}` }).click();
      const dialog = await openDialog(page, 'Record check');
      await dialog.getByRole('textbox', { name: 'Summary' }).fill(wide(200, 'i'));
    },
  },
];

const LIVE_STATES = ['live', 'offline', 'reconnecting'] as const;

// The stream never opens: the indicator stays on `reconnecting`.
async function neverOpen(page: Page) {
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

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test.beforeAll(async ({ playwright, baseURL }) => {
      const seed = await playwright.request.newContext({
        baseURL,
        extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
      });
      for (const body of initiatives) {
        const res = await seed.post('/api/v1/initiatives', { data: body });
        expect([201, 409], `seed ${body.slug}: ${res.status()}`).toContain(res.status());
      }
      for (const name of keyNames) {
        const res = await seed.post('/api/v1/keys', { data: { name } });
        expect([201, 409], `seed key ${name}: ${res.status()}`).toContain(res.status());
      }
      await seed.dispose();
    });

    test('the Initiatives list holds the longest accepted titles, slugs and tags without scrolling sideways', async ({ page }) => {
      await page.goto('/#/initiatives');
      await expect(page.locator('[data-testid=initiative-row][data-slug=ovf-wide-title]')).toBeVisible();
      for (const i of initiatives) await expect(page.locator(`[data-testid=initiative-row][data-slug=${i.slug}]`)).toHaveCount(1);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=initiative-row]', vp.size.width);
      await expectNothingPastRight(page, '[data-testid=initiative-row] [data-testid=tag]', vp.size.width);
      // The text is all still there: wrapped, never cut off.
      await expect(page.locator('[data-testid=initiative-row][data-slug=ovf-wide-title]')).toContainText(wide(200));
    });

    test('the Tasks view and the New task sheet hold the longest accepted values without scrolling sideways', async ({ page, playwright, baseURL }) => {
      const slug = `ovf${'w'.repeat(37)}`;
      const seed = await playwright.request.newContext({
        baseURL,
        extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
      });
      const made = await seed.post('/api/v1/tasks', { data: { initiative: slug, title: wide(200) } });
      expect(made.status()).toBe(201);
      const { ref } = (await made.json()) as { ref: string };
      await seed.dispose();

      await page.goto(`/#/tasks?initiative=${slug}`);
      await expect(page.locator(`[data-testid=task-row][data-ref=${ref}]`)).toBeVisible();
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=task-row], [data-testid=task-row] *', vp.size.width);

      await page.getByRole('button', { name: 'New task' }).locator('visible=true').click();
      const dialog = page.getByRole('dialog', { name: 'New task' });
      await dialog.getByLabel('Initiative').selectOption(slug);
      await dialog.getByRole('textbox', { name: 'Title' }).fill(wide(200));
      await dialog.getByLabel('Find an open task').fill(ref);
      await dialog.getByTestId('blocker-options').getByRole('button').first().click();
      await dialog.getByLabel('Link label').fill(wide(80));
      await dialog.getByLabel('Link URL').fill(`https://example.invalid/${'a'.repeat(170)}`);
      await dialog.getByRole('button', { name: 'Add link' }).click();
      await expect(dialog.getByTestId('links')).toBeVisible();
      await dialog.getByLabel('Find an open task').fill(ref);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[role=dialog] *', vp.size.width);
    });

    test('the API keys list holds the longest accepted key names without scrolling sideways', async ({ page }) => {
      await page.goto('/#/settings/keys');
      for (const name of keyNames) await expect(page.locator(`[data-testid=key-row]`, { hasText: name })).toHaveCount(1);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=key-row]', vp.size.width);
      await expect(page.locator('[data-testid=key-row]', { hasText: keyNames[0] })).toContainText(keyNames[0]);
    });

    test('the revoke confirmation names a 64-character key without scrolling sideways', async ({ page }) => {
      await page.goto('/#/settings/keys');
      const row = page.locator('[data-testid=key-row]', { hasText: keyNames[0] });
      await row.getByRole('button', { name: 'Revoke' }).click();
      const dialog = page.getByRole('dialog', { name: 'Revoke API key' });
      await expect(dialog).toBeVisible();
      await expect(dialog).toContainText(keyNames[0]);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[role=dialog] *', vp.size.width);
    });

    test('creating an initiative with the longest slug shows a toast that stays inside the viewport', async ({ page }) => {
      await page.goto('/#/initiatives');
      await page.getByRole('button', { name: 'New initiative' }).locator('visible=true').click();
      const dialog = page.getByRole('dialog', { name: 'New initiative' });
      const slug = `ovf-toast-${vp.name}${'x'.repeat(40)}`.slice(0, 40);
      await dialog.getByLabel('Slug').fill(slug);
      await dialog.getByRole('textbox', { name: 'Title' }).fill(wide(200));
      await dialog.getByLabel('Tags').fill(tags.join(', '));
      await expectNoSidewaysScroll(page);
      await dialog.getByRole('button', { name: 'Create initiative' }).click();
      await expect(page.getByTestId('toast')).toContainText(slug);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=toast]', vp.size.width);
    });

    test('a freshly created 64-character key shows once without scrolling sideways', async ({ page }) => {
      await page.goto('/#/settings/keys');
      await page.getByRole('button', { name: 'New API key' }).locator('visible=true').click();
      const dialog = page.getByRole('dialog', { name: 'New API key' });
      const name = `ovfnew${vp.name}${wide(64 - 6 - vp.name.length)}`;
      await dialog.getByLabel('Name').fill(name);
      await dialog.getByRole('button', { name: 'Create key' }).click();
      await expect(page.getByTestId('new-key')).toContainText(name);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=new-key], [data-testid=new-key] *', vp.size.width);
    });

    for (const state of PHASE1_STATES) {
      test(`${state.name} holds the longest accepted values without scrolling sideways`, async ({ page, playwright, baseURL }) => {
        seeded ??= seedPhase1(playwright, baseURL);
        const seed = await seeded;
        await state.open(page, seed);
        await expectNoSidewaysScroll(page);
        await expectNothingPastRight(page, state.within, vp.size.width);
      });
    }

    for (const state of PHASE2_STATES) {
      test(`${state.name} holds the longest accepted values without scrolling sideways`, async ({ page, playwright, baseURL }) => {
        seeded2 ??= seedPhase2(playwright, baseURL);
        const seed = await seeded2;
        await state.open(page, seed);
        await expectNoSidewaysScroll(page);
        await expectNothingPastRight(page, state.within, vp.size.width);
      });
    }

    test('dialog acknowledge: the toast naming the lease and the Attention view hold the longest values without scrolling sideways', async ({ page, playwright, baseURL }) => {
      seeded2 ??= seedPhase2(playwright, baseURL);
      const seed = await seeded2;
      const api = await actorContext(playwright, baseURL, P2_ACTOR, P2_WAY_BACK);
      const taskRef = (await send(api, '/api/v1/tasks', { initiative: P2_SLUG, title: wide(200, 'M') }, 201)).ref as string;
      const lease = (await send(api, '/api/v1/leases', { task: taskRef, intent: wide(200), resources: P2_RESOURCES }, 201)).lease.ref as string;
      await expireLease(api, lease);
      await openAttention(page, seed);
      const item = attentionItem(page, lease);
      await expect(item).toBeVisible();
      await expectNoSidewaysScroll(page);
      await item.getByRole('button', { name: `Acknowledge ${lease}` }).click();
      await expect(page.getByTestId('toast')).toHaveText(`Lease ${lease} acknowledged`);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=toast]', vp.size.width);
      await expectNothingPastRight(page, ATTENTION_WITHIN, vp.size.width);
      await api.dispose();
    });

    for (const word of LIVE_STATES) {
      test(`the live indicator reading ${word} stays inside the viewport without scrolling sideways`, async ({ page, context }) => {
        if (word === 'reconnecting') await neverOpen(page);
        await page.goto('/#/attention');
        const indicator = page.getByRole('status', { name: /^Live updates:/ });
        await expect(indicator).toBeVisible();
        if (word === 'offline') await context.setOffline(true);
        await expect(indicator).toHaveAccessibleName(`Live updates: ${word}`);
        await expect(indicator).toHaveText(word);
        await expectNoSidewaysScroll(page);
        await expectNothingPastRight(page, '[data-testid=live-indicator], [data-testid=live-indicator] *', vp.size.width);
        await context.setOffline(false);
      });
    }

    test('the Phase 2 list names the views, the six dialogs and the three live indicator states', () => {
      const names = PHASE2_STATES.map((s) => s.name);
      expect(names.filter((n) => n.startsWith('view '))).toEqual(['view #/attention', 'view #/notes', 'view #/notes broadcasts']);
      // Acknowledge has no sheet: its own test above opens nothing and checks the toast it raises.
      expect(names.filter((n) => n.startsWith('dialog ')).map((n) => n.slice('dialog '.length))).toEqual([
        'answer ask',
        'withdraw ask',
        'post note',
        'close from attention',
        'record check from attention',
      ]);
      expect(LIVE_STATES).toEqual(['live', 'offline', 'reconnecting']);
    });

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
  });
}
