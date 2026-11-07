import { expect, test as base, type Page } from '@playwright/test';

// Signs in through the mock provider, so no API key rides along (see login.spec.ts).
// R4 at the phone viewport: on each view and with each dialog/sheet of the R4 set open, every visible
// `a, button, input, select, textarea, [role=button]` has a hit area of at least 44x44 CSS px and every
// visible input, select and textarea has a computed font-size of at least 16 px (no iOS zoom on focus).
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
    await seed.dispose();
    await page.goto('/');
    await expect(page.getByTestId('initiative-row').first()).toBeVisible();
  });

  test('the Initiatives view', async ({ page }) => {
    await expectTouchFriendly(page, 4);
  });

  test('the initiative page', async ({ page }) => {
    await page.goto('/#/i/e2e-touch');
    await expect(page.getByTestId('initiative-title')).toHaveText('Touch targets');
    await expectTouchFriendly(page, 4);
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
});
