import { DatabaseSync } from 'node:sqlite';
import { join } from 'node:path';
import { expect, test as base, type APIRequestContext, type Page, type PlaywrightTestArgs } from '@playwright/test';

// Signs in through the mock provider, so no API key rides along (see login.spec.ts).
// R4 at the phone viewport: on each view and with each dialog/sheet of the R4 set open, every visible
// `a, button, input, select, textarea, [role=button]` has a hit area of at least 44x44 CSS px and every
// visible input, select and textarea has a computed font-size of at least 16 px (no iOS zoom on focus).
// R12 extends it to the Phase 1 views and dialogs as a finite, named list of states (PHASE1_STATES below): the views
// `#/tasks`, `#/i/<slug>` and `#/t/<ref>`, and the 9 dialogs edit initiative, close initiative, new task, edit task,
// close task, milestone, record check, add dependency and end lease.
const test = base;

const SELECTOR = 'a, button, input, select, textarea, [role=button]';
const MIN_HIT = 44;
const MIN_FONT = 16;
// Layout snaps to 1/64 px, so a 44 px box can measure 43.99997.
const SNAP = 1 / 32;

type Measured = { target: string; width: number; height: number; fontSize: number; field: boolean };

/**
 * Every matched element that is visible: rendered (not display:none or visibility:hidden), with a
 * non-empty box, and not clipped away to nothing (the sr-only skip link, clip-path: inset(50%), until it takes focus).
 * An input is measured as itself, so a visually hidden radio does not escape by being 1x1.
 */
async function measure(page: Page): Promise<Measured[]> {
  return page.evaluate((selector) => {
    const describe = (el: Element): string => {
      const label = el.getAttribute('aria-label') ?? el.getAttribute('data-testid') ?? el.textContent?.trim().slice(0, 30) ?? '';
      return `${el.tagName.toLowerCase()}[${label}]`;
    };
    const out: Measured[] = [];
    for (const el of document.querySelectorAll(selector)) {
      if (!el.checkVisibility({ visibilityProperty: true })) continue;
      const rect = el.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) continue;
      if (getComputedStyle(el).clipPath !== 'none') continue;
      out.push({
        target: describe(el),
        width: rect.width,
        height: rect.height,
        fontSize: parseFloat(getComputedStyle(el).fontSize),
        field: ['INPUT', 'SELECT', 'TEXTAREA'].includes(el.tagName),
      });
    }
    return out;
  }, SELECTOR) as Promise<Measured[]>;
}

async function expectTouchFriendly(page: Page, minimum: number) {
  const found = await measure(page);
  expect(found.length, 'the state shows interactive elements').toBeGreaterThanOrEqual(minimum);
  const small = found.filter((m) => m.width < MIN_HIT - SNAP || m.height < MIN_HIT - SNAP).map((m) => `${m.target} ${m.width}x${m.height}`);
  expect(small, 'elements with a hit area under 44x44').toEqual([]);
  const zoom = found.filter((m) => m.field && m.fontSize < MIN_FONT).map((m) => `${m.target} ${m.fontSize}px`);
  expect(zoom, 'fields with a font-size under 16px').toEqual([]);
}


// ---- Phase 1 (R12) -------------------------------------------------------------------------------------------------

const P1_SLUG = 'e2e-touch-p1';
const P1_RESOURCE = 'repo:touch-p1';

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

