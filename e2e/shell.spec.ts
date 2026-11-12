import { expect, test as base, type Page } from '@playwright/test';
import { ROUTES } from '../web/src/lib/routes';

// The responsive shell: one navigation surface per viewport (phone tab bar, tablet icon rail,
// desktop sidebar), the skip link, the account menu. Signs in through the mock provider, so no
// API key rides along (see login.spec.ts).
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 }, nav: 'nav-tabbar' },
  { name: 'tablet', size: { width: 820, height: 1180 }, nav: 'nav-rail' },
  { name: 'desktop', size: { width: 1440, height: 900 }, nav: 'nav-sidebar' },
] as const;
const allNavs = ['nav-tabbar', 'nav-rail', 'nav-sidebar'];

// The route table the shell is built from: the navs must list exactly its entries that are not pages inside another view.
const navRoutes = ROUTES.filter((route) => !('parent' in route));
const SEED_SLUG = `shl${'w'.repeat(37)}`; // the longest slug, with a 200-character title
const seeded = { taskRef: '' };

// The listed states: every view of the table, the two parameter views at a real slug and a real ref. `view` is the
// route-table id the fragment must select, `loaded` a test id that exists only once the view has its content (so the
// overflow check sees the long seeded text); its nav entry (the view itself, or its parent) is the one marked current.
const states = [
  { name: '#/', hash: () => '#/', view: 'attention', loaded: 'section-open_asks' },
  { name: '#/attention', hash: () => '#/attention', view: 'attention', loaded: 'section-open_asks' },
  { name: '#/initiatives', hash: () => '#/initiatives', view: 'initiatives', loaded: 'initiative-row' },
  { name: '#/tasks', hash: () => '#/tasks', view: 'tasks', loaded: 'task-row' },
  { name: '#/i/<slug>', hash: () => `#/i/${SEED_SLUG}`, view: 'initiative', loaded: 'initiative-title' },
  { name: '#/timeline', hash: () => '#/timeline', view: 'timeline', loaded: 'timeline-chart' },
  { name: '#/actors', hash: () => '#/actors', view: 'actors', loaded: 'actors-view' },
  { name: '#/notes', hash: () => '#/notes', view: 'notes', loaded: 'notes-view' },
  { name: '#/t/<ref>', hash: () => `#/t/${seeded.taskRef}`, view: 'task', loaded: 'task-title' },
  { name: '#/settings/keys', hash: () => '#/settings/keys', view: 'keys', loaded: 'key-row' },
] as const;

function viewOf(id: string) {
  const route = ROUTES.find((r) => r.id === id);
  if (route === undefined) throw new Error(`no route ${id} in the table`);
  return route;
}
// The nav entry of a view: the view itself, or the view it is a page of.
const navIdOf = (id: string): string => ('parent' in viewOf(id) ? (viewOf(id) as { parent: string }).parent : id);

async function noHorizontalOverflow(page: Page) {
  const { scrollWidth, clientWidth } = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(scrollWidth).toBeLessThanOrEqual(clientWidth);
}

