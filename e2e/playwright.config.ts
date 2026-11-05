import { createServer } from 'node:net';
import { defineConfig } from '@playwright/test';

// A free port, asked of the kernel once per run: Playwright loads this file in
// the runner and again in every worker, so the first load publishes the port in
// the environment and every later load (a child inherits it) reuses it.
async function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const probe = createServer();
    probe.once('error', reject);
    probe.listen(0, '127.0.0.1', () => {
      const { port } = probe.address() as { port: number };
      probe.close(() => resolve(port));
    });
  });
}

const port = Number(process.env.E2E_PORT ?? (process.env.E2E_PORT = String(await freePort())));
// ci/suite.sh points this at its own result directory; every reporter and the
// test artifacts live under it.
const out = process.env.E2E_OUT ?? 'out';

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.ts',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  // No retries anywhere: a flaky test must fail here. The only retry is
  // ci/suite.sh's own repeat of a failed run, which records it as flaky.
  retries: 0,
  workers: 1,
  reporter: [
    ['html', { outputFolder: `${out}/playwright-report`, open: 'never' }],
    ['list'],
    ['allure-playwright', { resultsDir: `${out}/allure-results` }],
    ['json', { outputFile: `${out}/results.json` }],
    ['junit', { outputFile: `${out}/junit.xml` }],
  ],
  timeout: 30000,
  expect: { timeout: 10000 },
  outputDir: `${out}/test-results`,
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  // The real binary, built from this checkout, on the free port, against a
  // freshly migrated and seeded SQLite store (see serve.sh).
  webServer: {
    command: 'sh serve.sh',
    url: `http://127.0.0.1:${port}/healthz`,
    reuseExistingServer: false,
    timeout: 180000,
    env: { E2E_PORT: String(port) },
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
});
