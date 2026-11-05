// Pure CRAP (Change Risk Anti-Patterns) scoring. No I/O: the CLI feeds it
// per-function complexity and coverage and acts on the verdict. There is no
// baseline and no exemption list: a function above the ceiling fails.

/** Scores are compared with this slack so a score that lands exactly on the ceiling passes. */
const SCORE_EPSILON = 1e-6;

export interface FunctionMetrics {
  /** Stable identity, e.g. `src/lib/foo.ts:bar`. */
  id: string;
  /** Cyclomatic complexity as reported by ESLint's `complexity` rule. */
  complexity: number;
  /** Fraction of the function covered, 0..1. */
  coverage: number;
}

export interface ScoredFunction extends FunctionMetrics {
  crap: number;
}

export interface CrapVerdict {
  ok: boolean;
  /** Every function above the ceiling. */
  violations: ScoredFunction[];
}

/** CRAP = c^2 * (1 - cov)^3 + c */
export function crapScore(complexity: number, coverage: number): number {
  if (!Number.isFinite(complexity) || complexity < 0) {
    throw new RangeError(`complexity must be a non-negative number, got ${complexity}`);
  }
  if (!Number.isFinite(coverage) || coverage < 0 || coverage > 1) {
    throw new RangeError(`coverage must be a fraction between 0 and 1, got ${coverage}`);
  }
  return complexity ** 2 * (1 - coverage) ** 3 + complexity;
}

export function scoreFunctions(functions: readonly FunctionMetrics[]): ScoredFunction[] {
  return functions.map((fn) => ({ ...fn, crap: crapScore(fn.complexity, fn.coverage) }));
}

/** A function fails when it scores above `ceiling`. */
export function judgeCrap(functions: readonly FunctionMetrics[], ceiling: number): CrapVerdict {
  const violations = scoreFunctions(functions).filter((fn) => fn.crap > ceiling + SCORE_EPSILON);
  return { ok: violations.length === 0, violations };
}
