import { describe, expect, it } from 'vitest';
import {
  DEFAULT_INITIATIVE_FILTER,
  DEFAULT_TASK_FILTER,
  filterTasks,
  INITIATIVE_FILTERS,
  initiativeStatusParam,
  parseInitiativeFilter,
  parseTaskFilter,
  showsInitiative,
  showsTask,
  TASK_FILTERS,
} from './initiativeFilters';

describe('initiative list filter', () => {
  it('offers open, closed and all, open first', () => {
    expect(INITIATIVE_FILTERS.map((o) => o.value)).toEqual(['open', 'closed', 'all']);
    expect(INITIATIVE_FILTERS.map((o) => o.label)).toEqual(['Open', 'Closed', 'All']);
    expect(DEFAULT_INITIATIVE_FILTER).toBe('open');
  });
  it('parses a value, falling back to open', () => {
    for (const v of ['open', 'closed', 'all'] as const) expect(parseInitiativeFilter(v)).toBe(v);
    for (const v of ['', 'done', 'ALL', 'not-closed', null, undefined]) expect(parseInitiativeFilter(v)).toBe('open');
  });
  it('asks the API for the status it names', () => {
    expect(initiativeStatusParam('open')).toBe('open');
    expect(initiativeStatusParam('closed')).toBe('closed');
    expect(initiativeStatusParam('all')).toBe('all');
  });
  it('shows open initiatives under open, closed under closed, both under all', () => {
    expect(showsInitiative('open', 'open')).toBe(true);
    expect(showsInitiative('open', 'closed')).toBe(false);
    expect(showsInitiative('closed', 'open')).toBe(false);
    expect(showsInitiative('closed', 'closed')).toBe(true);
    expect(showsInitiative('all', 'open')).toBe(true);
    expect(showsInitiative('all', 'closed')).toBe(true);
  });
});

describe('initiative page task filter', () => {
  it('offers not closed, done, closed and all, not closed first', () => {
    expect(TASK_FILTERS.map((o) => o.value)).toEqual(['not-closed', 'done', 'closed', 'all']);
    expect(TASK_FILTERS.map((o) => o.label)).toEqual(['Not closed', 'Done', 'Closed', 'All']);
    expect(DEFAULT_TASK_FILTER).toBe('not-closed');
  });
  it('parses a value, falling back to not closed', () => {
    for (const v of ['not-closed', 'done', 'closed', 'all'] as const) expect(parseTaskFilter(v)).toBe(v);
    for (const v of ['', 'open', 'Done', null, undefined]) expect(parseTaskFilter(v)).toBe('not-closed');
  });
  it('decides which statuses each filter shows', () => {
    const shown = (filter: Parameters<typeof showsTask>[0]) => ['open', 'done', 'closed'].filter((s) => showsTask(filter, s));
    expect(shown('not-closed')).toEqual(['open', 'done']);
    expect(shown('done')).toEqual(['done']);
    expect(shown('closed')).toEqual(['closed']);
    expect(shown('all')).toEqual(['open', 'done', 'closed']);
  });
  it('shows no task of an unknown status, even under all', () => {
    expect(showsTask('all', 'weird')).toBe(false);
  });
  it('keeps the tasks a filter shows, in order', () => {
    const tasks = [
      { ref: 'a-1', status: 'closed' },
      { ref: 'a-2', status: 'open' },
      { ref: 'a-3', status: 'done' },
      { ref: 'a-4', status: 'open' },
    ];
    expect(filterTasks(tasks, 'not-closed').map((t) => t.ref)).toEqual(['a-2', 'a-3', 'a-4']);
    expect(filterTasks(tasks, 'done').map((t) => t.ref)).toEqual(['a-3']);
    expect(filterTasks(tasks, 'closed').map((t) => t.ref)).toEqual(['a-1']);
    expect(filterTasks(tasks, 'all')).toEqual(tasks);
    expect(filterTasks([], 'all')).toEqual([]);
  });
});
