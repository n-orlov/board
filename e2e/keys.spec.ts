import { expect, test as base, type Page } from '@playwright/test';

// Signs in through the mock provider, so no API key rides along (see login.spec.ts).
// The Settings -> API keys view at the phone, tablet and desktop size: the table or the cards, the create
// form in the bottom sheet or the dialog, and the once-shown key box with its Copy and Done.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'tablet', size: { width: 820, height: 1180 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;

const newKey = (page: Page) => page.getByRole('button', { name: 'New API key' });

async function openForm(page: Page) {
  await newKey(page).click();
  const dialog = page.getByRole('dialog', { name: 'New API key' });
  await expect(dialog).toBeVisible();
  return dialog;
}

const overflow = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size, permissions: ['clipboard-read', 'clipboard-write'] });
    const keyName = (n: string) => `e2e-${vp.name}-${n}`;

    test('a key created on the Settings -> API keys page is shown once, copied, works, and gets 401 once revoked', async ({ page, playwright, baseURL }) => {
      const name = keyName('ui');
      await page.goto('/');
      await page.getByRole('link', { name: 'Settings' }).click();
      await expect(page.getByRole('heading', { level: 2, name: 'API keys' })).toBeVisible();
      // global-setup.ts's own key is already listed; the new one is not.
      await expect(page.getByTestId('key-row').first()).toBeVisible();
      const row = page.locator(`[data-testid=key-row][data-name=${name}]`);
      await expect(row).toHaveCount(0);

      // The form sits at the bottom edge on the phone and is centred on the desktop
      // (the sheet slides up on open: wait for it to settle).
      const dialog = await openForm(page);
      const bottom = async () => {
        const box = await dialog.boundingBox();
        return box === null ? -1 : Math.round(box.y + box.height);
      };
      if (vp.size.width < 768) await expect.poll(bottom).toBe(vp.size.height);
      else expect(await bottom()).toBeLessThan(vp.size.height);

      await dialog.getByLabel('Name').fill(name);
      await dialog.getByLabel('Expires in').fill('90d');
      await dialog.getByRole('button', { name: 'Create key' }).click();
      await expect(page.getByRole('dialog')).toHaveCount(0);

      // The once-shown key: a highlighted monospace box that wraps, so the page never scrolls sideways.
      const box = page.getByTestId('new-key');
      const shown = page.getByTestId('new-key-value');
      await expect(shown).toHaveText(/^brd_[A-Za-z0-9]{52}$/);
      const key = (await shown.textContent()) ?? '';
      const style = await shown.evaluate((el) => {
        const s = getComputedStyle(el);
        return { family: s.fontFamily, wrap: s.overflowWrap, wordBreak: s.wordBreak, white: s.whiteSpace };
      });
      expect(style.family).toMatch(/mono|Menlo|Consolas/);
      expect(`${style.wrap} ${style.wordBreak}`).toMatch(/break-word|break-all|anywhere/);
      const boxBounds = await box.boundingBox();
      expect(boxBounds).not.toBeNull();
      expect((boxBounds?.x ?? -1) + (boxBounds?.width ?? 0)).toBeLessThanOrEqual(vp.size.width);
      expect(await shown.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(0);
      expect(await overflow(page)).toBeLessThanOrEqual(0);
      expect((await shown.boundingBox())?.height ?? 0).toBeGreaterThan(vp.size.width < 768 ? 40 : 20);

      // The row is listed as active, with the prefix and a relative creation time, and never carries the key.
      await expect(row).toHaveCount(1);
      await expect(row.getByTestId('key-status')).toHaveText('active');
      await expect(row).toContainText(key.slice(4, 12));
      await expect(row.locator('time').first()).toHaveText('just now');
      expect(await row.textContent()).not.toContain(key);

      // Copy puts exactly that key on the clipboard and confirms it.
      await expect(page.getByTestId('copied')).toHaveCount(0);
      await page.getByRole('button', { name: 'Copy' }).click();
      await expect(page.getByTestId('copied')).toHaveText('Copied');
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(key);

      const bare = await playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${key}`, 'Board-Actor': 'e2e-agent' } });
      expect((await bare.get('/api/v1/keys')).status()).toBe(200);

      // Done drops the key from the page, and a reload does not bring it back.
      await page.getByRole('button', { name: 'Done' }).click();
      await expect(box).toHaveCount(0);
      await expect(page.getByText(key)).toHaveCount(0);
      await page.reload();
      await expect(row).toHaveCount(1);
      await expect(page.getByText(key)).toHaveCount(0);
      expect(await overflow(page)).toBeLessThanOrEqual(0);

      await row.getByRole('button', { name: 'Revoke' }).click();
      await expect(row.getByTestId('key-status')).toHaveText('revoked');
      await expect(row.getByRole('button', { name: 'Revoke' })).toHaveCount(0);

      const refused = await bare.get('/api/v1/keys');
      expect(refused.status()).toBe(401);
      expect(refused.headers()['content-type']).toBe('application/problem+json');
      await bare.dispose();
    });

    test('the list is a table from 768 px and cards below it', async ({ page }) => {
      await page.goto('/#/settings/keys');
      const row = page.getByTestId('key-row').first();
      await expect(row).toBeVisible();
      const display = (loc: ReturnType<Page['locator']>) => loc.evaluate((el) => getComputedStyle(el).display);
      const head = page.locator('thead');
      if (vp.size.width < 768) {
        expect(await display(row)).toBe('flex');
        expect((await head.boundingBox())?.width ?? 0).toBeLessThanOrEqual(1);
      } else {
        expect(await display(row)).toBe('table-row');
        await expect(head).toBeVisible();
        await expect(head.getByRole('columnheader')).toHaveText(['Name', 'Prefix', 'Status', 'Created', 'Last used', 'Expires', 'Actions']);
      }
      // The status is a badge with a border of its tone (active -> success token).
      const badge = row.getByTestId('key-status').locator('span');
      expect(await badge.evaluate((el) => getComputedStyle(el).borderTopWidth)).toBe('1px');
      expect(await overflow(page)).toBeLessThanOrEqual(0);
    });

    test('a name that is already taken is refused on the page and no key is shown', async ({ page }) => {
      const name = keyName('dup');
      await page.goto('/#/settings/keys');
      let dialog = await openForm(page);
      await dialog.getByLabel('Name').fill(name);
      await dialog.getByRole('button', { name: 'Create key' }).click();
      await expect(page.getByTestId('new-key')).toBeVisible();
      await page.getByRole('button', { name: 'Done' }).click();
      await expect(page.getByTestId('new-key')).toHaveCount(0);

      dialog = await openForm(page);
      await dialog.getByLabel('Name').fill(name);
      await dialog.getByRole('button', { name: 'Create key' }).click();
      // The refusal shows inside the form, which stays open with what was typed.
      await expect(dialog.getByRole('alert')).toContainText(name);
      await expect(dialog.getByLabel('Name')).toHaveValue(name);
      await expect(page.getByTestId('new-key')).toHaveCount(0);
      await expect(page.getByTestId('new-key-value')).toHaveCount(0);
    });
  });
}
