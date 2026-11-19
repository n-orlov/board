import { describe, expect, it } from 'vitest';
import { clampLabelWidth, LABEL_DEFAULT, LABEL_MIN, labelMax } from './labelWidth';

describe('label column width', () => {
  it('holds the width between the minimum and 60 % of the chart', () => {
    expect(clampLabelWidth(50, 1000)).toBe(LABEL_MIN);
    expect(clampLabelWidth(900, 1000)).toBe(600);
    expect(clampLabelWidth(250.4, 1000)).toBe(250);
  });
  it('never goes below the minimum on a narrow chart, and falls back on a non-number', () => {
    expect(labelMax(100)).toBe(LABEL_MIN);
    expect(clampLabelWidth(Number.NaN, 1000)).toBe(LABEL_DEFAULT);
  });
});
