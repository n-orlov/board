import { describe, expect, it } from 'vitest';
import { cadenceText, durationText, expiresIn, nextCheckText } from './durations';

const NOW = new Date('2026-11-06T12:00:00Z');
const from = (seconds: number): string => new Date(NOW.getTime() + seconds * 1000).toISOString();

describe('durationText', () => {
  it('uses the largest unit that divides exactly', () => {
    expect(durationText(60)).toBe('1 minute');
    expect(durationText(300)).toBe('5 minutes');
    expect(durationText(5400)).toBe('90 minutes');
    expect(durationText(3600)).toBe('1 hour');
    expect(durationText(7200)).toBe('2 hours');
    expect(durationText(86400)).toBe('1 day');
    expect(durationText(172800)).toBe('2 days');
    expect(durationText(90)).toBe('90 seconds');
    expect(durationText(1)).toBe('1 second');
  });
  it('does not round a length that does not divide', () => {
    expect(durationText(59)).toBe('59 seconds');
    expect(durationText(3601)).toBe('3601 seconds');
    expect(durationText(86460)).toBe('1441 minutes');
  });
});

describe('cadenceText', () => {
  it('phrases the cadence of a watch task', () => {
    expect(cadenceText(3600)).toBe('every 1 hour');
    expect(cadenceText(900)).toBe('every 15 minutes');
  });
  it('is empty without a cadence', () => {
    expect(cadenceText(undefined)).toBe('');
  });
});

describe('expiresIn', () => {
  it('counts what is left in the future', () => {
    expect(expiresIn(from(41 * 60), NOW)).toBe('expires in 41m');
    expect(expiresIn(from(3 * 3600 + 10 * 60), NOW)).toBe('expires in 3h 10m');
    expect(expiresIn(from(2 * 3600), NOW)).toBe('expires in 2h');
    expect(expiresIn(from(60), NOW)).toBe('expires in 1m');
  });
  it('says under a minute below 60 seconds, 0 seconds included', () => {
    expect(expiresIn(from(59), NOW)).toBe('expires in under a minute');
    expect(expiresIn(from(1), NOW)).toBe('expires in under a minute');
    expect(expiresIn(from(0), NOW)).toBe('expires in under a minute');
  });
  it('says expired and how long ago for a past instant', () => {
    expect(expiresIn(from(-1), NOW)).toBe('expired 1s ago');
    expect(expiresIn(from(-20), NOW)).toBe('expired 20s ago');
    expect(expiresIn(from(-120), NOW)).toBe('expired 2m ago');
    expect(expiresIn(from(-3 * 3600), NOW)).toBe('expired 3h ago');
    expect(expiresIn(from(-(3600 + 5 * 60)), NOW)).toBe('expired 1h 5m ago');
  });
  it('gives nothing for an unparseable instant', () => {
    expect(expiresIn('soon', NOW)).toBe('');
  });
});

describe('nextCheckText', () => {
  it('counts the time until the next check', () => {
    expect(nextCheckText(from(14 * 60), NOW)).toBe('due in 14m');
    expect(nextCheckText(from(2 * 3600), NOW)).toBe('due in 2h');
    expect(nextCheckText(from(60), NOW)).toBe('due in 1m');
  });
  it('says under a minute below 60 seconds, 0 seconds included', () => {
    expect(nextCheckText(from(59), NOW)).toBe('due in under a minute');
    expect(nextCheckText(from(0), NOW)).toBe('due in under a minute');
  });
  it('says overdue and by how much for a check that is late', () => {
    expect(nextCheckText(from(-1), NOW)).toBe('overdue by 1s');
    expect(nextCheckText(from(-(3600 + 5 * 60)), NOW)).toBe('overdue by 1h 5m');
  });
  it('gives nothing for an unparseable instant', () => {
    expect(nextCheckText('soon', NOW)).toBe('');
  });
});
