import { describe, expect, it } from 'vitest';
import {
  DEFAULT_TASKS_FILTER,
  apiQuery,
  filterFromHash,
  filterFromQuery,
  filterToQuery,
  flagOptions,
  initiativeOptions,
  isFiltered,
  tasksHash,
  type TasksFilter,
} from './taskFilters';

const set = (patch: Partial<TasksFilter>): TasksFilter => ({ ...DEFAULT_TASKS_FILTER, ...patch });

describe('the default filter', () => {
  it('has an empty query and the bare fragment', () => {
    expect(filterToQuery(DEFAULT_TASKS_FILTER)).toBe('');
    expect(tasksHash(DEFAULT_TASKS_FILTER)).toBe('#/tasks');
    expect(isFiltered(DEFAULT_TASKS_FILTER)).toBe(false);
  });
  it('is what an empty query and a bare fragment read as', () => {
    expect(filterFromQuery('')).toEqual(DEFAULT_TASKS_FILTER);
    expect(filterFromHash('#/tasks')).toEqual(DEFAULT_TASKS_FILTER);
  });
});

describe('the URL fragment round trip, one field at a time', () => {
  const cases: [string, Partial<TasksFilter>, string][] = [
    ['status', { status: 'done' }, 'status=done'],
    ['status open', { status: 'open' }, 'status=open'],
    ['status all', { status: 'all' }, 'status=all'],
    ['kind', { kind: 'watch' }, 'kind=watch'],
    ['initiative', { initiative: 'aa' }, 'initiative=aa'],
    ['active yes', { active: 'yes' }, 'active=true'],
    ['active no', { active: 'no' }, 'active=false'],
    ['blocked yes', { blocked: 'yes' }, 'blocked=true'],
    ['blocked no', { blocked: 'no' }, 'blocked=false'],
    ['overdue yes', { overdue: 'yes' }, 'overdue=true'],
    ['overdue no', { overdue: 'no' }, 'overdue=false'],
    ['text', { text: 'fix it' }, 'q=fix+it'],
  ];
  it.each(cases)('%s', (_name, patch, query) => {
    const filter = set(patch);
    expect(filterToQuery(filter)).toBe(query);
    expect(isFiltered(filter)).toBe(true);
    expect(filterFromQuery(query)).toEqual(filter);
    expect(tasksHash(filter)).toBe(`#/tasks?${query}`);
    expect(filterFromHash(tasksHash(filter))).toEqual(filter);
  });
});

describe('the URL fragment round trip', () => {
  it('keeps every field together, in a fixed order', () => {
    const filter: TasksFilter = { status: 'closed', kind: 'work', initiative: 'bb', active: 'no', blocked: 'yes', overdue: 'no', text: 'a&b=c #1' };
    const query = filterToQuery(filter);
    expect(query).toBe('status=closed&kind=work&initiative=bb&active=false&blocked=true&overdue=false&q=a%26b%3Dc+%231');
    expect(filterFromHash(`#/tasks?${query}`)).toEqual(filter);
  });
  it('keeps non-ASCII text', () => {
    const filter = set({ text: 'café ✓' });
    expect(filterFromHash(tasksHash(filter))).toEqual(filter);
  });
  it('omits the default status but keeps the other fields', () => {
    expect(filterToQuery(set({ status: 'not-closed', kind: 'work' }))).toBe('kind=work');
  });
  it('reads a query after the first question mark only', () => {
    expect(filterFromHash('#/tasks?q=what?&kind=watch')).toEqual(set({ text: 'what?', kind: 'watch' }));
  });
  it('falls back to the default for unknown values', () => {
    expect(filterFromQuery('status=nope&kind=big&active=maybe&blocked=1&overdue=')).toEqual(DEFAULT_TASKS_FILTER);
  });
});

describe('the API query', () => {
  it('always sends a status and spells the default not_closed', () => {
    expect(apiQuery(DEFAULT_TASKS_FILTER).toString()).toBe('status=not_closed');
  });
  it('sends the fields that are set', () => {
    const filter: TasksFilter = { status: 'all', kind: 'watch', initiative: 'aa', active: 'yes', blocked: 'no', overdue: 'yes', text: 'x y' };
    expect(apiQuery(filter).toString()).toBe('status=all&kind=watch&initiative=aa&active=true&blocked=false&overdue=true&q=x+y');
  });
  it('sends an explicit open status as it is', () => {
    expect(apiQuery(set({ status: 'open' })).get('status')).toBe('open');
  });
});

describe('flagOptions', () => {
  it('names the three choices after the flag', () => {
    expect(flagOptions('blocked')).toEqual([
      { value: '', label: 'Any blocked' },
      { value: 'yes', label: 'Blocked' },
      { value: 'no', label: 'Not blocked' },
    ]);
  });
});

describe('initiativeOptions', () => {
  it('offers any initiative first, then the slugs sorted', () => {
    expect(initiativeOptions(['bb', 'aa'], '')).toEqual([
      { value: '', label: 'Any initiative' },
      { value: 'aa', label: 'aa' },
      { value: 'bb', label: 'bb' },
    ]);
  });

  it('offers the initiative the URL names once, even when the list lacks it', () => {
    expect(initiativeOptions(['aa'], 'zz').map((o) => o.value)).toEqual(['', 'aa', 'zz']);
    expect(initiativeOptions(['aa'], 'aa').map((o) => o.value)).toEqual(['', 'aa']);
    expect(initiativeOptions([], '').map((o) => o.value)).toEqual(['']);
  });
});
