import { describe, expect, it } from 'vitest';
import { countBadges, countsText, leasesText, totalTasks } from './counts';

describe('totalTasks', () => {
  it('adds the three statuses', () => {
    expect(totalTasks({ open: 2, done: 1, closed: 4 })).toBe(7);
    expect(totalTasks({ open: 0, done: 0, closed: 0 })).toBe(0);
  });
});

describe('countBadges', () => {
  it('gives one badge per status in the order open, done, closed, zeros kept', () => {
    expect(countBadges({ open: 2, done: 0, closed: 5 })).toEqual([
      { key: 'open', label: 'open', count: 2, tone: 'success' },
      { key: 'done', label: 'done', count: 0, tone: 'warning' },
      { key: 'closed', label: 'closed', count: 5, tone: 'neutral' },
    ]);
  });
});

describe('countsText', () => {
  it('lists the statuses that hold tasks', () => {
    expect(countsText({ open: 2, done: 1, closed: 3 })).toBe('2 open · 1 done · 3 closed');
    expect(countsText({ open: 0, done: 1, closed: 0 })).toBe('1 done');
  });
  it('says no tasks when there are none', () => {
    expect(countsText({ open: 0, done: 0, closed: 0 })).toBe('No tasks');
  });
});

describe('leasesText', () => {
  it('phrases zero, one and several', () => {
    expect(leasesText(0)).toBe('no live leases');
    expect(leasesText(1)).toBe('1 live lease');
    expect(leasesText(3)).toBe('3 live leases');
  });
});
