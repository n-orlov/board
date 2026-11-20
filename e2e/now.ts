import { expect, type APIRequestContext, type Locator, type Page, type PlaywrightTestArgs } from '@playwright/test';

// Helpers for the Now view's Needs me bands (SPEC 11). The open state of a band is remembered under this key, apart for
// wide and narrow screens; a spec that needs a band's items on the phone, where only Open asks and Expired start open,
// seeds every band open before the first navigation (addInitScript, and only when the page has chosen nothing yet).
export const BANDS_KEY = 'vh.now.bands.v1';
export const BAND_KEYS = ['asks', 'expired', 'watches', 'done'] as const;
export type BandKey = (typeof BAND_KEYS)[number];

// Which band holds each attention section.
export const BAND_OF = {
  open_asks: 'asks',
  expired_leases: 'expired',
  conflicts: 'expired',
  overdue_watches: 'watches',
  done_not_closed: 'done',
} as const;

// The desktop dock keeps its open states in the layout, the stacked view in the bands key: seed both.
export const LAYOUT_KEY = 'vh.now.layout.v1';

function seedAllOpen(keys: { bands: string; layout: string }): void {
  try {
    const open = { asks: true, expired: true, watches: true, done: true };
    // On the stacked view In flight and Recently touched fold too (Recently touched starts closed on a phone).
    const folds = { ...open, flight: true, recent: true };
    if (localStorage.getItem(keys.bands) === null) localStorage.setItem(keys.bands, JSON.stringify({ wide: folds, narrow: folds }));
    if (localStorage.getItem(keys.layout) === null) {
      const layout = {
        columns: [
          { width: 0.5, widgets: ['asks', 'expired', 'watches', 'done'] },
          { width: 0.5, widgets: ['flight', 'recent'] },
        ],
        heights: {},
        open,
      };
      localStorage.setItem(keys.layout, JSON.stringify(layout));
    }
  } catch {
    // A page with no storage (about:blank) has nothing to seed.
  }
}

// Seeds every band open for the pages this one loads, and for the page already on screen: a goto that only changes the
// fragment of the loaded page does not run an init script again.
export async function showAllBands(page: Page): Promise<void> {
  const keys = { bands: BANDS_KEY, layout: LAYOUT_KEY };
  await page.addInitScript(seedAllOpen, keys);
  await page.evaluate(seedAllOpen, keys);
}

export const band = (page: Page, key: BandKey): Locator => page.getByTestId(`band-${key}`);

/** The items of one attention section: the band holds them, and each item names its section. */
export const items = (page: Page, section: keyof typeof BAND_OF): Locator =>
  band(page, BAND_OF[section]).locator(`[data-testid=attention-item][data-section=${section}]`);

/** Opens a band when it is closed (a phone starts with some closed). */
export async function openBand(page: Page, key: BandKey): Promise<void> {
  const toggle = band(page, key).getByTestId('band-toggle');
  await expect(toggle).toBeVisible();
  if ((await toggle.getAttribute('aria-expanded')) === 'false') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
}

/** Opens In flight or Recently touched when it is closed (a phone starts with Recently touched closed). */
export async function openFold(page: Page, key: 'flight' | 'recent'): Promise<void> {
  const toggle = page.getByTestId(`now-${key}`).getByTestId('widget-toggle');
  await expect(toggle).toBeVisible();
  if ((await toggle.getAttribute('aria-expanded')) === 'false') await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
}

export async function actorApi(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, actor: string): Promise<APIRequestContext> {
  return playwright.request.newContext({
    baseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': actor, 'Board-Actor-Way-Back': `ralphctl status ${actor}` },
  });
}

export async function created<T = { ref: string }>(api: APIRequestContext, path: string, data: object): Promise<T> {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(201);
  return (await response.json()) as T;
}

export interface NeedsSeed {
  slug: string;
  ask: string;
  doneTask: string;
  watchTask: string;
  conflictTasks: string[];
}

/**
 * One initiative of this test's own with one item in each band: an open ask, a done task that is not closed, an overdue
 * watch and a conflict (two actors holding one resource). Filter the view to `slug` to see only these.
 */
export async function seedNeedsMe(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, name: string): Promise<NeedsSeed> {
  const slug = `e2e-now-${name}`;
  const a = await actorApi(playwright, baseURL, `now-a-${name}`);
  const b = await actorApi(playwright, baseURL, `now-b-${name}`);
  await created(a, '/api/v1/initiatives', { slug, title: `Now ${name}` });
  const task = async (title: string, extra: object = {}) => (await created(a, '/api/v1/tasks', { initiative: slug, title, ...extra })).ref;
  const ask = (await created(a, '/api/v1/asks', { question: `Which way for ${name}?`, task: await task('Needs an answer'), mode: 'poll' })).ref;
  const doneTask = await task('Finished work');
  expect((await a.post(`/api/v1/tasks/${doneTask}/done`, { data: {} })).status()).toBe(200);
  const watchTask = await task('Slow watch', { kind: 'watch', cadence_seconds: 900, next_check_at: '2020-01-01T00:00:00Z' });
  const resource = `repo:now-${name}`;
  const first = await task('Conflict one');
  const second = await task('Conflict two');
  await created(a, '/api/v1/leases', { task: first, intent: 'first edit', resources: [resource] });
  await created(b, '/api/v1/leases', { task: second, intent: 'second edit', resources: [resource] });
  await a.dispose();
  await b.dispose();
  return { slug, ask, doneTask, watchTask, conflictTasks: [first, second] };
}

export const FLIGHT_KEY = 'vh.now.window.v1';

export interface FlightSeed {
  slug: string;
  /** A task with two live leases (held by `holders`) and its ref. */
  live: string;
  holders: [string, string];
  /** A task whose only lease has ended: it is Recently touched, not In flight. */
  ended: string;
  /** The first holder's API client, for more calls; the caller disposes it. */
  api: APIRequestContext;
}

/**
 * One initiative of this test's own with a task held by two actors (In flight, two nested leases) and a task whose lease
 * was ended (Recently touched). The caller disposes `api`.
 */
export async function seedFlight(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, name: string): Promise<FlightSeed> {
  const slug = `e2e-flight-${name}`;
  const holders: [string, string] = [`flight-a-${name}`, `flight-b-${name}`];
  const a = await actorApi(playwright, baseURL, holders[0]);
  const b = await actorApi(playwright, baseURL, holders[1]);
  await created(a, '/api/v1/initiatives', { slug, title: `Flight ${name}` });
  const live = (await created(a, '/api/v1/tasks', { initiative: slug, title: 'Being worked on' })).ref;
  const ended = (await created(a, '/api/v1/tasks', { initiative: slug, title: 'Was worked on' })).ref;
  await created(a, '/api/v1/leases', { task: live, intent: `edit ${name}` });
  await created(b, '/api/v1/leases', { task: live, intent: `review ${name}` });
  const first = await created<{ lease: { ref: string } }>(a, '/api/v1/leases', { task: ended, intent: 'quick fix' });
  const response = await a.post(`/api/v1/leases/${first.lease.ref}/end`, { data: {} });
  expect(response.status(), await response.text()).toBe(200);
  await b.dispose();
  return { slug, live, holders, ended, api: a };
}