// One initiative with a watch task (the subject: links, a blocker, a dependent, a milestone, a check, an expired lease and a live lease)
// and a second actor holding the same resource on the dependent, so the views show a conflict.
async function seedPhase1(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined): Promise<Seed> {
  const api = await apiContext(playwright, baseURL);
  const other = await apiContext(playwright, baseURL, 'touch-p1-other');
  const created = await api.post('/api/v1/initiatives', { data: { slug: P1_SLUG, title: 'Touch Phase 1', description: 'Phase 1 touch targets', tags: ['phone', 'ux'] } });
  expect([201, 409], `seed ${P1_SLUG}: ${created.status()}`).toContain(created.status());
  const task = async (title: string, extra: object = {}) => (await send(api, '/api/v1/tasks', { initiative: P1_SLUG, title, ...extra }, 201)).ref as string;
  const watch = await task('Touch watch', {
    kind: 'watch',
    cadence_seconds: 900,
    description: 'A watch task to measure',
    external_refs: [{ label: 'PR', url: 'https://example.com/pr/1' }],
  });
  const blocker = await task('Touch p1 blocker');
  const dependent = await task('Touch p1 dependent');
  await send(api, `/api/v1/tasks/${watch}/deps/${blocker}`, {}, 200);
  await send(api, `/api/v1/tasks/${dependent}/deps/${watch}`, {}, 200);
  await send(api, `/api/v1/tasks/${watch}/milestones`, { summary: 'Halfway there' }, 200);
  await send(api, `/api/v1/tasks/${watch}/checks`, { summary: 'Nothing changed' }, 200);
  const old = await send(api, '/api/v1/leases', { task: watch, intent: 'first try', resources: [P1_RESOURCE] }, 201);
  await expireLease(api, old.lease.ref);
  const live = await send(api, '/api/v1/leases', { task: watch, intent: 'second try', resources: [P1_RESOURCE, 'repo:touch-p1-b'] }, 201);
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
  await expect(page.getByTestId('task-title')).toHaveText('Touch watch');
  await expect(page.getByTestId('lease')).toHaveCount(1);
};

type Phase1State = { name: string; minimum: number; open: (page: Page, seed: Seed) => Promise<void> };

// The finite list of Phase 1 states this spec measures at the phone: 3 views and 9 dialogs.
const PHASE1_STATES: Phase1State[] = [
  {
    name: 'view #/tasks',
    minimum: 8,
    open: async (page, seed) => {
      await page.goto(`/#/tasks?initiative=${P1_SLUG}`);
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
    },
  },
  {
    name: 'view #/i/<slug>',
    minimum: 6,
    open: async (page, seed) => {
      await page.goto(`/#/i/${P1_SLUG}`);
      await expect(page.getByTestId('initiative-title')).toHaveText('Touch Phase 1');
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
    },
  },
  { name: 'view #/t/<ref>', minimum: 10, open: taskPage },
  {
    name: 'dialog edit initiative',
    minimum: 8,
    open: async (page) => {
      await page.goto(`/#/i/${P1_SLUG}`);
      await expect(page.getByTestId('initiative-title')).toHaveText('Touch Phase 1');
      await button(page, 'Edit', true).click();
      await openDialog(page, 'Edit initiative');
    },
  },
  {
    name: 'dialog close initiative',
    minimum: 6,
    open: async (page) => {
      await page.goto(`/#/i/${P1_SLUG}`);
      await expect(page.getByTestId('initiative-title')).toHaveText('Touch Phase 1');
      await button(page, 'Close', true).click();
      await openDialog(page, 'Close initiative');
    },
  },
  {
    name: 'dialog new task',
    minimum: 14,
    open: async (page, seed) => {
      await page.goto(`/#/tasks?initiative=${P1_SLUG}`);
      await expect(page.locator(`[data-testid=task-row][data-ref=${seed.watch}]`)).toBeVisible();
      await button(page, 'New task').click();
      const dialog = await openDialog(page, 'New task');
      await dialog.getByLabel('Kind').selectOption('watch');
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
    minimum: 8,
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, 'Edit', true).click();
      await openDialog(page, 'Edit task');
    },
  },
  {
    name: 'dialog close task',
    minimum: 6,
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, 'Close', true).click();
      await openDialog(page, 'Close task');
    },
  },
  {
    name: 'dialog milestone',
    minimum: 3,
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, 'Milestone', true).click();
      await openDialog(page, 'Record milestone');
    },
  },
  {
    name: 'dialog record check',
    minimum: 3,
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, 'Record check', true).click();
      await openDialog(page, 'Record check');
    },
  },
  {
    name: 'dialog add dependency',
    minimum: 3,
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
    minimum: 3,
    open: async (page, seed) => {
      await taskPage(page, seed);
      await button(page, `End lease ${seed.lease}`, true).click();
      await openDialog(page, 'End lease');
    },
  },
];

