import { describe, expect, it } from 'vitest';
import { DEFAULT_TASKS_FILTER } from './taskFilters';
import { sortRows } from './tableSort';
import type { TaskRow } from './initiativePage';
import { openView, saveViewQuery, viewKey, type ViewStorage } from './viewState';
import {
  TASKS_VIEW,
  TASK_COLUMNS,
  TASK_SORT_COLUMNS,
  TASK_SORT_LABELS,
  dropUnknownInitiative,
  viewFromHash,
  viewFromQuery,
  viewHash,
  viewToQuery,
} from './tasksView';

function row(over: Partial<TaskRow>): TaskRow {
  return { id: over.ref ?? 'x', ref: 'a-1', initiative: 'a', title: 't', kind: 'work', status: 'open', badges: [], attention: [], holder: '', ...over };
}

function memory(initial: Record<string, string> = {}): ViewStorage & { data: Map<string, string> } {
  const data = new Map(Object.entries(initial));
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
    removeItem: (k) => void data.delete(k),
    key: (i) => [...data.keys()][i] ?? null,
    get length() {
      return data.size;
    },
  };
}

describe('sort <-> query encoding', () => {
  it('writes the filters first and the sort after them', () => {
    const state = { filter: { ...DEFAULT_TASKS_FILTER, status: 'done' as const, kind: 'watch' as const }, sort: { column: 'ref', dir: 'desc' as const } };
    expect(viewToQuery(state)).toBe('status=done&kind=watch&sort=ref&dir=desc');
    expect(viewHash(state)).toBe('#/tasks?status=done&kind=watch&sort=ref&dir=desc');
  });

  it('writes only the sort when the filters are the defaults, and nothing for the defaults', () => {
    expect(viewToQuery({ filter: DEFAULT_TASKS_FILTER, sort: { column: 'status', dir: 'asc' } })).toBe('sort=status&dir=asc');
    expect(viewToQuery({ filter: DEFAULT_TASKS_FILTER, sort: null })).toBe('');
    expect(viewHash({ filter: DEFAULT_TASKS_FILTER, sort: null })).toBe('#/tasks');
  });

  it('reads the filters and the sort back from a query', () => {
    const state = viewFromQuery('kind=work&sort=holder&dir=desc&q=x');
    expect(state.filter).toEqual({ ...DEFAULT_TASKS_FILTER, kind: 'work', text: 'x' });
    expect(state.sort).toEqual({ column: 'holder', dir: 'desc' });
  });

  it('round-trips every sortable column in both directions', () => {
    for (const column of TASK_SORT_COLUMNS) {
      for (const dir of ['asc', 'desc'] as const) {
        const state = { filter: { ...DEFAULT_TASKS_FILTER, initiative: 'board' }, sort: { column, dir } };
        expect(viewFromHash(viewHash(state))).toEqual(state);
      }
    }
  });

  it('takes an unknown sort column, flags and a bad direction as the default order', () => {
    expect(viewFromQuery('sort=nope&dir=asc').sort).toBeNull();
    expect(viewFromQuery('sort=badges&dir=asc').sort).toBeNull();
    expect(viewFromQuery('sort=ref&dir=sideways').sort).toBeNull();
    expect(viewFromQuery('sort=ref').sort).toEqual({ column: 'ref', dir: 'asc' });
    expect(viewFromQuery('').sort).toBeNull();
  });

  it('reads a fragment with no query as the defaults', () => {
    expect(viewFromHash('#/tasks')).toEqual({ filter: DEFAULT_TASKS_FILTER, sort: null });
    expect(viewFromHash('#/tasks?sort=ref&dir=desc').sort).toEqual({ column: 'ref', dir: 'desc' });
  });

  it('names the columns of the table and a label for each', () => {
    expect(TASK_SORT_COLUMNS).toEqual(['ref', 'title', 'initiative', 'kind', 'status', 'flags', 'holder']);
    expect(TASK_SORT_LABELS.map((l) => l.column)).toEqual(TASK_SORT_COLUMNS);
    expect(TASK_SORT_LABELS.map((l) => l.label)).toEqual(['Ref', 'Title', 'Initiative', 'Kind', 'Status', 'Flags', 'Lease holder']);
  });
});

