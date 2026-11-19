import AxeBuilder from '@axe-core/playwright';
import { expect, test as base, type Locator, type Page } from '@playwright/test';
import { actorApi, created } from './now';

// R5: the Now navigation item's two badges, the red count of Needs me items and the green count of open tasks with a live
// lease. The shared server's other specs start and end leases and raise asks at the same time, so every assertion is a delta
// against the count read first, never an absolute number, and the page is never reloaded after the first read.
// Signs in through the mock provider, so no API key rides along.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 }, nav: 'nav-tabbar' },
  { name: 'desktop', size: { width: 1440, height: 900 }, nav: 'nav-sidebar' },
] as const;
const FIVE_SECONDS = { timeout: 5000 };

/** A badge's number, 0 while it is hidden (no element at all). */
async function count(badge: Locator): Promise<number> {
  if ((await badge.count()) === 0) return 0;
  return Number.parseInt((await badge.innerText()).replace('+', ''), 10);
}

async function scan(page: Page, theme: string) {
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))).then(() => undefined));
  const { violations } = await new AxeBuilder({ page }).analyze();
  const blocking = violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
  expect(
    blocking.map((v) => ({ rule: v.id, nodes: v.nodes.map((n) => ({ target: n.target, summary: n.failureSummary })) })),
    'serious or critical axe violations',
  ).toEqual([]);
}


// For the zero cases the counts must be the test's own, not the shared board's: the page gets a stand-in EventSource the
// test feeds (`attention` for the red count, `board` events that name a task for the green one), and the tasks read the
// green badge counts answers with as many tasks as the test says.
async function standInStream(page: Page) {
  await page.addInitScript(() => {
    type Listener = (e: { data: string }) => void;
    const w = window as unknown as { __streams: FakeStream[]; EventSource: unknown };
    class FakeStream {
      readyState = 1;
      onopen: ((e: unknown) => void) | null = null;
      onerror: ((e: unknown) => void) | null = null;
      listeners = new Map<string, Listener>();
      constructor() {
        w.__streams = [...(w.__streams ?? []), this];
        setTimeout(() => this.onopen?.({}), 0);
      }
      addEventListener(type: string, l: Listener) {
        this.listeners.set(type, l);
      }
      close() {}
    }
    w.EventSource = FakeStream;
  });
}

async function emit(page: Page, type: 'attention' | 'board', data: object) {
  await page.evaluate(
    ([t, d]) => {
      const w = window as unknown as { __streams: { listeners: Map<string, (e: { data: string }) => void> }[] };
      for (const stream of w.__streams) stream.listeners.get(t)?.({ data: d });
    },
    [type, JSON.stringify(data)] as const,
  );
}

