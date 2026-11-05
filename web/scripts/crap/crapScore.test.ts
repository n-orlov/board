import { describe, expect, it } from 'vitest';
import { crapScore, judgeCrap } from './crapScore.ts';

describe('crapScore', () => {
  it('is c^2 * (1 - cov)^3 + c', () => {
    expect(crapScore(5, 0)).toBe(30);
    expect(crapScore(5, 1)).toBe(5);
    expect(crapScore(4, 0.5)).toBeCloseTo(4 + 16 / 8, 10);
  });

  it.each([-1, Number.NaN, Number.POSITIVE_INFINITY])('rejects complexity %j', (c) => {
    expect(() => crapScore(c, 0.5)).toThrow(RangeError);
  });

  it.each([-0.1, 1.1, Number.NaN])('rejects coverage %j', (cov) => {
    expect(() => crapScore(3, cov)).toThrow(RangeError);
  });
});

describe('judgeCrap', () => {
  const fn = (id: string, complexity: number, coverage: number) => ({ id, complexity, coverage });

  it('passes a function exactly on the ceiling and fails one above it', () => {
    expect(judgeCrap([fn('a', 8, 1)], 8).ok).toBe(true);
    const verdict = judgeCrap([fn('a', 8, 1), fn('b', 8, 0.99)], 8);
    expect(verdict.ok).toBe(false);
    expect(verdict.violations.map((v) => v.id)).toEqual(['b']);
  });

  it('has no baseline: an uncovered complex function always fails', () => {
    const verdict = judgeCrap([fn('big', 20, 0)], 8);
    expect(verdict.ok).toBe(false);
    expect(verdict.violations[0].crap).toBe(420);
  });
});