// The overflow check must see the longest unbroken text the API takes, not only short titles:
// seed a 200-character title, a 40-character slug and a 64-character key name before any view loads.
test.beforeAll(async ({ playwright, baseURL }) => {
  const seed = await playwright.request.newContext({
    baseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
  });
  const created = await seed.post('/api/v1/initiatives', { data: { slug: SEED_SLUG, title: 'W'.repeat(200) } });
  expect([201, 409]).toContain(created.status());
  // One task with the longest title for `#/t/<ref>`; a re-run finds it by its title instead of adding another.
  const title = 'T'.repeat(200);
  const tasks = (await (await seed.get(`/api/v1/tasks?initiative=${SEED_SLUG}`)).json()) as { items: { ref: string; title: string }[] };
  const found = tasks.items.find((task) => task.title === title);
  if (found !== undefined) seeded.taskRef = found.ref;
  else {
    const task = await seed.post('/api/v1/tasks', { data: { initiative: SEED_SLUG, title } });
    expect(task.status()).toBe(201);
    seeded.taskRef = ((await task.json()) as { ref: string }).ref;
  }
  const key = await seed.post('/api/v1/keys', { data: { name: `shl${'W'.repeat(61)}` } });
  expect([201, 409]).toContain(key.status());
  await seed.dispose();
});

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    for (const state of states) {
      test(`on ${state.name} only the ${vp.nav} navigation is shown, its entries are the route table's, aria-current marks the view's entry, nothing overflows`, async ({ page }) => {
        await page.goto('/' + state.hash());
        const nav = page.getByTestId(vp.nav);
        await expect(nav).toBeVisible();
        for (const other of allNavs.filter((id) => id !== vp.nav)) await expect(page.getByTestId(other)).toBeHidden();

        // The route table drives the nav: one link per entry that is not a page inside another view, in table order.
        const links = nav.getByRole('link');
        await expect(links).toHaveCount(navRoutes.length);
        expect(await links.evaluateAll((els) => els.map((el) => el.getAttribute('href')))).toEqual(navRoutes.map((route) => route.hash));
        await expect(nav.locator('[aria-current=page]')).toHaveCount(1);
        const current = navIdOf(state.view);
        for (const route of navRoutes) {
          const link = nav.getByRole('link', { name: route.label });
          if (route.id === current) await expect(link).toHaveAttribute('aria-current', 'page');
          else await expect(link).not.toHaveAttribute('aria-current', /.*/);
        }
        await expect(page.getByTestId(state.loaded).first()).toBeVisible();
        // The live indicator is on screen in this layout with its accessible name (the other two layouts' are display: none).
        const indicator = page.getByRole('status', { name: /^Live updates: (live|reconnecting|offline)$/ });
        await expect(indicator).toHaveCount(1);
        await expect(indicator).toBeVisible();
        await expect(indicator).toHaveAccessibleName(/^Live updates: (live|reconnecting|offline)$/);
        if (vp.name === 'phone') await expect(page.getByTestId('topbar-title')).toHaveText(viewOf(state.view).title);
        await noHorizontalOverflow(page);

        // A nav click moves aria-current to the clicked entry; then back to this state through the fragment.
        for (const target of navRoutes.filter((route) => route.id !== current)) {
          await nav.getByRole('link', { name: target.label }).click();
          await expect(page).toHaveURL(new RegExp(`${target.hash}$`));
          await expect(nav.locator('[aria-current=page]')).toHaveCount(1);
          await expect(nav.getByRole('link', { name: target.label })).toHaveAttribute('aria-current', 'page');
          await noHorizontalOverflow(page);
          await page.goto('/' + state.hash());
          await expect(page.getByTestId(state.loaded).first()).toBeVisible();
          await expect(nav.getByRole('link', { name: viewOf(current).label })).toHaveAttribute('aria-current', 'page');
        }
      });
    }

    test('the account menu opens in the dialog component from the account button and closes on Escape', async ({ page }) => {
      await page.goto('/');
      const topbar = page.getByTestId('topbar');
      const foot = page.getByTestId('sidebar-foot');
      if (vp.name === 'phone') {
        await expect(topbar).toBeVisible();
        await expect(topbar).toContainText('board');
        await expect(page.getByTestId('topbar-title')).toHaveText('Attention');
        await expect(foot).toBeHidden();
      } else {
        await expect(topbar).toBeHidden();
      }
      if (vp.name === 'desktop') {
        await expect(foot).toBeVisible();
        await expect(foot.getByTestId('email')).toHaveText('operator@example.test');
        await expect(foot.getByRole('button', { name: 'Sign out' })).toBeVisible();
      }

      const button = page.getByRole('button', { name: 'Account menu' });
      await expect(button).toHaveCount(1); // the hidden shells' buttons are out of the accessibility tree
      await button.click();
      const dialog = page.getByRole('dialog', { name: 'Account' });
      await expect(dialog).toBeVisible();
      await expect(dialog.getByTestId('menu-email')).toHaveText('operator@example.test');
      await expect(dialog.getByRole('button', { name: 'Sign out' })).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(dialog).toHaveCount(0);
      await expect(button).toBeFocused();
    });

    test('Skip to content is the first focusable element and moves focus to the main region', async ({ page }) => {
      await page.goto('/#/settings/keys');
      await expect(page.getByTestId('nav-tabbar').or(page.getByTestId('nav-rail')).or(page.getByTestId('nav-sidebar')).first()).toBeAttached();
      await page.keyboard.press('Tab');
      const skip = page.getByRole('link', { name: 'Skip to content' });
      await expect(skip).toBeFocused();
      await expect(skip).toBeVisible();
      await page.keyboard.press('Enter');
      await expect(page.locator('main#main')).toBeFocused();
      // The skip link is not a route: the fragment stays what it was.
      expect(new URL(page.url()).hash).toBe('#/settings/keys');
    });
  });
}

