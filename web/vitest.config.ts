import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts', 'scripts/**/*.test.ts'],
    // ci/suite.sh points both at its own result directory.
    reporters: ['default', ['junit', { outputFile: process.env.WEB_JUNIT_FILE ?? 'junit/junit.xml' }]],
    coverage: {
      provider: 'v8',
      // Write coverage even when a test fails, so a red run still has evidence.
      reportOnFailure: true,
      reportsDirectory: process.env.WEB_COVERAGE_DIR ?? 'coverage',
      reporter: ['text', 'json', 'json-summary', 'lcov'],
      include: ['src/lib/**/*.ts'],
      exclude: ['src/**/*.test.ts'],
    },
  },
});
