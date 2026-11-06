import { expect, test as base, type Page } from '@playwright/test';

// R1: the UI makes no cross-origin request (no web font, CDN, remote icon or script). Signs in through the
// mock provider (another port, so another origin: recording starts only afterwards, and no API key rides
// along), then records every request the page issues while it loads each of the three views at the phone and
// the desktop viewport: 3 views x 2 viewports = 6 visits. Every recorded http(s)/ws(s) URL must have the board
// server's origin. data: and blob: URLs never touch the network and are not origins; they are left out.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;

const views = [
  {
    name: 'Initiatives',
    hash: '#/',
    ready: async (page: Page) => {
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
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
      await page.goto('/');
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
  });
}

// Direction check: the same recording and the same assertion must go red when a remote URL is on the page.
test.describe('direction check', () => {
  test.use({ viewport: viewports[0].size });

  test('a planted remote image is recorded and rejected', async ({ page, baseURL }) => {
    const origin = new URL(baseURL as string).origin;
    const remote = 'http://remote.invalid/planted.png';
    // Answer it locally so the check never depends on the network; the request is still recorded.
    await page.route(remote, (route) => route.fulfill({ status: 200, contentType: 'image/png', body: '' }));
    await page.goto('/');
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
