import { expect, test as base } from '@playwright/test';

// The live indicator in the shell (SPEC 11): `live` while the stream is open, `offline` while the browser
// is offline, `live` again once it is back. Signs in through the mock provider like shell.spec.ts, so no
// API key rides along. No fixed sleep: every step waits on the indicator itself, at most 5 s.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const FIVE_SECONDS = { timeout: 5000 };

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('the indicator reads live, offline after the browser goes offline, live again when it is back', async ({
      page,
      context,
    }) => {
      await page.goto('/');
      // getByRole sees only the indicator of the layout on screen (the other two are display: none).
      const indicator = page.getByRole('status', { name: /^Live updates:/ });

      await expect(indicator).toBeVisible(FIVE_SECONDS);
      await expect(indicator).toHaveText('live', FIVE_SECONDS);
      await expect(indicator).toHaveAccessibleName('Live updates: live', FIVE_SECONDS);

      await context.setOffline(true);
      await expect(indicator).toHaveText('offline', FIVE_SECONDS);
      await expect(indicator).toHaveAccessibleName('Live updates: offline', FIVE_SECONDS);

      await context.setOffline(false);
      await expect(indicator).toHaveText('live', FIVE_SECONDS);
      await expect(indicator).toHaveAccessibleName('Live updates: live', FIVE_SECONDS);
    });
  });
}
