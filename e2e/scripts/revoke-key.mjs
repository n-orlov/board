// Drives a headless browser through the mock provider's login and the Settings -> API keys
// page's Revoke button, for features/ui_keys_steps_test.go. Usage: node revoke-key.mjs <base URL> <key name>.
// Exits 0 once the page lists the key as revoked, else 1 with the reason on stderr.
import { chromium } from '@playwright/test';

const [baseURL, name] = process.argv.slice(2);
if (!baseURL || !name) {
  console.error('usage: node revoke-key.mjs <base URL> <key name>');
  process.exit(2);
}

const browser = await chromium.launch();
let code = 0;
try {
  const page = await browser.newPage({ baseURL });
  page.setDefaultTimeout(15000);
  // No session yet: the server sends this navigation through /auth/login and the mock provider.
  await page.goto('/#/settings/keys');
  const row = page.locator(`[data-testid=key-row][data-name="${name}"]`);
  await row.waitFor();
  await row.getByRole('button', { name: 'Revoke' }).click();
  await row.getByTestId('key-status').filter({ hasText: 'revoked' }).waitFor();
} catch (e) {
  console.error(String(e));
  code = 1;
} finally {
  await browser.close();
}
process.exit(code);
