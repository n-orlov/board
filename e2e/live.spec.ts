import { expect, test as base, type APIRequestContext, type PlaywrightTestArgs } from '@playwright/test';

// The live indicator in the shell (SPEC 11): `live` while the stream is open, `offline` while the browser
// is offline, `live` again once it is back. Signs in through the mock provider like shell.spec.ts, so no
// API key rides along. No fixed sleep: every step waits on the indicator itself, at most 5 s.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const FIVE_SECONDS = { timeout: 5000 };

// A request from the test itself, never from the page: the change a view must show without a reload.
async function apiAs(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined): Promise<APIRequestContext> {
  return playwright.request.newContext({
    baseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'live-spec' },
  });
}

async function post<T = Record<string, never>>(api: APIRequestContext, path: string, data: object, status: number): Promise<T> {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
  return (await response.json()) as T;
}

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('the indicator reads live, offline after the browser goes offline, live again when it is back', async ({
      page,
      context,
    }) => {
      await page.goto('/');
      // getByRole sees only the indicator of the layout on screen (the other two are display: none).
      const indicator = page.getByRole('status', { name: /^Live updates:/ });

      await expect(indicator).toBeVisible(FIVE_SECONDS);
      await expect(indicator).toHaveText('live', FIVE_SECONDS);
      await expect(indicator).toHaveAccessibleName('Live updates: live', FIVE_SECONDS);

      await context.setOffline(true);
      await expect(indicator).toHaveText('offline', FIVE_SECONDS);
      await expect(indicator).toHaveAccessibleName('Live updates: offline', FIVE_SECONDS);

      await context.setOffline(false);
      await expect(indicator).toHaveText('live', FIVE_SECONDS);
      await expect(indicator).toHaveAccessibleName('Live updates: live', FIVE_SECONDS);
    });

    test('the Initiatives list shows an initiative and its task counts changed through the API, without a reload', async ({ page, playwright, baseURL }) => {
      const api = await apiAs(playwright, baseURL);
      const slug = `e2e-live-list-${vp.name}`;
      await page.goto('/#/initiatives');
      await expect(page.getByTestId('initiatives-skeleton')).toBeHidden(FIVE_SECONDS);
      await expect(page.locator(`[data-testid="initiative-row"][data-slug="${slug}"]`)).toHaveCount(0);
      // The page is never reloaded: a marker set on it must survive to the end.
      await page.evaluate(() => ((window as unknown as { liveMark: boolean }).liveMark = true));

      await post(api, '/api/v1/initiatives', { slug, title: `Live list ${vp.name}` }, 201);
      const row = page.locator(`[data-testid="initiative-row"][data-slug="${slug}"]`);
      await expect(row).toBeVisible(FIVE_SECONDS);
      await expect(row.getByTestId('initiative-counts')).toHaveText('No tasks', FIVE_SECONDS);
      const before = await row.getByTestId('initiative-counts').innerText();

      await post(api, '/api/v1/tasks', { initiative: slug, title: 'Counted through the API' }, 201);
      await expect(row.getByTestId('initiative-counts')).not.toHaveText(before, FIVE_SECONDS);
      expect(await page.evaluate(() => (window as unknown as { liveMark?: boolean }).liveMark)).toBe(true);

      await post(api, `/api/v1/initiatives/${slug}/close`, { force: true }, 200);
      await expect(row).toBeHidden(FIVE_SECONDS);
    });

    test('the initiative page shows a task added, finished and a title edited through the API, without a reload', async ({ page, playwright, baseURL }) => {
      const api = await apiAs(playwright, baseURL);
      const slug = `e2e-live-page-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug, title: `Live page ${vp.name}` }, 201);
      await page.goto(`/#/i/${slug}`);
      await expect(page.getByTestId('initiative-title')).toHaveText(`Live page ${vp.name}`, FIVE_SECONDS);
      await page.evaluate(() => ((window as unknown as { liveMark: boolean }).liveMark = true));

      const { ref } = await post<{ ref: string }>(api, '/api/v1/tasks', { initiative: slug, title: 'Added through the API' }, 201);
      const row = page.getByTestId('task-row').filter({ hasText: 'Added through the API' });
      await expect(row).toBeVisible(FIVE_SECONDS);
      await expect(row).toContainText('open');

      await post(api, `/api/v1/tasks/${ref}/done`, {}, 200);
      await expect(row).toContainText('done', FIVE_SECONDS);

      const patched = await api.patch(`/api/v1/initiatives/${slug}`, { data: { title: `Renamed ${vp.name}` } });
      expect(patched.status(), await patched.text()).toBe(200);
      await expect(page.getByTestId('initiative-title')).toHaveText(`Renamed ${vp.name}`, FIVE_SECONDS);
      expect(await page.evaluate(() => (window as unknown as { liveMark?: boolean }).liveMark)).toBe(true);
    });

    test('a refresh keeps an expanded note body and the scroll position of the initiative page', async ({ page, playwright, baseURL }) => {
      const api = await apiAs(playwright, baseURL);
      const slug = `e2e-live-keep-${vp.name}`;
      await post(api, '/api/v1/initiatives', { slug, title: `Live keep ${vp.name}` }, 201);
      for (let n = 1; n <= 12; n += 1) await post(api, '/api/v1/tasks', { initiative: slug, title: `Filler task ${n}` }, 201);
      await post(api, '/api/v1/notes', { initiative: slug, summary: 'Kept open', body: 'Body **stays**' }, 201);
      await page.goto(`/#/i/${slug}`);
      const note = page.getByTestId('initiative-notes').getByTestId('note').first();
      await note.getByRole('button', { name: /body of/ }).click();
      await expect(note.getByTestId('note-body')).toBeVisible(FIVE_SECONDS);
      await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
      const scrolled = await page.evaluate(() => Math.round(window.scrollY));
      expect(scrolled).toBeGreaterThan(0);

      await post(api, '/api/v1/tasks', { initiative: slug, title: 'Arrives while reading' }, 201);
      await expect(page.getByTestId('task-row').filter({ hasText: 'Arrives while reading' })).toBeVisible(FIVE_SECONDS);
      await expect(note.getByTestId('note-body')).toBeVisible();
      expect(await page.evaluate(() => Math.round(window.scrollY))).toBeGreaterThanOrEqual(scrolled);
    });
  });
}
