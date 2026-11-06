import { expect, test as base, type Locator, type Page } from '@playwright/test';

// R8: the four dialogs/sheets of the R4 set (new initiative, new API key, revoke confirmation, account menu),
// opened by keyboard only, at the phone and the desktop viewport. Signs in through the mock provider, so no API
// key rides along (see login.spec.ts). Only the backdrop click uses the pointer: that is what it tests.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 }, nav: 'nav-tabbar' },
  { name: 'desktop', size: { width: 1440, height: 900 }, nav: 'nav-sidebar' },
] as const;

/** A short fingerprint of the focused element, to tell stops apart. */
const focused = (page: Page) =>
  page.evaluate(() => {
    const el = document.activeElement;
    if (!el || el === document.body) return 'body';
    const label = el.getAttribute('name') ?? el.getAttribute('aria-label') ?? el.textContent?.trim().slice(0, 20) ?? '';
    return `${el.tagName.toLowerCase()}[${label}${el instanceof HTMLInputElement ? ':' + el.value : ''}]`;
  });

const focusIsInside = (page: Page) => page.evaluate(() => document.activeElement?.closest('[role=dialog]') != null);

/** Tab from wherever focus is until `target` has it. Keyboard only; fails when it is not reached. */
async function tabTo(page: Page, target: Locator) {
  for (let i = 0; i < 150; i++) {
    await page.keyboard.press('Tab');
    if (await target.evaluate((el) => el === document.activeElement)) return;
  }
  throw new Error('the keyboard never reached the target');
}

/** 12 presses (more than any of the four dialogs has tab stops): focus stays inside and the sequence wraps. */
async function cycle(page: Page, key: 'Tab' | 'Shift+Tab') {
  const stops: string[] = [];
  for (let i = 0; i < 12; i++) {
    await page.keyboard.press(key);
    expect(await focusIsInside(page), `${key} press ${i + 1} keeps focus inside the dialog (on ${await focused(page)})`).toBe(true);
    stops.push(await focused(page));
  }
  expect(new Set(stops).size, `${key} visits more than one stop`).toBeGreaterThan(1);
  expect(stops.slice(1), `${key} wraps around: the first stop comes round again`).toContain(stops[0]);
}

type Case = {
  name: string;
  /** Opens nothing: where to go first, and the opener to reach by Tab. */
  setup: (page: Page, nav: string) => Promise<Locator>;
  dialog: string;
};

const cases: Case[] = [
  {
    name: 'new-initiative sheet',
    dialog: 'New initiative',
    setup: async (page) => page.getByRole('button', { name: 'New initiative' }).locator('visible=true'),
  },
  {
    name: 'new-key sheet',
    dialog: 'New API key',
    setup: async (page) => {
      await page.goto('/#/settings/keys');
      await expect(page.getByTestId('key-row').first()).toBeVisible();
      return page.getByRole('button', { name: 'New API key' }).locator('visible=true');
    },
  },
  {
    name: 'revoke confirmation',
    dialog: 'Revoke API key',
    setup: async (page) => {
      await page.goto('/#/settings/keys');
      await expect(page.getByTestId('key-row').first()).toBeVisible();
      return page.getByTestId('key-row').getByRole('button', { name: 'Revoke' }).locator('visible=true').first();
    },
  },
  {
    name: 'account menu',
    dialog: 'Account',
    setup: async (page) => page.getByRole('button', { name: 'Account menu' }),
  },
];

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test.beforeEach(async ({ page, playwright, baseURL }) => {
      const seed = await playwright.request.newContext({
        baseURL,
        extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
      });
      const res = await seed.post('/api/v1/initiatives', { data: { slug: 'e2e-dialogs', title: 'Dialog keyboard', tags: ['kbd'] } });
      expect([201, 409]).toContain(res.status());
      await seed.dispose();
      await page.goto('/');
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
    });

    for (const c of cases) {
      test(`the ${c.name}: opens by keyboard, traps focus, closes on Escape with focus back on the opener, closes on a backdrop click`, async ({ page }) => {
        const opener = await c.setup(page, vp.nav);
        await expect(opener).toBeVisible();
        const dialog = page.getByRole('dialog', { name: c.dialog });
        await expect(dialog).toBeHidden();

        // Reach the opener with Tab only and open it with Enter.
        await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
        await tabTo(page, opener);
        await page.keyboard.press('Enter');
        await expect(dialog).toBeVisible();
        await expect(dialog).toHaveAttribute('aria-modal', 'true');

        // Focus moved inside on open.
        expect(await focusIsInside(page), `focus is inside the dialog on open (on ${await focused(page)})`).toBe(true);
        await expect(opener).not.toBeFocused();

        // Tab and Shift+Tab keep focus inside, wrapping at both ends.
        await cycle(page, 'Tab');
        await cycle(page, 'Shift+Tab');

        // Escape closes it and focus is back on the opener.
        await page.keyboard.press('Escape');
        await expect(dialog).toBeHidden();
        await expect(opener).toBeFocused();

        // Open again (Enter on the opener, which has focus) and close by clicking the backdrop.
        await page.keyboard.press('Enter');
        await expect(dialog).toBeVisible();
        expect(await focusIsInside(page)).toBe(true);
        await page.getByTestId('dialog-backdrop').click({ position: { x: 5, y: 5 } });
        await expect(dialog).toBeHidden();
        await expect(opener).toBeFocused();
      });
    }
  });
}

