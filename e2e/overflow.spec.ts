import { expect, test as base, type Page } from '@playwright/test';

// R3: at 390, 820, 1024 and 1440 px the page never scrolls sideways, whatever text the API accepts. Signs in through the mock provider.
// Every case seeds the longest unbroken text the server takes (no space to wrap at): a 200-character
// title, a 40-character slug, 20 tags of 40 characters, a 64-character key name; wide glyphs (W) and
// narrow ones (i) both, since they stress the line breaking differently.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'tablet', size: { width: 820, height: 1180 } },
  { name: 'small-desktop', size: { width: 1024, height: 768 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;

const wide = (n: number, ch = 'W') => ch.repeat(n);
const tag = (i: number) => `t${String(i).padStart(2, '0')}${'w'.repeat(37)}`;
const tags = Array.from({ length: 20 }, (_, i) => tag(i));

const initiatives = [
  { slug: 'ovf-wide-title', title: wide(200), tags: ['ovf'] },
  { slug: 'ovf-narrow-title', title: wide(200, 'i'), tags: ['ovf'] },
  { slug: `ovf${"w".repeat(37)}`, title: 'Slug of the longest length', tags: ['ovf'] },
  { slug: 'ovf-max-tags', title: 'Twenty tags of forty characters', tags },
  { slug: 'ovf-url-title', title: `https://example.invalid/${'a'.repeat(176)}`, tags: ['ovf'] },
  { slug: 'ovf-mixed', title: `${wide(100)} ${wide(98, 'M')}`, tags },
];
const keyNames = [`ovf${wide(61)}`, `ovf${wide(61, 'i')}`, `ovf.${'_-.'.repeat(19)}x`];

async function overflow(page: Page) {
  return page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
    bodyScrollWidth: document.body.scrollWidth,
  }));
}

async function expectNoSidewaysScroll(page: Page) {
  const m = await overflow(page);
  expect(m.scrollWidth).toBeLessThanOrEqual(m.clientWidth);
  expect(m.bodyScrollWidth).toBeLessThanOrEqual(m.clientWidth);
}