test.describe('phone 390x844', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test.beforeEach(async ({ page, playwright, baseURL }) => {
    // Rows to measure: an initiative with tags (the key list already holds global-setup.ts's own key).
    const seed = await playwright.request.newContext({
      baseURL,
      extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
    });
    const res = await seed.post('/api/v1/initiatives', { data: { slug: 'e2e-touch', title: 'Touch targets', tags: ['phone', 'ux'] } });
    expect([201, 409]).toContain(res.status());
    if (res.status() === 201) {
      expect((await seed.post('/api/v1/tasks', { data: { initiative: 'e2e-touch', title: 'Touch task' } })).status()).toBe(201);
      expect((await seed.post('/api/v1/tasks', { data: { initiative: 'e2e-touch', title: 'Touch blocker' } })).status()).toBe(201); // for the Add dependency sheet
    }
    await seed.dispose();
    await page.goto('/#/initiatives');
    await expect(page.getByTestId('initiative-row').first()).toBeVisible();
  });

  test('the Attention view with an ask, its way back button and the filter chips', async ({ page, playwright, baseURL }) => {
    const seed = await playwright.request.newContext({
      baseURL,
      extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'touch-att', 'Board-Actor-Way-Back': 'ralphctl status touch-att' },
    });
    const made = await seed.post('/api/v1/initiatives', { data: { slug: 'e2e-touch-att', title: 'Touch Attention', tags: ['touch-att'] } });
    expect([201, 409]).toContain(made.status());
    if (made.status() === 201) {
      expect((await seed.post('/api/v1/asks', { data: { question: 'Is this big enough?', initiative: 'e2e-touch-att' } })).status()).toBe(201);
    }
    await seed.dispose();
    await page.goto('/#/attention?initiative=e2e-touch-att');
    await expect(page.getByTestId('copy-way-back').first()).toBeVisible();
    await expectTouchFriendly(page, 6);
  });

  test('the Initiatives view', async ({ page }) => {
    await expectTouchFriendly(page, 4);
  });

  test('the initiative page', async ({ page }) => {
    await page.goto('/#/i/e2e-touch');
    await expect(page.getByTestId('initiative-title')).toHaveText('Touch targets');
    await expectTouchFriendly(page, 4);
  });

  test('the Tasks view', async ({ page }) => {
    await page.goto('/#/tasks?initiative=e2e-touch');
    await expect(page.getByRole('heading', { level: 2, name: 'Tasks' })).toBeVisible();
    await expect(page.getByTestId('task-row').first()).toBeVisible();
    await expectTouchFriendly(page, 8);
  });

  test('the Tasks view with the Now panel open and a conflict', async ({ page, playwright, baseURL }) => {
    const seed = await playwright.request.newContext({
      baseURL,
      extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'touch-a' },
    });
    expect((await seed.post('/api/v1/initiatives', { data: { slug: 'e2e-touch-now', title: 'Touch Now' } })).status()).toBe(201);
    for (const [actor, title] of [['touch-a', 'Now one'], ['touch-b', 'Now two']]) {
      const made = await seed.post('/api/v1/tasks', { data: { initiative: 'e2e-touch-now', title } });
      expect(made.status()).toBe(201);
      const { ref } = (await made.json()) as { ref: string };
      const lease = await seed.post('/api/v1/leases', { data: { task: ref, intent: 'touching', resources: ['repo:touch'] }, headers: { 'Board-Actor': actor! } });
      expect(lease.status()).toBe(201);
    }
    await seed.dispose();

    await page.goto('/#/tasks?initiative=e2e-touch-now');
    await expect(page.getByTestId('task-row').first()).toBeVisible();
    await page.getByTestId('now-toggle').click();
    await expect(page.locator('[data-testid=now-conflict][data-resource="repo:touch"]')).toBeVisible();
    await expectTouchFriendly(page, 10);
  });

  test('the new-task sheet', async ({ page }) => {
    await page.goto('/#/tasks?initiative=e2e-touch');
    await expect(page.getByTestId('task-row').first()).toBeVisible();
    await page.getByRole('button', { name: 'New task' }).click();
    const dialog = page.getByRole('dialog', { name: 'New task' });
    await expect(dialog).toBeVisible();
    await dialog.getByLabel('Kind').selectOption('watch');
    await dialog.getByLabel('Find an open task').fill('e2e-touch');
    await dialog.getByTestId('blocker-options').getByRole('button').first().click(); // a chosen blocker has its remove button
    await expect(dialog.getByTestId('blockers')).toBeVisible();
    await dialog.getByLabel('Link label').fill('PR');
    await dialog.getByLabel('Link URL').fill('https://example.com/1');
    await dialog.getByRole('button', { name: 'Add link' }).click();
    await expect(dialog.getByTestId('links')).toBeVisible();
    await expectTouchFriendly(page, 14);
  });

  test('the Settings -> API keys view', async ({ page }) => {
    await page.getByTestId('nav-tabbar').getByRole('link', { name: 'Settings' }).click();
    await expect(page.getByTestId('key-row').first()).toBeVisible();
    await expectTouchFriendly(page, 5);
  });

  test('the Signed out view', async ({ page }) => {
    await page.getByRole('button', { name: 'Account menu' }).click();
    await page.getByRole('dialog', { name: 'Account' }).getByRole('button', { name: 'Sign out' }).click();
    await expect(page).toHaveURL(/#\/signed-out$/);
    await expect(page.getByTestId('signed-out')).toBeVisible();
    await expectTouchFriendly(page, 1);
  });

  test('the new-initiative sheet', async ({ page }) => {
    await page.getByRole('button', { name: 'New initiative' }).click();
    const dialog = page.getByRole('dialog', { name: 'New initiative' });
    await expect(dialog).toBeVisible();
    expect((await dialog.locator('input, textarea').count()) >= 3).toBe(true);
    await expectTouchFriendly(page, 8);
  });

  test('the new-key sheet', async ({ page }) => {
    await page.getByTestId('nav-tabbar').getByRole('link', { name: 'Settings' }).click();
    await page.getByRole('button', { name: 'New API key' }).click();
    const dialog = page.getByRole('dialog', { name: 'New API key' });
    await expect(dialog).toBeVisible();
    await expectTouchFriendly(page, 6);
  });

  test('the revoke confirmation', async ({ page }) => {
    await page.getByTestId('nav-tabbar').getByRole('link', { name: 'Settings' }).click();
    await page.getByTestId('key-row').first().getByRole('button', { name: 'Revoke' }).click();
    const dialog = page.getByRole('dialog', { name: 'Revoke API key' });
    await expect(dialog).toBeVisible();
    await expectTouchFriendly(page, 6);
  });

  test('the account menu', async ({ page }) => {
    await page.getByRole('button', { name: 'Account menu' }).click();
    const dialog = page.getByRole('dialog', { name: 'Account' });
    await expect(dialog).toBeVisible();
    expect(await dialog.locator('input[type=radio]').count()).toBe(3);
    await expectTouchFriendly(page, 8);
  });

  for (const state of PHASE1_STATES) {
    test(`${state.name} has 44 px hit areas and 16 px inputs`, async ({ page, playwright, baseURL }) => {
      seeded ??= seedPhase1(playwright, baseURL);
      const seed = await seeded;
      await state.open(page, seed);
      await expectTouchFriendly(page, state.minimum);
    });
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
});
