import { expect, test } from './fixtures';

test('the embedded UI shell renders at /', async ({ page }) => {
  const response = await page.goto('/');
  expect(response?.status()).toBe(200);
  await expect(page.getByRole('heading', { level: 1, name: 'board' })).toBeVisible();
  await expect(page.locator('#app')).toContainText('API at');
});

test('a request without the key is refused with a problem document', async ({ playwright, baseURL }) => {
  const bare = await playwright.request.newContext({ baseURL, extraHTTPHeaders: {} });
  const response = await bare.get('/readyz');
  expect(response.status()).toBe(401);
  expect(response.headers()['content-type']).toBe('application/problem+json');
  expect((await bare.get('/healthz')).status()).toBe(200);
  await bare.dispose();
});
