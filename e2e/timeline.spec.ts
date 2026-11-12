import { DatabaseSync } from 'node:sqlite';
import { join } from 'node:path';
import { expect, test, type APIRequestContext, type Locator, type Page, type PlaywrightTestArgs } from '@playwright/test';

// The Timeline view at #/timeline (PRD R6, SPEC 11), read through the DOM vis-timeline draws: the initiative group and
// its task rows, the planned, actual and lease bars and the markers, collapsing the initiative, the zoom buttons and
// the wheel, a click on a task or a lease opening the task page, and one SVG arrow per dependency from the blocking
// task's bar to the dependent task's bar (end points inside the two bars, so the spec fails when none is drawn).
// The board is shared with the other specs, so each test seeds an initiative of its own and filters the view to it.
const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const FIVE_SECONDS = { timeout: 5000 };
const DAY = 86_400_000;
const HOUR = 3_600_000;

async function post<T = Record<string, never>>(api: APIRequestContext, path: string, data: object, status: number): Promise<T> {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
  return (await response.json()) as T;
}

// The e2e store is written to move history: tasks and leases are made now through the API, then dated back so the bars
// have a length on a window of three weeks (the same way attention.spec.ts runs a lease out).
function store<T>(work: (db: DatabaseSync) => T): T {
  const db = new DatabaseSync(join(process.env.E2E_WORK!, 'board.db'));
  try {
    db.exec('PRAGMA busy_timeout = 5000');
    return work(db);
  } finally {
    db.close();
  }
}

function dateTask(slug: string, ref: string, createdAt: number) {
  const seq = Number(ref.slice(slug.length + 1));
  const changed = store((db) =>
    db.prepare('UPDATE tasks SET created_at = ? WHERE seq = ? AND initiative_id = (SELECT id FROM initiatives WHERE slug = ?)').run(createdAt, seq, slug),
  );
  expect(changed.changes).toBe(1);
}

function dateLease(ref: string, startedAt: number, endedAt: number | null) {
  const changed = store((db) => db.prepare('UPDATE leases SET started_at = ?, ended_at = COALESCE(?, ended_at) WHERE n = ?').run(startedAt, endedAt, Number(ref.slice(1))));
  expect(changed.changes).toBe(1);
}

interface Seed {
  slug: string;
  api: APIRequestContext;
  /** In row order: A and C block B and D; E has no dependency. */
  a: string;
  b: string;
  c: string;
  d: string;
  e: string;
  ended: string;
  live: string;
}

// One initiative with five tasks (A to E), two dependency pairs (B waits on A, D waits on C), a planned bar on A, an ended
// and a live lease on A, and a milestone and a check on A.
async function seed(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, name: string): Promise<Seed> {
  const slug = `e2e-tl-${name}`;
  const api = await playwright.request.newContext({
    baseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': `tl-${name}` },
  });
  const now = Date.now();
  await post(api, '/api/v1/initiatives', { slug, title: `Timeline ${name}` }, 201);
  const task = async (title: string, extra: object = {}) => (await post<{ ref: string }>(api, '/api/v1/tasks', { initiative: slug, title, ...extra }, 201)).ref;
  const a = await task('Task A', { kind: 'watch', cadence_seconds: 900, planned_start: new Date(now - 7 * DAY).toISOString(), planned_end: new Date(now + 2 * DAY).toISOString() });
  const b = await task('Task B');
  const c = await task('Task C');
  const d = await task('Task D');
  const e = await task('Task E');
  await post(api, `/api/v1/tasks/${b}/deps/${a}`, {}, 200);
  await post(api, `/api/v1/tasks/${d}/deps/${c}`, {}, 200);
  await post(api, `/api/v1/tasks/${a}/milestones`, { summary: 'Halfway there' }, 200);
  await post(api, `/api/v1/tasks/${a}/checks`, { summary: 'Nothing changed' }, 200);
  const ended = (await post<{ lease: { ref: string } }>(api, '/api/v1/leases', { task: a, intent: 'first try', resources: [`repo:tl-${name}-1`] }, 201)).lease.ref;
  await post(api, `/api/v1/leases/${ended}/end`, { outcome: 'handed over' }, 200);
  const live = (await post<{ lease: { ref: string } }>(api, '/api/v1/leases', { task: a, intent: 'second try', resources: [`repo:tl-${name}-2`] }, 201)).lease.ref;
  [a, b, c, d, e].forEach((ref, i) => dateTask(slug, ref, now - (6 - i) * DAY));
  dateLease(ended, now - 4 * DAY, now - 3 * DAY);
  dateLease(live, now - 2 * DAY, null);
  return { slug, api, a, b, c, d, e, ended, live };
}

