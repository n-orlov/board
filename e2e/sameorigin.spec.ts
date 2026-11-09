import { DatabaseSync } from 'node:sqlite';
import { join } from 'node:path';
import { expect, test as base, type APIRequestContext, type Page, type PlaywrightTestArgs } from '@playwright/test';

// R1: the UI makes no cross-origin request (no web font, CDN, remote icon or script). Signs in through the
// mock provider (another port, so another origin: recording starts only afterwards, and no API key rides
// along), then records every request the page issues while it loads each of the views (Attention, Initiatives, Tasks, Settings -> API keys) at the phone and
// the desktop viewport. R12 adds PHASE1_STATES, the finite list of the Phase 1
// views `#/tasks`, `#/i/<slug>` and `#/t/<ref>`, each loaded in full over a seeded watch task with a blocker, a
// dependent, a milestone, a lease and a conflict, and each again with a dialog opened on the page (edit initiative,
// new task, edit task, add dependency, end lease): 8 states x 2 viewports = 16 more visits. Every recorded http(s)/ws(s) URL must have the board
// server's origin. R13 adds PHASE2_STATES, the 2 views `#/attention` and `#/notes` and the six dialogs answer ask, withdraw ask, post note, close
// from attention, acknowledge and record check from attention: 8 states x 2 viewports = 16 more visits, each of which must also
// record the `/api/v1/stream` request, so the EventSource is held to the same bar as every fetch. data: and blob: URLs never touch the network and are not origins; they are left out.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;

const views = [
  {
    name: 'Attention',
    hash: '#/',
    ready: async (page: Page) => {
      await expect(page.getByTestId('section-open_asks')).toBeVisible();
    },
  },
  {
    name: 'Initiatives',
    hash: '#/initiatives',
    ready: async (page: Page) => {
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
    },
  },
  {
    name: 'Tasks',
    hash: '#/tasks',
    ready: async (page: Page) => {
      await expect(page.getByRole('heading', { level: 2, name: 'Tasks' })).toBeVisible();
      await expect(page.getByTestId('tasks-skeleton')).toHaveCount(0);
    },
  },
  {
    name: 'Settings -> API keys',
    hash: '#/settings/keys',
    ready: async (page: Page) => {
      await expect(page.getByTestId('key-row').first()).toBeVisible();
    },
  },
  {
    name: 'Signed out',
    hash: '#/signed-out',
    ready: async (page: Page) => {
      await expect(page.getByTestId('signed-out')).toBeVisible();
    },
  },
] as const;

const P1_SLUG = 'e2e-sameorigin-p1';

type Seed = { watch: string; lease: string };
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

// One initiative with a watch task (the subject) that has a link, a blocker, a dependent, a milestone and a live
// lease; a second actor holds the same resource on the dependent, so the task page shows a conflict. Seeded once.
async function seedPhase1(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined): Promise<Seed> {
  const api = await apiContext(playwright, baseURL);
  const other = await apiContext(playwright, baseURL, 'sameorigin-other');
  const created = await api.post('/api/v1/initiatives', { data: { slug: P1_SLUG, title: 'Same origin Phase 1', description: 'Phase 1 origins', tags: ['origin'] } });
  expect([201, 409], `seed ${P1_SLUG}: ${created.status()}`).toContain(created.status());
  const listed = await api.get(`/api/v1/tasks?initiative=${P1_SLUG}`);
  expect(listed.status()).toBe(200);
  const existing = ((await listed.json()) as { tasks?: { ref: string; title: string }[] }).tasks ?? [];
  const watchRow = existing.find((t) => t.title === 'Origin watch task');
  let watch = watchRow?.ref as string | undefined;
  let lease = '';
  if (!watch) {
    const task = async (title: string, extra: object = {}) => (await send(api, '/api/v1/tasks', { initiative: P1_SLUG, title, ...extra }, 201)).ref as string;
    watch = await task('Origin watch task', { kind: 'watch', cadence_seconds: 900, external_refs: [{ label: 'Spec', url: 'https://example.invalid/spec' }] });
    const blocker = await task('Origin blocker');
    const dependent = await task('Origin dependent');
    await send(api, `/api/v1/tasks/${watch}/deps/${blocker}`, {}, 200);
    await send(api, `/api/v1/tasks/${dependent}/deps/${watch}`, {}, 200);
    await send(api, `/api/v1/tasks/${watch}/milestones`, { summary: 'Origin milestone' }, 200);
    const live = await send(api, '/api/v1/leases', { task: watch, intent: 'Origin lease', resources: ['repo:origin'] }, 201);
    lease = live.lease.ref as string;
    await send(other, '/api/v1/leases', { task: dependent, intent: 'Origin conflict', resources: ['repo:origin'] }, 201);
  }
  if (!lease) {
    const leases = await api.get('/api/v1/leases?state=active');
    expect(leases.status()).toBe(200);
    const rows = ((await leases.json()) as { leases?: { ref: string; task: string }[] }).leases ?? [];
    lease = rows.find((l) => l.task === watch)?.ref ?? '';
  }
  await api.dispose();
  await other.dispose();
  return { watch, lease };
}

