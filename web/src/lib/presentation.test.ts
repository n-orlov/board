import { describe, expect, it } from 'vitest';
import { initiativeTone, keyTone, tagLabels, toneClasses, type Tone } from './presentation';

describe('status tones', () => {
  it('shows an open initiative as success', () => expect(initiativeTone('open')).toBe('success'));
  it('shows a closed initiative as neutral', () => expect(initiativeTone('closed')).toBe('neutral'));
  it('shows an active key as success', () => expect(keyTone('active')).toBe('success'));
  it('shows a revoked key as danger', () => expect(keyTone('revoked')).toBe('danger'));
  it('shows an expired key as warning', () => expect(keyTone('expired')).toBe('warning'));
  it('shows a status it does not know as neutral', () => {
    expect(initiativeTone('archived')).toBe('neutral');
    expect(keyTone('open')).toBe('neutral');
    expect(initiativeTone('active')).toBe('neutral');
    expect(keyTone('toString')).toBe('neutral');
  });
});

describe('toneClasses', () => {
  it.each<[Tone, string]>([
    ['success', 'border-success text-success'],
    ['neutral', 'border-border text-text-muted'],
    ['warning', 'border-warning text-warning'],
    ['danger', 'border-danger text-danger'],
  ])('maps %s to token utilities', (tone, want) => {
    expect(toneClasses(tone)).toBe(want);
  });
});

describe('tagLabels', () => {
  it('keeps order, trims and drops blank tags', () => {
    expect(tagLabels(['b', ' a ', '', '   ', 'c'])).toEqual(['b', 'a', 'c']);
  });
  it('gives no chips for no tags', () => {
    expect(tagLabels([])).toEqual([]);
  });
});
