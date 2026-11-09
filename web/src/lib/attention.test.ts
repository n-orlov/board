import { describe, expect, it } from 'vitest';
import {
  DEFAULT_ATTENTION_FILTER,
  SECTIONS,
  ageText,
  apiQuery,
  askerWaitingLabel,
  attentionHash,
  emptyText,
  filterFromHash,
  filterFromQuery,
  filterToQuery,
  isFiltered,
  sectionCount,
  toggled,
  type AttentionFilter,
} from './attention';

describe('ask ages', () => {
  const cases: [number, string][] = [
    [0, 'just now'],
    [59, 'just now'],
    [60, '1 minute ago'],
    [119, '1 minute ago'],
    [120, '2 minutes ago'],
    [3599, '59 minutes ago'],
    [3600, '1 hour ago'],
    [7199, '1 hour ago'],
    [7200, '2 hours ago'],
    [86399, '23 hours ago'],
    [86400, '1 day ago'],
    [86400 * 3 + 5, '3 days ago'],
  ];
  it.each(cases)('%i seconds is %s', (seconds, text) => {
    expect(ageText(seconds)).toBe(text);
  });
});

describe('the asker-waiting label', () => {
  it('says the asker is waiting when it is', () => {
    expect(askerWaitingLabel(true)).toBe('asker waiting');
  });
  it('says it is not when it is not', () => {
    expect(askerWaitingLabel(false)).toBe('asker not waiting');
  });
});

describe('the sections', () => {
  it('are five, in SPEC 5.3 order', () => {
    expect(SECTIONS.map((s) => s.key)).toEqual(['open_asks', 'done_not_closed', 'expired_leases', 'overdue_watches', 'conflicts']);
    expect(SECTIONS.map((s) => s.title)).toEqual(['Open asks', 'Done, not closed', 'Expired leases', 'Overdue watches', 'Conflicts']);
  });
  it('have their own empty-state text', () => {
    expect(emptyText('open_asks')).toBe('No open asks. Nobody is waiting on an answer.');
    expect(emptyText('done_not_closed')).toBe('Nothing is waiting to be closed.');
    expect(emptyText('expired_leases')).toBe('No expired leases to acknowledge.');
    expect(emptyText('overdue_watches')).toBe('No watch is overdue.');
    expect(emptyText('conflicts')).toBe('No conflicts. Every resource has one holder at most.');
    expect(new Set(SECTIONS.map((s) => s.empty)).size).toBe(5);
  });
  it('read their count from the counts', () => {
    const counts = { open_asks: 1, done_not_closed: 2, expired_leases: 3, overdue_watches: 4, conflicts: 5, total: 15 };
    expect(SECTIONS.map((s) => sectionCount(counts, s.key))).toEqual([1, 2, 3, 4, 5]);
  });
});

const set = (patch: Partial<AttentionFilter>): AttentionFilter => ({ ...DEFAULT_ATTENTION_FILTER, ...patch });

describe('the default filter', () => {
  it('has an empty query and the bare fragment', () => {
    expect(filterToQuery(DEFAULT_ATTENTION_FILTER)).toBe('');
    expect(attentionHash(DEFAULT_ATTENTION_FILTER)).toBe('#/attention');
    expect(isFiltered(DEFAULT_ATTENTION_FILTER)).toBe(false);
    expect(apiQuery(DEFAULT_ATTENTION_FILTER).toString()).toBe('');
  });
  it('is what an empty query and a bare fragment read as', () => {
    expect(filterFromQuery('')).toEqual(DEFAULT_ATTENTION_FILTER);
    expect(filterFromHash('#/attention')).toEqual(DEFAULT_ATTENTION_FILTER);
    expect(filterFromHash('#/attention?')).toEqual(DEFAULT_ATTENTION_FILTER);
  });
});

describe('the filter round trip', () => {
  const cases: [string, Partial<AttentionFilter>, string][] = [
    ['one tag', { tags: ['work'] }, 'tag=work'],
    ['repeated tags', { tags: ['work', 'home', 'lab'] }, 'tag=work&tag=home&tag=lab'],
    ['one initiative', { initiatives: ['aa'] }, 'initiative=aa'],
    ['repeated initiatives', { initiatives: ['aa', 'bb'] }, 'initiative=aa&initiative=bb'],
    ['both', { tags: ['work', 'home'], initiatives: ['aa'] }, 'tag=work&tag=home&initiative=aa'],
    ['awkward characters', { tags: ['a&b=c', 'x y', '#1'] }, 'tag=a%26b%3Dc&tag=x+y&tag=%231'],
  ];
  it.each(cases)('%s', (_name, patch, query) => {
    const filter = set(patch);
    expect(filterToQuery(filter)).toBe(query);
    expect(isFiltered(filter)).toBe(true);
    expect(filterFromQuery(query)).toEqual(filter);
    expect(attentionHash(filter)).toBe(`#/attention?${query}`);
    expect(filterFromHash(attentionHash(filter))).toEqual(filter);
    expect(apiQuery(filter).toString()).toBe(query);
  });
  it('sends repeated tags as repeated API keys', () => {
    expect(apiQuery(set({ tags: ['a', 'b'], initiatives: ['i'] })).getAll('tag')).toEqual(['a', 'b']);
    expect(apiQuery(set({ tags: ['a', 'b'], initiatives: ['i'] })).getAll('initiative')).toEqual(['i']);
  });
  it('reads keys in any order and ignores unknown ones', () => {
    expect(filterFromQuery('initiative=aa&x=1&tag=b&tag=a')).toEqual({ initiatives: ['aa'], tags: ['b', 'a'] });
  });
  it('drops blank and repeated values', () => {
    expect(filterFromQuery('tag=&tag=%20&tag=a&tag=a&initiative=')).toEqual({ initiatives: [], tags: ['a'] });
    expect(filterToQuery(set({ tags: ['a', '', 'a', ' b '] }))).toBe('tag=a&tag=b');
    expect(isFiltered(set({ tags: [' '] }))).toBe(false);
  });
});

describe('toggling a value', () => {
  it('adds an absent value at the end and removes a present one', () => {
    expect(toggled(['a'], 'b')).toEqual(['a', 'b']);
    expect(toggled(['a', 'b'], 'a')).toEqual(['b']);
    expect(toggled([], 'a')).toEqual(['a']);
  });
});
