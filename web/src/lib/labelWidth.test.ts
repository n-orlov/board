import { describe, expect, it } from 'vitest';
import {
  LABEL_DEFAULT,
  LABEL_MIN,
  LABEL_STEP,
  LABEL_STORAGE_KEY,
  clampLabelWidth,
  labelMax,
  labelWidthForKey,
  loadLabelWidth,
  parseLabelWidth,
  saveLabelWidth,
} from './labelWidth';

describe('the label column width clamp', () => {
  it('holds a width below the minimum at the minimum', () => {
    expect(clampLabelWidth(50, 1000)).toBe(LABEL_MIN);
    expect(clampLabelWidth(LABEL_MIN - 1, 1000)).toBe(LABEL_MIN);
  });
  it('holds a width above the share of the chart at that share', () => {
    expect(clampLabelWidth(900, 1000)).toBe(600);
    expect(clampLabelWidth(601, 1000)).toBe(600);
    expect(labelMax(1000)).toBe(600);
  });
  it('keeps a width in range, rounded to whole pixels', () => {
    expect(clampLabelWidth(250, 1000)).toBe(250);
    expect(clampLabelWidth(250.4, 1000)).toBe(250);
    expect(clampLabelWidth(LABEL_MIN, 1000)).toBe(LABEL_MIN);
    expect(clampLabelWidth(600, 1000)).toBe(600);
  });
  it('never goes below the minimum on a narrow chart', () => {
    expect(labelMax(100)).toBe(LABEL_MIN);
    expect(clampLabelWidth(400, 100)).toBe(LABEL_MIN);
    expect(labelMax(Number.NaN)).toBe(LABEL_MIN);
  });
  it('gives the default, held, for a value that is no number', () => {
    expect(clampLabelWidth(Number.NaN, 1000)).toBe(LABEL_DEFAULT);
    expect(clampLabelWidth(Number.POSITIVE_INFINITY, 1000)).toBe(LABEL_DEFAULT);
    expect(clampLabelWidth(Number.NaN, 400)).toBe(240);
  });
});

describe('the stored width', () => {
  it('reads a whole-pixel value at or above the minimum', () => {
    expect(parseLabelWidth('250')).toBe(250);
    expect(parseLabelWidth(String(LABEL_MIN))).toBe(LABEL_MIN);
  });
  it('falls back to the default on a bad value', () => {
    for (const bad of [null, undefined, '', 'wide', '-300', '12.5', '119', '0', '1e3', ' 300', '300px', '123456', 'NaN']) {
      expect(parseLabelWidth(bad)).toBe(LABEL_DEFAULT);
    }
  });
  it('loads under the versioned vh. key and falls back when storage is absent or blocked', () => {
    const data: Record<string, string> = { [LABEL_STORAGE_KEY]: '222' };
    const storage = { getItem: (k: string) => data[k] ?? null, setItem: (k: string, v: string) => void (data[k] = v) };
    expect(LABEL_STORAGE_KEY).toMatch(/^vh\..*\.v\d+$/);
    expect(loadLabelWidth(storage)).toBe(222);
    saveLabelWidth(storage, 333);
    expect(data[LABEL_STORAGE_KEY]).toBe('333');
    expect(loadLabelWidth(undefined)).toBe(LABEL_DEFAULT);
    const blocked = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    expect(loadLabelWidth(blocked)).toBe(LABEL_DEFAULT);
    expect(() => saveLabelWidth(blocked, 300)).not.toThrow();
    data[LABEL_STORAGE_KEY] = 'garbage';
    expect(loadLabelWidth(storage)).toBe(LABEL_DEFAULT);
  });
});

describe('the keyboard', () => {
  it('steps by arrow keys, clamped', () => {
    expect(labelWidthForKey('ArrowRight', 300, 1000)).toBe(300 + LABEL_STEP);
    expect(labelWidthForKey('ArrowLeft', 300, 1000)).toBe(300 - LABEL_STEP);
    expect(labelWidthForKey('ArrowLeft', LABEL_MIN, 1000)).toBe(LABEL_MIN);
    expect(labelWidthForKey('ArrowRight', 600, 1000)).toBe(600);
  });
  it('Home is the minimum, End the widest, other keys do nothing', () => {
    expect(labelWidthForKey('Home', 300, 1000)).toBe(LABEL_MIN);
    expect(labelWidthForKey('End', 300, 1000)).toBe(600);
    expect(labelWidthForKey('a', 300, 1000)).toBeUndefined();
  });
});
