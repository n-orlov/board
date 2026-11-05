// TS CRAP gate: ESLint cyclomatic complexity x Vitest (v8) coverage per function, ceiling from ci/quality.json (crap_ts.ceiling).
// No baseline, no exemptions: a function above the ceiling fails the gate (exit 1). Exit 2 is an input error.
//
//   node scripts/crap/crap.ts [--src src] [--coverage coverage/coverage-final.json] [--config ../ci/quality.json] [--root .]
//
// Run `npm run test` first; coverage comes from the coverage-final.json Vitest writes. Run from web/.
import { ESLint } from 'eslint';
import tseslint from 'typescript-eslint';
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { toMetrics, type FileCoverage, type FunctionComplexity } from './crapReport.ts';
import { judgeCrap, scoreFunctions, type FunctionMetrics } from './crapScore.ts';
import { functionRanges, type FunctionFacts } from './functionRanges.ts';

const TEST_FILES = ['**/*.test.ts', '**/*.spec.ts', '**/*.d.ts'];

function fail(message: string): never {
  console.error(`crap: ${message}`);
  process.exit(2);
}

function option(name: string, fallback: string): string {
  const at = process.argv.indexOf(`--${name}`);
  return at >= 0 && process.argv[at + 1] !== undefined ? process.argv[at + 1] : fallback;
}

/** The ceiling lives in ci/quality.json only. */
export function readCeiling(configFile: string): number {
  if (!existsSync(configFile)) fail(`${configFile} is missing`);
  const crapTs = (JSON.parse(readFileSync(configFile, 'utf8')) as { crap_ts?: { enabled?: boolean; ceiling?: number } }).crap_ts;
  if (crapTs?.enabled !== true || typeof crapTs.ceiling !== 'number') fail(`${configFile} has no enabled crap_ts.ceiling`);
  return crapTs.ceiling;
}

/** The coverage entry for `file` (absolute): Vitest writes absolute paths, which differ when the run happened in another mount. */
function coverageFor(coverage: Record<string, FileCoverage>, file: string, rel: string): FileCoverage | undefined {
  const key = Object.keys(coverage).find((k) => k === file || k.endsWith(`/${rel}`));
  return key === undefined ? undefined : coverage[key];
}

async function collect(root: string, srcDir: string, coverage: Record<string, FileCoverage>): Promise<FunctionMetrics[]> {
  const eslint = new ESLint({
    cwd: root,
    overrideConfigFile: true,
    errorOnUnmatchedPattern: false,
    overrideConfig: [
      { ignores: TEST_FILES },
      {
        files: ['**/*.ts'],
        languageOptions: { parser: tseslint.parser },
        plugins: { crap: { rules: { 'function-ranges': functionRanges } } },
        rules: { complexity: ['warn', 0], 'crap/function-ranges': 'warn' },
      },
    ],
  });
  const metrics: FunctionMetrics[] = [];
  for (const result of await eslint.lintFiles([srcDir])) {
    const rel = path.relative(root, result.filePath).split(path.sep).join('/');
    const facts: FunctionFacts[] = [];
    const complexities = new Map<string, number>();
    for (const message of result.messages) {
      if (message.fatal) fail(`${rel}: ${message.message}`);
      if (message.ruleId === 'crap/function-ranges') {
        facts.push(JSON.parse(message.message) as FunctionFacts);
      } else if (message.ruleId === 'complexity') {
        const found = /complexity of (\d+)/.exec(message.message);
        if (!found) fail(`${rel}: unrecognised complexity message "${message.message}"`);
        complexities.set(`${message.line}:${message.column - 1}`, Number(found[1]));
      }
    }
    facts.sort((a, b) => a.headLine - b.headLine || a.headColumn - b.headColumn);
    const functions: FunctionComplexity[] = facts.map((fact) => {
      const complexity = complexities.get(`${fact.headLine}:${fact.headColumn}`);
      if (complexity === undefined) fail(`${rel}:${fact.headLine}: no complexity reported for ${fact.name}`);
      return { name: fact.name, complexity, bodyLine: fact.bodyLine, bodyColumn: fact.bodyColumn };
    });
    metrics.push(...toMetrics(rel, functions, coverageFor(coverage, result.filePath, rel)));
  }
  return metrics;
}

const format = (score: number): string => (Math.round(score * 100) / 100).toString();

async function main(): Promise<void> {
  const root = path.resolve(option('root', '.'));
  const coverageFile = path.resolve(root, option('coverage', 'coverage/coverage-final.json'));
  const configFile = path.resolve(root, option('config', path.join(import.meta.dirname, '../../../ci/quality.json')));
  const ceiling = readCeiling(configFile);
  if (!existsSync(coverageFile)) fail(`${coverageFile} is missing: run \`npm run test\` first.`);
  const coverage = JSON.parse(readFileSync(coverageFile, 'utf8')) as Record<string, FileCoverage>;
  const metrics = await collect(root, option('src', 'src'), coverage);
  if (metrics.length === 0) fail('no function was scored: an empty scan measures nothing');
  const verdict = judgeCrap(metrics, ceiling);
  const byScore = [...verdict.violations].sort((a, b) => b.crap - a.crap);
  for (const fn of byScore) console.error(`CRAP ${format(fn.crap)}  ${fn.id}  (over the ceiling ${ceiling})`);
  if (!verdict.ok) {
    console.error(`${verdict.violations.length} function(s) fail the TS CRAP gate (ceiling ${ceiling}, no baseline).`);
    process.exit(1);
  }
  const worst = Math.max(...scoreFunctions(metrics).map((fn) => fn.crap));
  console.log(`TS CRAP gate passed: ${metrics.length} functions, ceiling ${ceiling}, worst ${format(worst)}.`);
}

await main();
