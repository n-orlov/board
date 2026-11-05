// Frontend mutation gate: `npm run mutation` runs Stryker over stryker.config.mjs (HTML + JSON reports in reports/mutation/) and then fails when any single module under the configured path scores below the per-module break.
// `--check-only [--report <file>]` skips Stryker and judges an existing JSON report. Run from web/. The thresholds come from ci/quality.json
// (stryker.break, stryker.module_break, stryker.path) and must equal stryker.config.mjs's own thresholds.break and mutate path.
// Exit 0 passed, 1 below a threshold, 2 an input error.
import { readFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { checkModules, type MutationReportLike } from './mutationCheck.ts';

const root = process.cwd();
const args = process.argv.slice(2);
const option = (name: string, fallback: string): string => {
  const at = args.indexOf(`--${name}`);
  return at >= 0 && args[at + 1] !== undefined ? args[at + 1] : fallback;
};
const checkOnly = args.includes('--check-only');
const reportPath = path.resolve(root, option('report', 'reports/mutation/mutation.json'));
const configPath = path.resolve(root, option('config', '../ci/quality.json'));

interface Stryker {
  enabled?: boolean;
  break?: number;
  module_break?: number;
  path?: string;
}

function loadThresholds(): { break: number; moduleBreak: number; path: string } {
  const stryker = (JSON.parse(readFileSync(configPath, 'utf8')) as { stryker?: Stryker }).stryker;
  if (stryker?.enabled !== true || typeof stryker.break !== 'number' || typeof stryker.module_break !== 'number' || !stryker.path) {
    throw new Error(`${configPath} has no enabled stryker break, module_break and path`);
  }
  return { break: stryker.break, moduleBreak: stryker.module_break, path: stryker.path.replace(/^web\//, '') };
}

async function strykerConfig(): Promise<{ thresholds: { break: number }; mutate: string[] }> {
  return ((await import(pathToFileURL(path.join(root, 'stryker.config.mjs')).href)) as { default: { thresholds: { break: number }; mutate: string[] } }).default;
}

try {
  const wanted = loadThresholds();
  const config = await strykerConfig();
  if (config.thresholds.break !== wanted.break) {
    throw new Error(`stryker.config.mjs thresholds.break ${config.thresholds.break} != ci/quality.json stryker.break ${wanted.break}`);
  }
  if (!config.mutate.includes(`${wanted.path}/**/*.ts`)) {
    throw new Error(`stryker.config.mjs mutate ${JSON.stringify(config.mutate)} does not mutate ${wanted.path}`);
  }
  let strykerStatus = 0;
  if (!checkOnly) {
    const run = spawnSync(path.join(root, 'node_modules/.bin/stryker'), ['run'], { stdio: 'inherit', cwd: root });
    strykerStatus = run.status ?? 1;
  }
  const report = JSON.parse(readFileSync(reportPath, 'utf8')) as MutationReportLike;
  const { scores, failures } = checkModules(report, wanted.path, wanted.moduleBreak);
  for (const entry of scores) console.log(`MUTATION ${entry.score.toFixed(2)}%  ${entry.module}`);
  for (const failure of failures) console.error(`MUTATION FAIL ${failure}`);
  if (strykerStatus !== 0) console.error(`mutation: stryker exited ${strykerStatus} (whole-run score below the break or a run error)`);
  if (failures.length > 0 || strykerStatus !== 0) process.exit(1);
  console.log('mutation: ok');
} catch (error) {
  console.error(`mutation: ${error instanceof Error ? error.message : String(error)}`);
  process.exit(2);
}
