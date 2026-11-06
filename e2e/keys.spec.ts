import { expect, test as base } from '@playwright/test';

// Signs in through the mock provider, so no API key rides along (see login.spec.ts).
const test = base;

test('a key created on the Settings -> API keys page is shown once and gets 401 once revoked', async ({ page, playwright, baseURL }) => {
  await page.goto('/');
  await expect(page.getByTestId('email')).toHaveText('operator@example.test');
  await page.getByRole('link', { name: 'Settings' }).click();
  await expect(page.getByRole('heading', { level: 2, name: 'API keys' })).toBeVisible();
  // global-setup.ts's own key is already listed; the new one is not.
  await expect(page.getByTestId('key-row').first()).toBeVisible();
  await expect(page.locator('[data-testid=key-row][data-name=e2e-ui]')).toHaveCount(0);

  await page.getByLabel('Name').fill('e2e-ui');
  await page.getByLabel('Expires in').fill('90d');
  await page.getByRole('button', { name: 'Create key' }).click();

  const shown = page.getByTestId('new-key-value');
  await expect(shown).toHaveText(/^brd_[A-Za-z0-9]{52}$/);
  const key = (await shown.textContent()) ?? '';
  const row = page.locator('[data-testid=key-row][data-name=e2e-ui]');
  await expect(row).toHaveCount(1);
  await expect(row.getByTestId('key-status')).toHaveText('active');
  await expect(row).toContainText(key.slice(4, 12));
  // The listing never carries the key: it is not on the page once dismissed, nor after a reload.
  expect(await row.textContent()).not.toContain(key);

  const bare = await playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${key}`, 'Board-Actor': 'e2e-agent' } });
  expect((await bare.get('/api/v1/keys')).status()).toBe(200);

  await page.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByTestId('new-key')).toHaveCount(0);
  await page.reload();
  await expect(row).toHaveCount(1);
  await expect(page.getByText(key)).toHaveCount(0);

  await row.getByRole('button', { name: 'Revoke' }).click();
  await expect(row.getByTestId('key-status')).toHaveText('revoked');
  await expect(row.getByRole('button', { name: 'Revoke' })).toHaveCount(0);

  const refused = await bare.get('/api/v1/keys');
  expect(refused.status()).toBe(401);
  expect(refused.headers()['content-type']).toBe('application/problem+json');
  await bare.dispose();
});

test('a name that is already taken is refused on the page and no key is shown', async ({ page }) => {
  await page.goto('/#/settings/keys');
  await page.getByLabel('Name').fill('dup-ui');
  await page.getByRole('button', { name: 'Create key' }).click();
  await expect(page.getByTestId('new-key')).toBeVisible();
  await page.getByRole('button', { name: 'Done' }).click();

  await page.getByLabel('Name').fill('dup-ui');
  await page.getByRole('button', { name: 'Create key' }).click();
  await expect(page.getByRole('alert')).toContainText('dup-ui');
  await expect(page.getByTestId('new-key')).toHaveCount(0);
});
