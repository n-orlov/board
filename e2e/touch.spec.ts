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
    if (res.status() === 201) {
      expect((await seed.post('/api/v1/tasks', { data: { initiative: 'e2e-touch', title: 'Touch task' } })).status()).toBe(201);
      expect((await seed.post('/api/v1/tasks', { data: { initiative: 'e2e-touch', title: 'Touch blocker' } })).status()).toBe(201); // for the Add dependency sheet
    }
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

  test('the task page and its dependency and milestone sheets', async ({ page }) => {
    await page.goto('/#/tasks?initiative=e2e-touch');
    await page.getByTestId('task-row').first().getByRole('link').first().click();
    await expect(page.getByTestId('task-title')).toHaveText('Touch task');
    await expectTouchFriendly(page, 8);

    await page.getByRole('button', { name: 'Add dependency' }).click();
    const dep = page.getByRole('dialog', { name: 'Add dependency' });
    await dep.getByLabel('Find an open task').fill('touch blocker');
    await expect(dep.getByTestId('dep-options')).toBeVisible();
    await expectTouchFriendly(page, 3);
    await dep.getByRole('button', { name: 'Cancel' }).click();

    await page.getByRole('button', { name: 'Milestone' }).click();
    await expect(page.getByRole('dialog', { name: 'Record milestone' })).toBeVisible();
    await expectTouchFriendly(page, 3);
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

  test('the edit-initiative and close-initiative sheets', async ({ page }) => {
    await page.goto('/#/i/e2e-touch');
    await expect(page.getByTestId('initiative-title')).toHaveText('Touch targets');
    await page.getByRole('button', { name: 'Edit' }).click();
    await expect(page.getByRole('dialog', { name: 'Edit initiative' })).toBeVisible();
    await expectTouchFriendly(page, 8);
    await page.getByRole('button', { name: 'Cancel' }).click();
    await page.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(page.getByRole('dialog', { name: 'Close initiative' })).toBeVisible();
    await expectTouchFriendly(page, 6);
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