// Every interactive element has a visible :focus-visible ring: read from computed style after keyboard focus.
async function expectRing(target: Locator) {
  const ring = () =>
    target.evaluate((el) => {
      const s = getComputedStyle(el);
      return { style: s.outlineStyle, width: parseFloat(s.outlineWidth), color: s.outlineColor, offset: parseFloat(s.outlineOffset), focused: el === document.activeElement };
    });
  const after = await ring();
  expect(after.focused, 'the element has keyboard focus').toBe(true);
  expect(after.style).toBe('solid');
  expect(after.width).toBeGreaterThanOrEqual(2);
  expect(after.color, 'the ring is not transparent').not.toMatch(/rgba\(\s*\d+,\s*\d+,\s*\d+,\s*0\s*\)|transparent/);
  expect(after.offset).toBeGreaterThanOrEqual(0);
}

for (const vp of viewports) {
  test.describe(`focus ring at ${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test.beforeEach(async ({ page, playwright, baseURL }) => {
      const seed = await playwright.request.newContext({
        baseURL,
        extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
      });
      const res = await seed.post('/api/v1/initiatives', { data: { slug: 'e2e-dialogs', title: 'Dialog keyboard', tags: ['kbd'] } });
      expect([201, 409]).toContain(res.status());
      await seed.dispose();
      await page.goto('/');
      await expect(page.getByTestId('initiative-row').first()).toBeVisible();
    });

    test('a nav entry shows a visible focus ring after keyboard focus', async ({ page }) => {
      const entry = page.getByTestId(vp.nav).getByRole('link', { name: 'Settings' });
      expect(await entry.evaluate((el) => getComputedStyle(el).outlineStyle), 'no ring before focus').toBe('none');
      await tabTo(page, entry);
      await expectRing(entry);
    });

    test('the New initiative primary button shows a visible focus ring after keyboard focus', async ({ page }) => {
      const button = page.getByRole('button', { name: 'New initiative' }).locator('visible=true');
      expect(await button.evaluate((el) => getComputedStyle(el).outlineStyle), 'no ring before focus').toBe('none');
      await tabTo(page, button);
      await expectRing(button);
    });

    test('the new-initiative title input shows a visible focus ring after keyboard focus', async ({ page }) => {
      const button = page.getByRole('button', { name: 'New initiative' }).locator('visible=true');
      await tabTo(page, button);
      await page.keyboard.press('Enter');
      const title = page.getByRole('dialog', { name: 'New initiative' }).locator('input[name=title]');
      expect(await title.evaluate((el) => getComputedStyle(el).outlineStyle), 'no ring before focus').toBe('none');
      await tabTo(page, title);
      await expectRing(title);
    });
  });
}
