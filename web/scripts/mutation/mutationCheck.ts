// Pure per-module mutation check: Stryker's `thresholds.break` only judges the whole run, so every mutated module is scored on its own.
export interface MutantLike {
  status: string;
}
export interface MutationReportLike {
  files: Record<string, { mutants: MutantLike[] }>;
}
export interface ModuleScore {
  module: string;
  /** Mutation score in percent. */
  score: number;
  detected: number;
  undetected: number;
}

const DETECTED = new Set(['Killed', 'Timeout']);
const UNDETECTED = new Set(['Survived', 'NoCoverage']);

const normalise = (file: string): string => file.replaceAll('\\', '/').replace(/^\.\//, '');

/** Scores one file the way Stryker does: (Killed + Timeout) / (Killed + Timeout + Survived + NoCoverage), as a percentage; null with nothing to score. */
export function scoreMutants(mutants: readonly MutantLike[]): ModuleScore | null {
  let detected = 0;
  let undetected = 0;
  for (const mutant of mutants) {
    if (DETECTED.has(mutant.status)) detected += 1;
    else if (UNDETECTED.has(mutant.status)) undetected += 1;
  }
  const total = detected + undetected;
  return total === 0 ? null : { module: '', score: (detected / total) * 100, detected, undetected };
}

export interface ModuleCheck {
  scores: ModuleScore[];
  /** One human-readable line per module that scores below the break, and one when the report holds nothing to score at all. */
  failures: string[];
}

/** Judges every module of the report under `path` against `breakThreshold`. A module with no scoreable mutant is skipped; a report with none at all fails. */
export function checkModules(report: MutationReportLike, path: string, breakThreshold: number): ModuleCheck {
  const prefix = normalise(path).replace(/\/?$/, '/');
  const scores: ModuleScore[] = [];
  for (const [file, entry] of Object.entries(report.files)) {
    const module = normalise(file);
    const score = module.startsWith(prefix) ? scoreMutants(entry.mutants) : null;
    if (score !== null) scores.push({ ...score, module });
  }
  scores.sort((a, b) => a.module.localeCompare(b.module));
  const failures = scores
    .filter((entry) => entry.score < breakThreshold)
    .map((entry) => `${entry.module}: ${entry.score.toFixed(2)}% is below the break of ${breakThreshold}%`);
  if (scores.length === 0) failures.push(`no scoreable mutant under ${path} in the report`);
  return { scores, failures };
}