// The Attention badge is driven by the stream's `attention` message: the page gets a stand-in EventSource the test
// feeds, so the counts are the test's, not whatever the board holds.
async function feedStream(page: Page) {
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

async function sendTotal(page: Page, total: number) {
  await page.evaluate((n) => {
    const w = window as unknown as { __streams: { listeners: Map<string, (e: { data: string }) => void> }[] };
    const counts = { open_asks: n, done_not_closed: 0, expired_leases: 0, overdue_watches: 0, conflicts: 0, total: n };
    for (const stream of w.__streams) stream.listeners.get('attention')?.({ data: JSON.stringify(counts) });
  }, total);
}

test.describe('the route table and the navigation order', () => {
  test('the table holds Attention first and Notes, and the default route is Attention', async ({ page }) => {
    expect(ROUTES.map((route) => route.id)).toEqual(['attention', 'initiatives', 'initiative', 'tasks', 'task', 'timeline', 'actors', 'archive', 'notes', 'keys']);
    expect(navRoutes.map((route) => route.label)).toEqual(['Attention', 'Initiatives', 'Tasks', 'Timeline', 'Actors', 'Notes', 'Settings']);
    await page.setViewportSize({ width: 1440, height: 900 });
    for (const hash of ['/', '/#/', '/#/nowhere']) {
      await page.goto(hash);
      await expect(page.getByTestId('nav-sidebar').getByRole('link', { name: 'Attention' })).toHaveAttribute('aria-current', 'page');
      await expect(page.getByTestId('section-open_asks').first()).toBeVisible();
    }
    const links = page.getByTestId('nav-sidebar').getByRole('link');
    expect(await links.evaluateAll((els) => els.map((el) => el.getAttribute('href')))).toEqual(['#/attention', '#/initiatives', '#/tasks', '#/timeline', '#/actors', '#/notes', '#/settings/keys']);
  });
});

for (const vp of viewports) {
  test.describe(`the Attention badge at ${vp.name}`, () => {
    test.use({ viewport: vp.size });

    for (const [total, text] of [
      [0, ''],
      [3, '3'],
      [120, '99+'],
    ] as const) {
      test(`shows ${text === '' ? 'no badge' : text} for counts.total ${total}, with an accessible label`, async ({ page }) => {
        await feedStream(page);
        await page.goto('/#/tasks');
        const nav = page.getByTestId(vp.nav);
        await expect(nav).toBeVisible();
        await sendTotal(page, total);
        const badge = nav.getByTestId('attention-badge');
        const link = nav.getByRole('link', { name: /^Attention/ });
        if (total === 0) {
          await expect(badge).toHaveCount(0);
          await expect(link).toHaveAccessibleName('Attention');
        } else {
          await expect(badge).toHaveText(text);
          await expect(link).toHaveAccessibleName(`Attention ${total} items need attention`);
        }
        // No other entry carries a badge, and the page does not overflow.
        await expect(nav.getByTestId('attention-badge')).toHaveCount(total === 0 ? 0 : 1);
        await noHorizontalOverflow(page);
        // The count follows the stream: back to 0 hides the badge, 3 again shows it.
        await sendTotal(page, 0);
        await expect(badge).toHaveCount(0);
        await sendTotal(page, 3);
        await expect(badge).toHaveText('3');
      });
    }
  });
}

test.describe('the phone tab bar', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('holds Attention, Initiatives, Tasks, Timeline, Actors, Notes and Settings, all of them fitting in 44 px targets at 390 px, even with the badge at 99+', async ({ page }) => {
    await feedStream(page);
    await page.goto('/#/attention');
    const nav = page.getByTestId('nav-tabbar');
    await expect(nav).toBeVisible();
    await sendTotal(page, 120);
    await expect(nav.getByTestId('attention-badge')).toHaveText('99+');
    const links = nav.getByRole('link');
    await expect(links).toHaveCount(7);
    // Everything that fits is a link of its own; there is no "more" entry while the table fits the bar.
    await expect(nav.getByRole('button')).toHaveCount(0);
    const boxes = await links.evaluateAll((els) =>
      els.map((el) => {
        const r = el.getBoundingClientRect();
        return { name: el.textContent?.trim() ?? '', left: r.left, right: r.right, width: r.width, height: r.height };
      }),
    );
    let previousRight = 0;
    for (const box of boxes) {
      expect(box.width, `${box.name} width`).toBeGreaterThanOrEqual(44);
      expect(box.height, `${box.name} height`).toBeGreaterThanOrEqual(44);
      expect(box.left, `${box.name} left edge`).toBeGreaterThanOrEqual(previousRight - 0.5);
      expect(box.right, `${box.name} right edge`).toBeLessThanOrEqual(390.5);
      previousRight = box.right;
    }
    // No label is clipped inside its link.
    const clipped = await links.evaluateAll((els) => els.filter((el) => el.scrollWidth > el.clientWidth).map((el) => el.textContent));
    expect(clipped).toEqual([]);
    await noHorizontalOverflow(page);
  });
});

test.describe('phone list with the tab bar', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('the last row of a 30+ initiatives list is not covered by the tab bar at the bottom of the page', async ({ page, playwright, baseURL }) => {
    const seed = await playwright.request.newContext({
      baseURL,
      extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
    });
    for (let i = 0; i < 32; i++) {
      const slug = `shell-${String(i).padStart(2, '0')}`;
      const res = await seed.post('/api/v1/initiatives', { data: { slug, title: `Shell list ${i}` } });
      expect([201, 409]).toContain(res.status());
    }
    await seed.dispose();

    await page.goto('/#/initiatives');
    await expect(page.getByTestId('initiative-row')).not.toHaveCount(0);
    expect(await page.getByTestId('initiative-row').count()).toBeGreaterThanOrEqual(30);
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
    const last = page.getByTestId('initiative-row').last();
    await last.scrollIntoViewIfNeeded();
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
    const row = await last.boundingBox();
    const bar = await page.getByTestId('tabbar').boundingBox();
    if (row === null || bar === null) throw new Error('the last row or the tab bar has no box');
    expect(row.y + row.height).toBeLessThanOrEqual(bar.y);
    // The bar is fixed to the bottom edge of the viewport.
    expect(bar.y + bar.height).toBeCloseTo(844, 0);
    await noHorizontalOverflow(page);
  });
});