describe('the columns', () => {
  it('give each column its type', () => {
    expect(Object.fromEntries(Object.entries(TASK_COLUMNS).map(([name, c]) => [name, c.type]))).toEqual({
      ref: 'ref',
      title: 'text',
      initiative: 'text',
      kind: 'kind',
      status: 'status',
      flags: 'text',
      holder: 'text',
    });
  });
  const rows = [
    row({ ref: 'b-10', title: 'zeta', initiative: 'b', kind: 'watch', status: 'done', holder: 'ralph' }),
    row({ ref: 'b-9', title: 'alpha', initiative: 'a', kind: 'work', status: 'open', holder: '' }),
    row({ ref: 'a-2', title: 'Mid', initiative: 'c', kind: 'work', status: 'closed', holder: 'nik' }),
  ];
  const refs = (column: string, dir: 'asc' | 'desc') => sortRows(rows, { column, dir }, TASK_COLUMNS).map((r) => r.ref);

  it('sorts a ref by its number', () => {
    expect(refs('ref', 'asc')).toEqual(['a-2', 'b-9', 'b-10']);
    expect(refs('ref', 'desc')).toEqual(['b-10', 'b-9', 'a-2']);
  });
  it('sorts the title and the initiative as text', () => {
    expect(refs('title', 'asc')).toEqual(['b-9', 'a-2', 'b-10']);
    expect(refs('initiative', 'asc')).toEqual(['b-9', 'b-10', 'a-2']);
  });
  it('sorts the kind work before watch and the status in lifecycle order', () => {
    expect(refs('kind', 'asc').at(-1)).toBe('b-10');
    expect(refs('status', 'asc')).toEqual(['b-9', 'b-10', 'a-2']);
  });
  it('sorts the lease holder as text with the empty ones last either way', () => {
    expect(refs('holder', 'asc')).toEqual(['a-2', 'b-10', 'b-9']);
    expect(refs('holder', 'desc')).toEqual(['b-10', 'a-2', 'b-9']);
  });
  it('sorts Flags by the badge labels with the unflagged rows last either way, ties in input order', () => {
    const flagged = [
      row({ ref: 'a-1', badges: [] }),
      row({ ref: 'a-2', badges: [{ key: 'overdue', label: 'overdue', tone: 'danger' }] }),
      row({ ref: 'a-3', badges: [{ key: 'active', label: 'active', tone: 'success' }, { key: 'blocked', label: 'blocked', tone: 'warning' }] }),
      row({ ref: 'a-4', badges: [{ key: 'active', label: 'active', tone: 'success' }] }),
      row({ ref: 'a-5', badges: [] }),
    ];
    const order = (dir: 'asc' | 'desc') => sortRows(flagged, { column: 'flags', dir }, TASK_COLUMNS).map((r) => r.ref);
    expect(order('asc')).toEqual(['a-4', 'a-3', 'a-2', 'a-1', 'a-5']);
    expect(order('desc')).toEqual(['a-2', 'a-3', 'a-4', 'a-1', 'a-5']);
  });
  it('round-trips the flags column through the query', () => {
    expect(viewToQuery({ filter: DEFAULT_TASKS_FILTER, sort: { column: 'flags', dir: 'desc' } })).toBe('sort=flags&dir=desc');
    expect(viewFromQuery('sort=flags&dir=desc').sort).toEqual({ column: 'flags', dir: 'desc' });
  });
  it('sorts a row with every flag by its whole label list, and rows with the same flags keep their input order in both directions', () => {
    const flag = (key: 'active' | 'blocked' | 'overdue') => ({ key, label: key, tone: 'warning' as const });
    const flagged = [
      row({ ref: 'a-1', badges: [flag('blocked'), flag('overdue')] }),
      row({ ref: 'a-2', badges: [flag('active'), flag('blocked'), flag('overdue')] }),
      row({ ref: 'a-3', badges: [flag('blocked'), flag('overdue')] }),
      row({ ref: 'a-4', badges: [] }),
      row({ ref: 'a-5', badges: [flag('blocked')] }),
    ];
    const order = (dir: 'asc' | 'desc') => sortRows(flagged, { column: 'flags', dir }, TASK_COLUMNS).map((r) => r.ref);
    expect(order('asc')).toEqual(['a-2', 'a-5', 'a-1', 'a-3', 'a-4']);
    expect(order('desc')).toEqual(['a-1', 'a-3', 'a-5', 'a-2', 'a-4']);
  });
  it('leaves every row unflagged and in input order when no row has a flag', () => {
    const plain = [row({ ref: 'a-3' }), row({ ref: 'a-1' }), row({ ref: 'a-2' })];
    expect(sortRows(plain, { column: 'flags', dir: 'asc' }, TASK_COLUMNS).map((r) => r.ref)).toEqual(['a-3', 'a-1', 'a-2']);
    expect(sortRows(plain, { column: 'flags', dir: 'desc' }, TASK_COLUMNS).map((r) => r.ref)).toEqual(['a-3', 'a-1', 'a-2']);
  });
  it('gives every column of the table a phone Sort choice, a place in the query and the saved state', () => {
    for (const column of Object.keys(TASK_COLUMNS)) {
      expect(TASK_SORT_LABELS.map((l) => l.column)).toContain(column);
      expect(viewToQuery({ filter: DEFAULT_TASKS_FILTER, sort: { column, dir: 'asc' } })).toBe(`sort=${column}&dir=asc`);
      const storage = memory();
      saveViewQuery(storage, TASKS_VIEW, `sort=${column}&dir=desc`);
      const replaced: string[] = [];
      openView({ storage, replaceHash: (h) => replaced.push(h) }, TASKS_VIEW, '#/tasks', '');
      expect(replaced).toEqual([`#/tasks?sort=${column}&dir=desc`]);
      expect(viewFromQuery(`sort=${column}&dir=desc`).sort).toEqual({ column, dir: 'desc' });
    }
  });
  it('keeps the input order with no sort', () => {
    expect(sortRows(rows, null, TASK_COLUMNS).map((r) => r.ref)).toEqual(['b-10', 'b-9', 'a-2']);
  });
});