const open = async (page: Page, s: Seed) => {
  await page.goto(`/#/timeline?initiative=${s.slug}`);
  await expect(page.getByTestId('timeline-view')).toBeVisible();
  await expect(page.locator('.vis-item.tl-actual')).toHaveCount(5, FIVE_SECONDS);
  await expect(page.locator('.vis-loading-screen')).toHaveCount(0, FIVE_SECONDS); // vis-timeline's cover is off once it has drawn
};

const labels = (page: Page) => page.locator('.vis-labelset .vis-label');
const bar = (page: Page, ref: string) => page.locator(`.vis-item.tl-id-act-${ref}`);
const span = async (page: Page) => Number(await page.getByTestId('timeline-span').getAttribute('data-span-ms'));
const arrows = (page: Page) => page.locator('svg.tl-arrows path.tl-arrow');

interface Pt {
  x: number;
  y: number;
}
interface Drawn {
  from: string;
  to: string;
  start: Pt;
  end: Pt;
  fromBox: { left: number; top: number; right: number; bottom: number } | null;
  toBox: { left: number; top: number; right: number; bottom: number } | null;
}

// Every arrow with its end points (the first and last point of its path, in page coordinates) and the page boxes of the
// two bars it joins, read from the DOM.
async function drawn(page: Page): Promise<Drawn[]> {
  return page.evaluate(() => {
    const svg = document.querySelector('svg.tl-arrows')!;
    const at = svg.getBoundingClientRect();
    const box = (ref: string) => {
      const el = document.querySelector(`.vis-item.tl-id-act-${ref}`);
      if (el === null) return null;
      const r = el.getBoundingClientRect();
      return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
    };
    return [...svg.querySelectorAll('path.tl-arrow')].map((path) => {
      const nums = (path.getAttribute('d') ?? '').match(/-?\d+(\.\d+)?/g)!.map(Number);
      const from = path.getAttribute('data-from')!;
      const to = path.getAttribute('data-to')!;
      return {
        from,
        to,
        start: { x: at.left + nums[0]!, y: at.top + nums[1]! },
        end: { x: at.left + nums[nums.length - 2]!, y: at.top + nums[nums.length - 1]! },
        fromBox: box(from),
        toBox: box(to),
      };
    });
  });
}

const within = (p: Pt, box: Drawn['fromBox']) =>
  box !== null && p.x >= box.left - 1 && p.x <= box.right + 1 && p.y >= box.top - 1 && p.y <= box.bottom + 1;

// Exactly the two pairs, each path ending in the bars of its two tasks.
async function expectTwoArrows(page: Page, s: Seed) {
  await expect(arrows(page)).toHaveCount(2, FIVE_SECONDS);
  const all = await drawn(page);
  expect(all.map((x) => `${x.from}>${x.to}`).sort()).toEqual([`${s.a}>${s.b}`, `${s.c}>${s.d}`].sort());
  for (const x of all) {
    expect(within(x.start, x.fromBox), `${x.from}>${x.to} starts in the bar of ${x.from}: ${JSON.stringify(x)}`).toBe(true);
    expect(within(x.end, x.toBox), `${x.from}>${x.to} ends in the bar of ${x.to}: ${JSON.stringify(x)}`).toBe(true);
  }
}

