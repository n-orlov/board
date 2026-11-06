import { describe, expect, it } from 'vitest';
import { absoluteTime, relativeTime } from './format';

const NOW = new Date(2026, 10, 6, 12, 0, 0);
const S = 1000;
const MIN = 60 * S;
const H = 60 * MIN;
const D = 24 * H;
const ago = (ms: number): string => new Date(NOW.getTime() - ms).toISOString();

describe('relativeTime cases', () => {
  it('says just now for a few seconds', () => {
    expect(relativeTime(ago(5 * S), NOW)).toBe('just now');
    expect(relativeTime(ago(0), NOW)).toBe('just now');
  });
  it('says just now for an instant in the future', () => {
    expect(relativeTime(ago(-10 * MIN), NOW)).toBe('just now');
  });
  it('counts minutes', () => {
    expect(relativeTime(ago(MIN), NOW)).toBe('1 minute ago');
    expect(relativeTime(ago(5 * MIN + 30 * S), NOW)).toBe('5 minutes ago');
  });
  it('counts hours', () => {
    expect(relativeTime(ago(H), NOW)).toBe('1 hour ago');
    expect(relativeTime(ago(3 * H + 59 * MIN), NOW)).toBe('3 hours ago');
  });
  it('says yesterday between 24 and 48 hours', () => {
    expect(relativeTime(ago(30 * H), NOW)).toBe('yesterday');
  });
  it('counts days', () => {
    expect(relativeTime(ago(2 * D), NOW)).toBe('2 days ago');
    expect(relativeTime(ago(4 * D + 20 * H), NOW)).toBe('4 days ago');
  });
  it('shows the local date when older than 7 days', () => {
    expect(relativeTime(new Date(2026, 9, 1, 8, 5).toISOString(), NOW)).toBe('2026-10-01');
    expect(relativeTime(new Date(2025, 0, 9, 23, 59).toISOString(), NOW)).toBe('2025-01-09');
  });
  it('gives nothing for an unparseable timestamp', () => {
    expect(relativeTime('not a time', NOW)).toBe('');
  });
});

describe('relativeTime boundaries', () => {
  it('just now | minutes at 60 seconds', () => {
    expect(relativeTime(ago(MIN - 1), NOW)).toBe('just now');
    expect(relativeTime(ago(MIN), NOW)).toBe('1 minute ago');
  });
  it('minutes | hours at 60 minutes', () => {
    expect(relativeTime(ago(H - 1), NOW)).toBe('59 minutes ago');
    expect(relativeTime(ago(H), NOW)).toBe('1 hour ago');
  });
  it('hours | yesterday at 24 hours', () => {
    expect(relativeTime(ago(D - 1), NOW)).toBe('23 hours ago');
    expect(relativeTime(ago(D), NOW)).toBe('yesterday');
  });
  it('yesterday | days at 48 hours', () => {
    expect(relativeTime(ago(2 * D - 1), NOW)).toBe('yesterday');
    expect(relativeTime(ago(2 * D), NOW)).toBe('2 days ago');
  });
  it('days | date after 7 days', () => {
    expect(relativeTime(ago(7 * D), NOW)).toBe('7 days ago');
    expect(relativeTime(ago(7 * D + 1), NOW)).toBe('2026-10-30');
  });
});

describe('absoluteTime', () => {
  it('is the local date and time', () => {
    expect(absoluteTime(new Date(2026, 0, 2, 3, 4).toISOString())).toBe('2026-01-02 03:04');
    expect(absoluteTime(new Date(2026, 10, 12, 23, 59).toISOString())).toBe('2026-11-12 23:59');
  });
  it('gives nothing for an unparseable timestamp', () => {
    expect(absoluteTime('')).toBe('');
  });
});
