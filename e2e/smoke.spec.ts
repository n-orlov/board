import { expect, test } from '@playwright/test';

test('the embedded UI shell renders at /', async ({ page }) => {
  const response = await page.goto('/');
  expect(response?.status()).toBe(200);
  await expect(page.getByRole('heading', { level: 1, name: 'board' })).toBeVisible();
  await expect(page.locator('#app')).toContainText('API at');
});