async function expectNoSidewaysScroll(page: Page) {
  const m = await page.evaluate(() => ({ s: document.documentElement.scrollWidth, c: document.documentElement.clientWidth, b: document.body.scrollWidth }));
  expect(m.s).toBeLessThanOrEqual(m.c);
  expect(m.b).toBeLessThanOrEqual(m.c);
}

async function expect44(locator: Locator) {
  const box = await locator.boundingBox();
  expect(box, 'a visible box').not.toBeNull();
  expect(box!.width).toBeGreaterThanOrEqual(44);
  expect(box!.height).toBeGreaterThanOrEqual(44);
}

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('draws the initiative group, its five task rows, the planned, actual and lease bars and the markers', async ({ page, playwright, baseURL }) => {
      const s = await seed(playwright, baseURL, `rows-${vp.name}`);
      await open(page, s);
      await expect(labels(page)).toHaveCount(6);
      await expect(labels(page).first()).toContainText(`Timeline rows-${vp.name}`);
      for (const [i, ref] of [s.a, s.b, s.c, s.d, s.e].entries()) await expect(labels(page).nth(i + 1)).toContainText(`${ref} Task ${'ABCDE'[i]}`);
      await expect(page.locator('.vis-item.tl-planned')).toHaveCount(1);
      await expect(page.locator('.vis-item.tl-lease')).toHaveCount(2);
      await expect(page.locator('.vis-item.tl-lease-ended')).toHaveCount(1);
      await expect(page.locator('.vis-item.tl-lease-live')).toHaveCount(1);
      await expect(page.locator('.vis-item.vis-point.tl-marker-milestone')).toHaveText('◆');
      await expect(page.locator('.vis-item.vis-point.tl-marker-check')).toHaveText('✓');
      await expect(page.locator('.vis-item.tl-actual.tl-open')).toHaveCount(5);
      await s.api.dispose();
    });

    test('collapsing the initiative hides its task rows and bars and expanding brings them back', async ({ page, playwright, baseURL }) => {
      const s = await seed(playwright, baseURL, `fold-${vp.name}`);
      await open(page, s);
      await labels(page).first().click();
      await expect(labels(page)).toHaveCount(1, FIVE_SECONDS);
      await expect(page.locator('.vis-item.tl-actual')).toHaveCount(0);
      await expect(arrows(page)).toHaveCount(0);
      await labels(page).first().click();
      await expect(labels(page)).toHaveCount(6, FIVE_SECONDS);
      await expect(page.locator('.vis-item.tl-actual')).toHaveCount(5);
      await expectTwoArrows(page, s);
      await s.api.dispose();
    });

    test('the zoom buttons and the wheel change the range between an hour and a quarter', async ({ page, playwright, baseURL }) => {
      const s = await seed(playwright, baseURL, `zoom-${vp.name}`);
      await open(page, s);
      await expect(page.getByTestId('timeline-span')).toContainText('Showing 3 weeks');
      const start = await span(page);
      expect(Math.round(start / DAY)).toBe(21);

      await page.getByTestId('zoom-in').click();
      await expect.poll(() => span(page)).toBeLessThan(start);
      const closer = await span(page);
      await page.getByTestId('zoom-out').click();
      await expect.poll(() => span(page)).toBeGreaterThan(closer);

      // The wheel over the chart zooms (one direction in, the other out), the same range the buttons move.
      const centre = (await page.locator('.vis-panel.vis-center').boundingBox())!;
      await page.mouse.move(centre.x + centre.width / 2, centre.y + centre.height / 2);
      const before = await span(page);
      await page.mouse.wheel(0, 400);
      await expect.poll(() => span(page)).not.toBe(before);
      const wheeled = await span(page);
      await page.mouse.wheel(0, -400);
      await expect.poll(() => span(page)).not.toBe(wheeled);
      expect(Math.sign(wheeled - before) * Math.sign((await span(page)) - wheeled)).toBe(-1); // back the other way

      for (let i = 0; i < 20 && (await page.getByTestId('zoom-in').isEnabled()); i++) await page.getByTestId('zoom-in').click();
      await expect(page.getByTestId('zoom-in')).toBeDisabled();
      expect(await span(page)).toBeGreaterThanOrEqual(HOUR);
      expect(await span(page)).toBeLessThanOrEqual(HOUR * 1.02);
      await expect(page.getByTestId('timeline-span')).toContainText('Showing 1 hour');

      for (let i = 0; i < 20 && (await page.getByTestId('zoom-out').isEnabled()); i++) await page.getByTestId('zoom-out').click();
      await expect(page.getByTestId('zoom-out')).toBeDisabled();
      expect(await span(page)).toBeLessThanOrEqual(92 * DAY);
      expect(await span(page)).toBeGreaterThanOrEqual(92 * DAY * 0.98);
      await expect(page.getByTestId('timeline-span')).toContainText('Showing 3 months');
      await expect(page.getByTestId('zoom-in')).toBeEnabled();
      await s.api.dispose();
    });

    test('a click on a task bar, a lease bar or a task row opens the task page', async ({ page, playwright, baseURL }) => {
      const s = await seed(playwright, baseURL, `open-${vp.name}`);
      await open(page, s);
      await bar(page, s.e).click();
      await expect(page).toHaveURL(new RegExp(`#/t/${s.e}$`));
      await expect(page.getByTestId('task-title')).toHaveText('Task E', FIVE_SECONDS);

      await open(page, s);
      await page.locator('.vis-item.tl-lease-live').click();
      await expect(page).toHaveURL(new RegExp(`#/t/${s.a}$`));
      await expect(page.getByTestId('task-title')).toHaveText('Task A', FIVE_SECONDS);

      await open(page, s);
      await labels(page).nth(3).click(); // the row of task C
      await expect(page).toHaveURL(new RegExp(`#/t/${s.c}$`));
      await expect(page.getByTestId('task-title')).toHaveText('Task C', FIVE_SECONDS);
      await s.api.dispose();
    });

    test('draws one arrow per dependency, from the blocking bar to the dependent bar, and redraws it on zoom, pan and collapse', async ({ page, playwright, baseURL }) => {
      const s = await seed(playwright, baseURL, `arrows-${vp.name}`);
      await open(page, s);
      await expectTwoArrows(page, s);
      const first = JSON.stringify(await drawn(page));

      await page.getByTestId('zoom-in').click();
      await expect.poll(async () => JSON.stringify(await drawn(page))).not.toBe(first); // moved with the bars
      await expectTwoArrows(page, s);

      const chart = (await page.getByTestId('timeline-chart').boundingBox())!;
      const y = chart.y + chart.height - 20;
      const before = JSON.stringify(await drawn(page));
      await page.mouse.move(chart.x + chart.width / 2, y);
      await page.mouse.down();
      await page.mouse.move(chart.x + chart.width / 2 + 40, y, { steps: 4 });
      await page.mouse.up();
      await expect.poll(async () => JSON.stringify(await drawn(page))).not.toBe(before);
      await expectTwoArrows(page, s);

      await labels(page).first().click();
      await expect(arrows(page)).toHaveCount(0, FIVE_SECONDS);
      await labels(page).first().click();
      await expectTwoArrows(page, s);
      await s.api.dispose();
    });

    test('keeps the page from scrolling sideways, and its controls and rows are 44 px targets', async ({ page, playwright, baseURL }) => {
      const s = await seed(playwright, baseURL, `fit-${vp.name}`);
      await open(page, s);
      await expectNoSidewaysScroll(page);
      await expect44(page.getByTestId('zoom-in'));
      await expect44(page.getByTestId('zoom-out'));
      if (vp.name === 'phone') {
        for (const label of await labels(page).all()) {
          const box = (await label.boundingBox())!;
          expect(box.height).toBeGreaterThanOrEqual(44);
        }
        const chart = (await page.getByTestId('timeline-chart').boundingBox())!;
        expect(chart.x + chart.width).toBeLessThanOrEqual(vp.size.width + 0.5);
      }
      await page.getByTestId('zoom-out').click();
      await expectNoSidewaysScroll(page);
      await s.api.dispose();
    });
  });
}