const openDialog = async (page: Page, name: string) => {
  const dialog = page.getByRole('dialog', { name });
  await expect(dialog).toBeVisible();
  return dialog;
};

// A visible button of the page: on the phone the action bar sits above the tab bar, on the desktop in the header.
const button = (page: Page, name: string | RegExp, exact = false) => page.getByRole('button', { name, exact }).locator('visible=true').first();

type Phase1State = { name: string; entry: (seed: Seed) => string; ready: (page: Page, seed: Seed) => Promise<void>; dialog?: (page: Page, seed: Seed) => Promise<void> };

const taskPageReady = async (page: Page) => {
  await expect(page.getByTestId('task-title')).toHaveText('Origin watch task');
  await expect(page.getByTestId('conflict')).toHaveCount(1);
};
const tasksViewReady = async (page: Page, seed: Seed) => {
  await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
};
const initiativePageReady = async (page: Page, seed: Seed) => {
  await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
};

// The finite list of Phase 1 states this spec records: 3 views, then dialogs opened on them.
const PHASE1_STATES: Phase1State[] = [
  { name: 'view #/tasks', entry: () => `/#/tasks?initiative=${P1_SLUG}`, ready: tasksViewReady },
  { name: 'view #/i/<slug>', entry: () => `/#/i/${P1_SLUG}`, ready: initiativePageReady },
  { name: 'view #/t/<ref>', entry: (seed) => `/#/t/${seed.watch}`, ready: taskPageReady },
  {
    name: 'view #/tasks with the new-task dialog open',
    entry: () => `/#/tasks?initiative=${P1_SLUG}`,
    ready: tasksViewReady,
    dialog: async (page) => {
      await button(page, 'New task').click();
      const dialog = await openDialog(page, 'New task');
      await dialog.getByLabel('Kind').selectOption('watch');
      await dialog.getByLabel('Find an open task').fill('Origin');
      await expect(dialog.getByTestId('blocker-options')).toBeVisible();
    },
  },
  {
    name: 'view #/i/<slug> with the edit-initiative dialog open',
    entry: () => `/#/i/${P1_SLUG}`,
    ready: initiativePageReady,
    dialog: async (page) => {
      await button(page, 'Edit', true).click();
      await openDialog(page, 'Edit initiative');
    },
  },
  {
    name: 'view #/t/<ref> with the edit-task dialog open',
    entry: (seed) => `/#/t/${seed.watch}`,
    ready: taskPageReady,
    dialog: async (page) => {
      await button(page, 'Edit', true).click();
      await openDialog(page, 'Edit task');
    },
  },
  {
    name: 'view #/t/<ref> with the add-dependency dialog open',
    entry: (seed) => `/#/t/${seed.watch}`,
    ready: taskPageReady,
    dialog: async (page) => {
      await button(page, 'Add dependency', true).click();
      const dialog = await openDialog(page, 'Add dependency');
      await dialog.getByLabel('Find an open task').fill('Origin');
      await expect(dialog.getByTestId('dep-options')).toBeVisible();
    },
  },
  {
    name: 'view #/t/<ref> with the end-lease dialog open',
    entry: (seed) => `/#/t/${seed.watch}`,
    ready: taskPageReady,
    dialog: async (page, seed) => {
      await button(page, `End lease ${seed.lease}`, true).click();
      await openDialog(page, 'End lease');
    },
  },
];

// ---- Phase 2 (R13) -------------------------------------------------------------------------------------------------

const P2_SLUG = 'e2e-sameorigin-p2';
const P2_ACTOR = 'sameorigin-p2';

type Seed2 = { ask: string; doneTask: string; watch: string; note: string };
let seeded2: Promise<Seed2> | undefined;

// Runs a lease out: its expiry moves into the past in the e2e store, then an API read expires it.
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