describe('dropping a restored initiative the board no longer has', () => {
  const state = viewFromQuery('initiative=gone&kind=work&sort=ref&dir=desc');

  it('drops an unknown initiative and keeps the other filters and the sort', () => {
    const dropped = dropUnknownInitiative(state, ['board', 'other']);
    expect(dropped.filter.initiative).toBe('');
    expect(dropped.filter.kind).toBe('work');
    expect(dropped.sort).toEqual({ column: 'ref', dir: 'desc' });
    expect(viewToQuery(dropped)).toBe('kind=work&sort=ref&dir=desc');
  });
  it('keeps an initiative the board has', () => {
    expect(dropUnknownInitiative(state, ['gone'])).toBe(state);
  });
  it('drops the initiative when the board lists none (an empty answer is an answer)', () => {
    expect(dropUnknownInitiative(state, []).filter.initiative).toBe('');
  });
  it('keeps a state with no initiative as it is', () => {
    const none = viewFromQuery('sort=ref');
    expect(dropUnknownInitiative(none, [])).toBe(none);
  });

  it('drops the unknown initiative of a saved query restored by the shared helper, and unknown enum values with it', () => {
    const storage = memory();
    saveViewQuery(storage, TASKS_VIEW, 'status=bogus&kind=nope&initiative=gone&sort=title&dir=asc');
    expect(viewKey(TASKS_VIEW)).toBe('board.view.v1.tasks');
    expect(storage.data.has('board.view.v1.tasks')).toBe(true);
    const hashes: string[] = [];
    const query = openView({ storage, replaceHash: (h) => hashes.push(h) }, TASKS_VIEW, '#/tasks', '');
    const restored = dropUnknownInitiative(viewFromQuery(query), ['board']);
    expect(restored.filter).toEqual(DEFAULT_TASKS_FILTER);
    expect(restored.sort).toEqual({ column: 'title', dir: 'asc' });
    expect(hashes).toEqual(['#/tasks?status=bogus&kind=nope&initiative=gone&sort=title&dir=asc']);
  });
});
