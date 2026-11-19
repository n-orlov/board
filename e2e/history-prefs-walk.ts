import { expect, type APIRequestContext, type Page, type PlaywrightTestArgs } from '@playwright/test';

// Shared by history_entry_prefs.spec.ts and timeline-history-prefs.spec.ts. Each history entry restores its OWN
// filters and sort on Back and Forward (PRD R7), on the routes that have them: an entry left at the defaults (a bare fragment) comes back at the defaults even after a later visit to the same route
// changed and saved its preferences, and the entry that changed them comes back with them. A genuinely new bare link
// still restores the saved preferences (R16). Every test creates what it needs under `e2e-hep-` slugs.
export const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
export const FIVE_SECONDS = { timeout: 5000 };
const AWAY = '#/now';

export async function post(api: APIRequestContext, path: string, data: object, status: number): Promise<{ ref: string }> {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
  return (await response.json()) as { ref: string };
}

export const apiContext = (playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined, actor = 'playwright') =>
  playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': actor } });

const fragment = (page: Page) => new URL(page.url()).hash;
/** A navigation by link: a new history entry, no key yet. */
const follow = (page: Page, hash: string) => page.evaluate((h) => { window.location.hash = h; }, hash);
/** Two animation frames: a view that rewrote the fragment on opening has done so by then. */
const settled = (page: Page) => page.evaluate(() => new Promise<void>((done) => requestAnimationFrame(() => requestAnimationFrame(() => done()))));

export interface Route {
  path: string;
  /** What the page shows for the preference this route changes, the way a person reads it. */
  read: (page: Page) => Promise<string>;
  /** Changes the preference through the UI and returns the fragment the page then shows. */
  change: (page: Page) => Promise<string>;
}

export const selectRoute = (path: string, testid: string, value: string, query: string): Route => ({
  path,
  read: (p) => p.getByTestId(testid).inputValue(),
  change: async (p) => {
    await p.getByTestId(testid).selectOption(value);
    const want = `${path}?${query}`;
    await expect(p).toHaveURL((url) => url.hash === want, FIVE_SECONDS);
    return want;
  },
});

export const sortRoute = (path: string, column: string, phone: boolean): Route => ({
  path,
  read: async (p) => (phone ? ((await p.getByTestId('sort-select').inputValue()) || 'default') : ((await p.locator('th', { has: p.getByTestId(`sort-${column}`) }).getAttribute('aria-sort')) ?? '')),
  change: async (p) => {
    if (phone) await p.getByTestId('sort-select').selectOption(`${column}:desc`);
    else for (let i = 0; i < 2; i++) await p.getByTestId(`sort-${column}`).click();
    const want = `${path}?sort=${column}&dir=desc`;
    await expect(p).toHaveURL((url) => url.hash === want, FIVE_SECONDS);
    return want;
  },
});

/** Back to the entry left at the defaults, whatever a later visit saved; and every other entry as it was. */
export async function walk(page: Page, route: Route) {
  await page.goto(route.path);
  await expect.poll(() => route.read(page), FIVE_SECONDS).not.toBe('');
  const byDefault = await route.read(page);
  expect(fragment(page)).toBe(route.path);

  await follow(page, AWAY); // entry 2
  await expect(page).toHaveURL((url) => url.hash === AWAY, FIVE_SECONDS);
  await follow(page, route.path); // entry 3: a new bare link, nothing saved yet
  await expect.poll(() => route.read(page), FIVE_SECONDS).toBe(byDefault);
  const changedFragment = await route.change(page); // saved
  const changed = await route.read(page);
  expect(changed).not.toBe(byDefault);
  await follow(page, AWAY); // entry 4
  await expect(page).toHaveURL((url) => url.hash === AWAY, FIVE_SECONDS);

  const atDefault = async () => {
    await expect(page).toHaveURL((url) => url.hash === route.path, FIVE_SECONDS);
    await expect.poll(() => route.read(page), FIVE_SECONDS).toBe(byDefault);
    await settled(page);
    expect(fragment(page), 'a revisited default entry must not take a later visit\'s saved preferences').toBe(route.path);
    expect(await route.read(page)).toBe(byDefault);
  };
  const atChanged = async () => {
    await expect(page).toHaveURL((url) => url.hash === changedFragment, FIVE_SECONDS);
    await expect.poll(() => route.read(page), FIVE_SECONDS).toBe(changed);
  };

  await page.goBack(); // entry 3
  await atChanged();
  await page.goBack(); // entry 2
  await expect(page).toHaveURL((url) => url.hash === AWAY, FIVE_SECONDS);
  await page.goBack(); // entry 1
  await atDefault();
  await page.goForward();
  await page.goForward(); // entry 3 again
  await atChanged();
  await page.goBack();
  await page.goBack(); // entry 1 again
  await atDefault();

  // A genuinely new bare link restores what was saved.
  await follow(page, AWAY);
  await follow(page, route.path);
  await expect(page).toHaveURL((url) => url.hash === changedFragment, FIVE_SECONDS);
  await expect.poll(() => route.read(page), FIVE_SECONDS).toBe(changed);
}