// One initiative with an open blocking ask, a done task, an overdue watch task and a note, so the Attention view and the Notes view
// have an item for each dialog to open on. Seeded once.
async function seedPhase2(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined): Promise<Seed2> {
  const a = await apiContext(playwright, baseURL, P2_ACTOR);
  const created = await a.post('/api/v1/initiatives', { data: { slug: P2_SLUG, title: 'Same origin Phase 2', tags: ['origin'] } });
  expect([201, 409], `seed ${P2_SLUG}: ${created.status()}`).toContain(created.status());
  const task = async (title: string, extra: object = {}) => (await send(a, '/api/v1/tasks', { initiative: P2_SLUG, title, ...extra }, 201)).ref as string;
  const askTask = await task('Origin p2 ask subject');
  const ask = (await send(a, '/api/v1/asks', { question: 'Is this same-origin?', options: ['Yes', 'No'], task: askTask, mode: 'poll', blocking: true }, 201)).ref as string;
  const doneTask = await task('Origin p2 done');
  await send(a, `/api/v1/tasks/${doneTask}/done`, {}, 200);
  const watch = await task('Origin p2 watch', { kind: 'watch', cadence_seconds: 900, next_check_at: '2020-01-01T00:00:00Z' });
  const note = (await send(a, '/api/v1/notes', { task: askTask, summary: 'Origin p2 note', body: 'Body of the note.' }, 201)).ref as string;
  await a.dispose();
  return { ask, doneTask, watch, note };
}

const attentionItem = (page: Page, text: string) => page.getByTestId('attention-item').filter({ hasText: text });

const openAttention = async (page: Page, seed: Seed2) => {
  await page.goto(`/#/attention?initiative=${P2_SLUG}`);
  await expect(attentionItem(page, seed.ask)).toBeVisible();
};

const openNotes = async (page: Page) => {
  await page.goto(`/#/notes?initiative=${P2_SLUG}`);
  await expect(page.getByTestId('notes-list').getByTestId('note').first()).toBeVisible();
};

type Phase2State = { name: string; open: (page: Page, seed: Seed2, api: APIRequestContext) => Promise<void> };

// The finite list of Phase 2 states this spec records: 2 views and the six dialogs opened on them.
const PHASE2_STATES: Phase2State[] = [
  { name: 'view #/attention', open: openAttention },
  {
    name: 'view #/notes',
    open: async (page, seed) => {
      await openNotes(page);
      await page.getByRole('button', { name: `Show body of ${seed.note}` }).click();
      await expect(page.getByTestId('note-body').first()).toBeVisible();
    },
  },
  {
    name: 'dialog answer ask',
    open: async (page, seed) => {
      await openAttention(page, seed);
      await attentionItem(page, seed.ask).getByRole('button', { name: `Answer ${seed.ask}` }).click();
      await openDialog(page, `Answer ${seed.ask}`);
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
      await openNotes(page);
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
      // Acknowledging removes the item, so each visit acknowledges an expired lease of its own.
      const task = (await send(api, '/api/v1/tasks', { initiative: P2_SLUG, title: 'Origin p2 acknowledged lease' }, 201)).ref as string;
      const resource = `repo:origin-p2-${task}`;
      const lease = (await send(api, '/api/v1/leases', { task, intent: 'to acknowledge', resources: [resource] }, 201)).lease.ref as string;
      await expireLease(api, lease);
      await openAttention(page, seed);
      await attentionItem(page, lease).getByRole('button', { name: `Acknowledge ${lease}` }).click();
      await expect(page.getByTestId('toast')).toHaveText(`Lease ${lease} acknowledged`);
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

const networkSchemes = new Set(['http:', 'https:', 'ws:', 'wss:']);

/** The recorded URLs that go over the network to an origin other than `origin`. */
function foreignURLs(urls: string[], origin: string): string[] {
  return urls.filter((u) => {
    const url = new URL(u);
    return networkSchemes.has(url.protocol) && url.origin !== origin;
  });
}

/** Records the URL of every request the page issues (including those of its frames and workers). */
function record(page: Page): string[] {
  const urls: string[] = [];
  page.on('request', (request) => urls.push(request.url()));
  return urls;
}

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test.beforeEach(async ({ page, playwright, baseURL }) => {
      const seed = await playwright.request.newContext({
        baseURL,
        extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
      });
      const res = await seed.post('/api/v1/initiatives', { data: { slug: 'e2e-sameorigin', title: 'Same origin', tags: ['origin'] } });
      expect([201, 409]).toContain(res.status());
      await seed.dispose();
      await page.goto('/#/initiatives');
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
    });

    for (const view of views) {
      test(`${view.name} view requests only the board server's origin`, async ({ page, baseURL }) => {
        const origin = new URL(baseURL as string).origin;
        const urls = record(page);

        // A full page load of the view (not a hash change inside the loaded document).
        await page.goto('about:blank');
        await page.goto(`/${view.hash}`);
        await view.ready(page);
        await page.waitForLoadState('networkidle');

        const network = urls.filter((u) => networkSchemes.has(new URL(u).protocol));
        expect(network.length, 'the visit issued no network request at all').toBeGreaterThan(0);
        expect(network.some((u) => new URL(u).pathname === '/'), 'the document itself was recorded').toBe(true);
        expect(foreignURLs(urls, origin), 'requests to another origin').toEqual([]);
        for (const u of network) expect(new URL(u).origin).toBe(origin);
      });
    }

    for (const state of PHASE1_STATES) {
      test(`${state.name} requests only the board server's origin`, async ({ page, playwright, baseURL }) => {
        seeded ??= seedPhase1(playwright, baseURL);
        const seed = await seeded;
        const origin = new URL(baseURL as string).origin;
        const urls = record(page);

        // A full page load of the view, then the dialog opened on the loaded page.
        await page.goto('about:blank');
        await page.goto(state.entry(seed));
        await state.ready(page, seed);
        await state.dialog?.(page, seed);
        await page.waitForLoadState('networkidle');

        const network = urls.filter((u) => networkSchemes.has(new URL(u).protocol));
        expect(network.length, 'the visit issued no network request at all').toBeGreaterThan(0);
        expect(network.some((u) => new URL(u).pathname === '/'), 'the document itself was recorded').toBe(true);
        expect(foreignURLs(urls, origin), 'requests to another origin').toEqual([]);
        for (const u of network) expect(new URL(u).origin).toBe(origin);
      });
    }
    for (const state of PHASE2_STATES) {
      test(`${state.name} requests only the board server's origin, the stream included`, async ({ page, playwright, baseURL }) => {
        seeded2 ??= seedPhase2(playwright, baseURL);
        const seed = await seeded2;
        const api = await apiContext(playwright, baseURL, P2_ACTOR);
        const origin = new URL(baseURL as string).origin;
        const urls = record(page);
        try {
          // A full page load of the view, then the dialog opened on the loaded page.
          await page.goto('about:blank');
          await state.open(page, seed, api);
          await page.waitForLoadState('networkidle');
        } finally {
          await api.dispose();
        }

        const network = urls.filter((u) => networkSchemes.has(new URL(u).protocol));
        expect(network.some((u) => new URL(u).pathname === '/'), 'the document itself was recorded').toBe(true);
        expect(network.some((u) => new URL(u).pathname === '/api/v1/stream'), 'the live stream request was recorded').toBe(true);
        expect(foreignURLs(urls, origin), 'requests to another origin').toEqual([]);
        for (const u of network) expect(new URL(u).origin).toBe(origin);
      });
    }
  });
}

