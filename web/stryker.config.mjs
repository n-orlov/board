// Frontend mutation testing (SPEC 15.4): every module under src/lib is mutated; tests live next to them.
// Stryker's `thresholds.break` is a whole-run score; scripts/mutation/mutation.ts additionally fails when any single module
// scores below the per-module break. Both are 80, and ci/quality.json (stryker.break, stryker.module_break, stryker.path) holds
// the same numbers: the gate in ci/qualitycheck compares them and fails when this file disagrees with it.
import path from 'node:path';

/** @type {import('@stryker-mutator/api/core').PartialStrykerOptions} */
export default {
  testRunner: 'vitest-file',
  plugins: ['@stryker-mutator/vitest-runner', path.resolve('scripts/mutation/vitestFileRunner.mjs')],
  vitest: { configFile: 'vitest.mutation.config.ts', related: false },
  mutate: ['src/lib/**/*.ts', '!src/lib/**/*.test.ts'],
  reporters: ['clear-text', 'progress', 'html', 'json'],
  htmlReporter: { fileName: 'reports/mutation/mutation.html' },
  jsonReporter: { fileName: 'reports/mutation/mutation.json' },
  thresholds: { high: 90, low: 85, break: 80 },
  coverageAnalysis: 'perTest',
  tempDirName: '.stryker-tmp',
  cleanTempDir: true,
};