const needsMe = (n: number) => ({ open_asks: n, done_not_closed: 0, expired_leases: 0, overdue_watches: 0, conflicts: 0, total: n });

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('starting a lease raises the green count and answering an ask lowers the red one, on a page other than Now, without a reload', async ({
      page,
      playwright,
      baseURL,
    }) => {
      const slug = `e2e-navb-${vp.name}`;
      const api = await actorApi(playwright, baseURL, `navb-${vp.name}`);
      await created(api, '/api/v1/initiatives', { slug, title: `Nav badges ${vp.name}` });
      const task = (await created(api, '/api/v1/tasks', { initiative: slug, title: 'Needs an answer' })).ref;
      const held = (await created(api, '/api/v1/tasks', { initiative: slug, title: 'Gets a lease' })).ref;
      const ask = (await created(api, '/api/v1/asks', { question: `Which way for ${vp.name}?`, task, mode: 'poll' })).ref;

      await page.goto('/#/initiatives');
      await expect(page.getByTestId('initiatives-skeleton')).toBeHidden(FIVE_SECONDS);
      await page.evaluate(() => ((window as unknown as { navMark: boolean }).navMark = true));
      const nav = page.getByTestId(vp.nav);
      const link = nav.getByRole('link', { name: /^Now/ });
      const red = nav.getByTestId('attention-badge');
      const green = nav.getByTestId('live-badge');
      // The red count has our own open ask in it by now; wait for the stream to say so (it never reads 0 after the ask).
      await expect.poll(() => count(red), FIVE_SECONDS).toBeGreaterThan(0);
      const redBefore = await count(red);
      const greenBefore = await count(green);

      await created(api, '/api/v1/leases', { task: held, intent: `badge ${vp.name}` });
      await expect.poll(async () => (await count(green)) - greenBefore, FIVE_SECONDS).toBeGreaterThanOrEqual(1);
      await expect(green).toBeVisible();
      const label = await link.getAttribute('aria-label');
      expect(label).toBeNull(); // the words ride in the content so the name keeps the visible text
      await expect(link).toHaveAccessibleName(/^Now .*open tasks? (has|have) a live lease$/);

      const answered = await api.post(`/api/v1/asks/${ask}/answer`, { data: { answer: 'left' } });
      expect(answered.status(), await answered.text()).toBe(200);
      await expect.poll(async () => (await count(red)) - redBefore, FIVE_SECONDS).toBeLessThanOrEqual(-1);
      expect(await page.evaluate(() => (window as unknown as { navMark?: boolean }).navMark)).toBe(true);
      await api.dispose();
    });

    test('each badge is hidden at zero, shown with an accessible label naming its meaning above zero, and follows the stream', async ({ page }) => {
      let live = 0;
      await standInStream(page);
      await page.route('**/api/v1/tasks?*active=true*', (route) =>
        route.fulfill({ json: { items: Array.from({ length: live }, (_, i) => ({ ref: `T-${i + 1}` })) } }),
      );
      await page.goto('/#/notes');
      const nav = page.getByTestId(vp.nav);
      await expect(nav).toBeVisible();
      const link = nav.getByRole('link', { name: /^Now/ });
      const red = nav.getByTestId('attention-badge');
      const green = nav.getByTestId('live-badge');

      // Both counts at zero: neither badge is in the page, and the link is named just "Now".
      await emit(page, 'attention', needsMe(0));
      await expect(red).toHaveCount(0);
      await expect(green).toHaveCount(0);
      await expect(link).toHaveAccessibleName('Now');

      // Red only: the green one stays hidden at zero.
      await emit(page, 'attention', needsMe(2));
      await expect(red).toHaveText('2');
      await expect(green).toHaveCount(0);
      await expect(link).toHaveAccessibleName('Now 2 items need attention');

      // A live event naming a task reads the count again: green appears beside red, each with its own words.
      live = 3;
      await emit(page, 'board', { type: 'lease_started', task: 'T-3' });
      await expect(green).toHaveText('3');
      await expect(link).toHaveAccessibleName('Now 2 items need attention 3 open tasks have a live lease');

      // Green only: the red one is hidden again at zero.
      await emit(page, 'attention', needsMe(0));
      await expect(red).toHaveCount(0);
      await expect(green).toHaveText('3');
      await expect(link).toHaveAccessibleName('Now 3 open tasks have a live lease');

      // Back to zero: green is hidden too, and no other nav entry ever carried either badge.
      live = 0;
      await emit(page, 'board', { type: 'lease_ended', task: 'T-3' });
      await expect(green).toHaveCount(0);
      await expect(link).toHaveAccessibleName('Now');
      await expect(page.getByTestId('attention-badge')).toHaveCount(0);
      await expect(page.getByTestId('live-badge')).toHaveCount(0);
    });

    for (const theme of ['light', 'dark'] as const) {
      test.describe(`${theme} theme`, () => {
        test.use({ colorScheme: theme });

        test('axe passes with both badges shown', async ({ page, playwright, baseURL }) => {
          const slug = `e2e-navb-axe-${vp.name}-${theme}`;
          const api = await actorApi(playwright, baseURL, `navb-axe-${vp.name}-${theme}`);
          await created(api, '/api/v1/initiatives', { slug, title: `Nav badges axe ${vp.name} ${theme}` });
          const task = (await created(api, '/api/v1/tasks', { initiative: slug, title: 'Held and asked about' })).ref;
          await created(api, '/api/v1/asks', { question: 'Axe?', task, mode: 'poll' });
          await created(api, '/api/v1/leases', { task, intent: 'axe scan' });
          await page.goto('/#/tasks');
          const nav = page.getByTestId(vp.nav);
          await expect(nav.getByTestId('attention-badge')).toBeVisible(FIVE_SECONDS);
          await expect(nav.getByTestId('live-badge')).toBeVisible(FIVE_SECONDS);
          await scan(page, theme);
          await api.dispose();
        });
      });
    }
  });
}