test('the Phase 2 list names the 2 views and the six dialogs', () => {
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
});

test('the Phase 1 list names the 3 views and the dialogs opened on them', () => {
  const names = PHASE1_STATES.map((s) => s.name);
  for (const view of ['#/tasks', '#/i/<slug>', '#/t/<ref>']) expect(names.some((n) => n.includes(view) && !n.includes('dialog'))).toBe(true);
  expect(names.filter((n) => n.includes('dialog open'))).toHaveLength(5);
});

// Direction check: the same recording and the same assertion must go red when a remote URL is on the page.
test.describe('direction check', () => {
  test.use({ viewport: viewports[0].size });

  test('a planted remote image is recorded and rejected', async ({ page, baseURL }) => {
    const origin = new URL(baseURL as string).origin;
    const remote = 'http://remote.invalid/planted.png';
    // Answer it locally so the check never depends on the network; the request is still recorded.
    await page.route(remote, (route) => route.fulfill({ status: 200, contentType: 'image/png', body: '' }));
    await page.goto('/#/initiatives');
    await expect(page.getByTestId('initiative-row').first()).toBeVisible();
    const urls = record(page);
    await page.evaluate((src) => {
      const img = document.createElement('img');
      img.src = src;
      document.body.append(img);
    }, remote);
    await expect.poll(() => urls.includes(remote)).toBe(true);
    expect(foreignURLs(urls, origin)).toEqual([remote]);
  });

  test('same-origin, data: and blob: URLs are not foreign', () => {
    const origin = 'http://127.0.0.1:1234';
    expect(foreignURLs([`${origin}/a`, 'data:image/png;base64,AAAA', 'blob:http://127.0.0.1:1234/x'], origin)).toEqual([]);
    expect(foreignURLs(['http://127.0.0.1:1235/a', 'https://cdn.example/f.woff2'], origin)).toHaveLength(2);
  });
});
