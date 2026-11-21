import { describe, expect, it } from 'vitest';
import {
  DEFAULT_ATTENTION_FILTER,
  SECTIONS,
  ageText,
  apiQuery,
  askerWaitingLabel,
  nowHash,
  emptyText,
  filterFromHash,
  filterFromQuery,
  filterToQuery,
  isFiltered,
  sectionCount,
  type AttentionFilter,
  type TaskAttention,
  attentionBadges,
  orderNeedsAttention,
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
    expect(nowHash(DEFAULT_ATTENTION_FILTER)).toBe('#/now');
    expect(isFiltered(DEFAULT_ATTENTION_FILTER)).toBe(false);
    expect(apiQuery(DEFAULT_ATTENTION_FILTER).toString()).toBe('');
  });
  it('is what an empty query and a bare fragment read as', () => {
    expect(filterFromQuery('')).toEqual(DEFAULT_ATTENTION_FILTER);
    expect(filterFromHash('#/now')).toEqual(DEFAULT_ATTENTION_FILTER);
    expect(filterFromHash('#/now?')).toEqual(DEFAULT_ATTENTION_FILTER);
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
    expect(nowHash(filter)).toBe(`#/now?${query}`);
    expect(filterFromHash(nowHash(filter))).toEqual(filter);
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
    expect(filterFromQuery('tag=%20OWN%20&tag=Ralph&tag=own&initiative=aa')).toEqual({ initiatives: ['aa'], tags: ['own', 'ralph'] });
    expect(filterToQuery(set({ tags: ['a', '', 'a', ' b '] }))).toBe('tag=a&tag=b');
    expect(isFiltered(set({ tags: [' '] }))).toBe(false);
  });
});

describe('attention badges', () => {
  const cases: [string, TaskAttention, string, string][] = [
    ['stranded', { reason: 'stranded' }, 'stranded', 'Stranded, its lease ran out and was not acknowledged'],
    ['asking', { reason: 'asking' }, 'waiting on you', 'Waiting on you'],
    ['asking, blocking', { reason: 'asking', blocking: true }, 'waiting on you · blocking', 'Waiting on you, blocking'],
    ['asking, agent waiting', { reason: 'asking', asker_waiting: true }, 'waiting on you · agent waiting', 'Waiting on you, agent waiting'],
    [
      'asking, blocking and agent waiting',
      { reason: 'asking', blocking: true, asker_waiting: true },
      'waiting on you · blocking · agent waiting',
      'Waiting on you, blocking, agent waiting',
    ],
    ['done', { reason: 'done' }, 'done, not closed', 'Done, not closed'],
  ];
  it.each(cases)('%s has its label and accessible name', (_title, item, label, name) => {
    const [badge] = attentionBadges([item]);
    expect(badge.label).toBe(label);
    expect(badge.name).toBe(name);
    expect(badge.reason).toBe(item.reason);
  });

  it('gives every reason the warning tone and visible text', () => {
    const badges = attentionBadges([{ reason: 'stranded' }, { reason: 'asking', blocking: true }, { reason: 'done' }]);
    expect(badges.map((b) => b.tone)).toEqual(['warning', 'warning', 'warning']);
    expect(badges.every((b) => b.label.trim() !== '' && b.name.trim() !== '')).toBe(true);
  });

  it('keeps the list order, one badge per reason, none for an empty list', () => {
    expect(attentionBadges([{ reason: 'stranded' }, { reason: 'done' }]).map((b) => b.reason)).toEqual(['stranded', 'done']);
    expect(attentionBadges([])).toEqual([]);
  });

  it('does not say blocking or agent waiting when the flags are false', () => {
    expect(attentionBadges([{ reason: 'asking', blocking: false, asker_waiting: false }])[0].name).toBe('Waiting on you');
  });
});

describe('Needs attention order', () => {
  const task = (ref: string, updated_at: string, attention: TaskAttention[]) => ({ ref, updated_at, attention });
  const refs = (tasks: { ref: string }[]) => tasks.map((t) => t.ref);

  it('puts stranded, then asking with blocking first, then done, newest change first in a group', () => {
    const mixed = [
      task('d-old', '2026-01-01T00:00:00Z', [{ reason: 'done' }]),
      task('a-new', '2026-01-09T00:00:00Z', [{ reason: 'asking' }]),
      task('s-old', '2026-01-02T00:00:00Z', [{ reason: 'stranded' }]),
      task('d-new', '2026-01-08T00:00:00Z', [{ reason: 'done' }]),
      task('b-old', '2026-01-03T00:00:00Z', [{ reason: 'asking', blocking: true }]),
      task('s-new', '2026-01-07T00:00:00Z', [{ reason: 'stranded' }]),
      task('a-old', '2026-01-04T00:00:00Z', [{ reason: 'asking', asker_waiting: true }]),
      task('b-new', '2026-01-06T00:00:00Z', [{ reason: 'asking', blocking: true, asker_waiting: true }]),
    ];
    expect(refs(orderNeedsAttention(mixed))).toEqual(['s-new', 's-old', 'b-new', 'b-old', 'a-new', 'a-old', 'd-new', 'd-old']);
  });

  it('puts a task with several reasons in the group of its first one', () => {
    const tasks = [
      task('done-only', '2026-02-02T00:00:00Z', [{ reason: 'done' }]),
      task('stranded-and-done', '2026-02-01T00:00:00Z', [{ reason: 'stranded' }, { reason: 'done' }]),
      task('asking-and-done', '2026-02-03T00:00:00Z', [{ reason: 'asking' }, { reason: 'done' }]),
    ];
    expect(refs(orderNeedsAttention(tasks))).toEqual(['stranded-and-done', 'asking-and-done', 'done-only']);
  });

  it('keeps the input order for equal changes, does not change its input and puts a task with no reason last', () => {
    const tasks = [
      task('none', '2026-03-09T00:00:00Z', []),
      task('x', '2026-03-01T00:00:00Z', [{ reason: 'done' }]),
      task('y', '2026-03-01T00:00:00Z', [{ reason: 'done' }]),
    ];
    expect(refs(orderNeedsAttention(tasks))).toEqual(['x', 'y', 'none']);
    expect(refs(tasks)).toEqual(['none', 'x', 'y']);
    expect(orderNeedsAttention([])).toEqual([]);
  });
});
