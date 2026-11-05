// Vitest config used by Stryker (stryker.config.mjs): the unit-test config for src/ only, without the JUnit reporter and coverage
// (Stryker brings its own per-test coverage). Same tests, same assertions. Not mergeConfig: that would concatenate `include`
// and pull the scripts/ tests (which spawn node) into every mutant run.
import base from './vitest.config.ts';

const test = base.test ?? {};
export default {
  ...base,
  test: { ...test, include: ['src/**/*.test.ts'], reporters: ['default'], coverage: { enabled: false } },
};