// Nothing visible reaches past the right edge of the viewport.
async function expectNothingPastRight(page: Page, selector: string, width: number) {
  const rights = await page.locator(selector).evaluateAll((els) =>
    els.filter((el) => (el as HTMLElement).checkVisibility()).map((el) => el.getBoundingClientRect().right),
  );
  expect(rights.length).toBeGreaterThan(0);
  for (const right of rights) expect(right).toBeLessThanOrEqual(width + 0.5);
}

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test.beforeAll(async ({ playwright, baseURL }) => {
      const seed = await playwright.request.newContext({
        baseURL,
        extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
      });
      for (const body of initiatives) {
        const res = await seed.post('/api/v1/initiatives', { data: body });
        expect([201, 409], `seed ${body.slug}: ${res.status()}`).toContain(res.status());
      }
      for (const name of keyNames) {
        const res = await seed.post('/api/v1/keys', { data: { name } });
        expect([201, 409], `seed key ${name}: ${res.status()}`).toContain(res.status());
      }
      await seed.dispose();
    });

    test('the Initiatives list holds the longest accepted titles, slugs and tags without scrolling sideways', async ({ page }) => {
      await page.goto('/');
      await expect(page.locator('[data-testid=initiative-row][data-slug=ovf-wide-title]')).toBeVisible();
      for (const i of initiatives) await expect(page.locator(`[data-testid=initiative-row][data-slug=${i.slug}]`)).toHaveCount(1);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=initiative-row]', vp.size.width);
      await expectNothingPastRight(page, '[data-testid=initiative-row] [data-testid=tag]', vp.size.width);
      // The text is all still there: wrapped, never cut off.
      await expect(page.locator('[data-testid=initiative-row][data-slug=ovf-wide-title]')).toContainText(wide(200));
    });

    test('the Tasks view and the New task sheet hold the longest accepted values without scrolling sideways', async ({ page, playwright, baseURL }) => {
      const slug = `ovf${'w'.repeat(37)}`;
      const seed = await playwright.request.newContext({
        baseURL,
        extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
      });
      const made = await seed.post('/api/v1/tasks', { data: { initiative: slug, title: wide(200) } });
      expect(made.status()).toBe(201);
      const { ref } = (await made.json()) as { ref: string };
      await seed.dispose();

      await page.goto(`/#/tasks?initiative=${slug}`);
      await expect(page.locator(`[data-testid=task-row][data-ref=${ref}]`)).toBeVisible();
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=task-row], [data-testid=task-row] *', vp.size.width);

      await page.getByRole('button', { name: 'New task' }).locator('visible=true').click();
      const dialog = page.getByRole('dialog', { name: 'New task' });
      await dialog.getByLabel('Initiative').selectOption(slug);
      await dialog.getByRole('textbox', { name: 'Title' }).fill(wide(200));
      await dialog.getByLabel('Find an open task').fill(ref);
      await dialog.getByTestId('blocker-options').getByRole('button').first().click();
      await dialog.getByLabel('Link label').fill(wide(80));
      await dialog.getByLabel('Link URL').fill(`https://example.invalid/${'a'.repeat(170)}`);
      await dialog.getByRole('button', { name: 'Add link' }).click();
      await expect(dialog.getByTestId('links')).toBeVisible();
      await dialog.getByLabel('Find an open task').fill(ref);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[role=dialog] *', vp.size.width);
    });

    test('the API keys list holds the longest accepted key names without scrolling sideways', async ({ page }) => {
      await page.goto('/#/settings/keys');
      for (const name of keyNames) await expect(page.locator(`[data-testid=key-row]`, { hasText: name })).toHaveCount(1);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=key-row]', vp.size.width);
      await expect(page.locator('[data-testid=key-row]', { hasText: keyNames[0] })).toContainText(keyNames[0]);
    });

    test('the revoke confirmation names a 64-character key without scrolling sideways', async ({ page }) => {
      await page.goto('/#/settings/keys');
      const row = page.locator('[data-testid=key-row]', { hasText: keyNames[0] });
      await row.getByRole('button', { name: 'Revoke' }).click();
      const dialog = page.getByRole('dialog', { name: 'Revoke API key' });
      await expect(dialog).toBeVisible();
      await expect(dialog).toContainText(keyNames[0]);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[role=dialog] *', vp.size.width);
    });

    test('creating an initiative with the longest slug shows a toast that stays inside the viewport', async ({ page }) => {
      await page.goto('/');
      await page.getByRole('button', { name: 'New initiative' }).locator('visible=true').click();
      const dialog = page.getByRole('dialog', { name: 'New initiative' });
      const slug = `ovf-toast-${vp.name}${'x'.repeat(40)}`.slice(0, 40);
      await dialog.getByLabel('Slug').fill(slug);
      await dialog.getByRole('textbox', { name: 'Title' }).fill(wide(200));
      await dialog.getByLabel('Tags').fill(tags.join(', '));
      await expectNoSidewaysScroll(page);
      await dialog.getByRole('button', { name: 'Create initiative' }).click();
      await expect(page.getByTestId('toast')).toContainText(slug);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=toast]', vp.size.width);
    });

    test('a freshly created 64-character key shows once without scrolling sideways', async ({ page }) => {
      await page.goto('/#/settings/keys');
      await page.getByRole('button', { name: 'New API key' }).locator('visible=true').click();
      const dialog = page.getByRole('dialog', { name: 'New API key' });
      const name = `ovfnew${vp.name}${wide(64 - 6 - vp.name.length)}`;
      await dialog.getByLabel('Name').fill(name);
      await dialog.getByRole('button', { name: 'Create key' }).click();
      await expect(page.getByTestId('new-key')).toContainText(name);
      await expectNoSidewaysScroll(page);
      await expectNothingPastRight(page, '[data-testid=new-key], [data-testid=new-key] *', vp.size.width);
    });
  });
}
