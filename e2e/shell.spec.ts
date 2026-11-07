import { expect, test as base, type Page } from '@playwright/test';

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
const routes = [
  { hash: '#/', label: 'Initiatives', other: 'Settings', otherHash: '#/settings/keys' },
  { hash: '#/settings/keys', label: 'Settings', other: 'Initiatives', otherHash: '#/' },
] as const;

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
  const created = await seed.post('/api/v1/initiatives', { data: { slug: `shl${'w'.repeat(37)}`, title: 'W'.repeat(200) } });
  expect([201, 409]).toContain(created.status());
  const key = await seed.post('/api/v1/keys', { data: { name: `shl${'W'.repeat(61)}` } });
  expect([201, 409]).toContain(key.status());
  await seed.dispose();
});

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    for (const route of routes) {
      test(`on ${route.hash} only the ${vp.nav} navigation is shown, a nav click moves aria-current, nothing overflows`, async ({ page }) => {
        await page.goto('/' + route.hash);
        const nav = page.getByTestId(vp.nav);
        await expect(nav).toBeVisible();
        for (const other of allNavs.filter((id) => id !== vp.nav)) await expect(page.getByTestId(other)).toBeHidden();
        await expect(nav.getByRole('link')).toHaveCount(3);

        await expect(nav.locator('[aria-current=page]')).toHaveCount(1);
        await expect(nav.getByRole('link', { name: route.label })).toHaveAttribute('aria-current', 'page');
        await expect(nav.getByRole('link', { name: route.other })).not.toHaveAttribute('aria-current', /.*/);
        await noHorizontalOverflow(page);

        await nav.getByRole('link', { name: route.other }).click();
        await expect(page).toHaveURL(new RegExp(`${route.otherHash}$`));
        await expect(nav.getByRole('link', { name: route.other })).toHaveAttribute('aria-current', 'page');
        await expect(nav.getByRole('link', { name: route.label })).not.toHaveAttribute('aria-current', /.*/);
        await noHorizontalOverflow(page);
      });
    }

    test('the account menu opens in the dialog component from the account button and closes on Escape', async ({ page }) => {
      await page.goto('/');
      const topbar = page.getByTestId('topbar');
      const foot = page.getByTestId('sidebar-foot');
      if (vp.name === 'phone') {
        await expect(topbar).toBeVisible();
        await expect(topbar).toContainText('board');
        await expect(page.getByTestId('topbar-title')).toHaveText('Initiatives');
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

    await page.goto('/');
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
