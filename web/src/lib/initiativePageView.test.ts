import { describe, expect, it } from 'vitest';
import type { TaskRow } from './initiativePage';
import { sortRows } from './tableSort';
import { openView, saveViewQuery, viewKey, type ViewStorage } from './viewState';
import {
  INITIATIVE_VIEW,
  PAGE_TASK_COLUMNS,
  PAGE_TASK_SORT_COLUMNS,
  PAGE_TASK_SORT_LABELS,
  isCustomised,
  isPageHash,
  pagePath,
  viewFromQuery,
  viewHash,
  viewToQuery,
} from './initiativePageView';

function row(over: Partial<TaskRow>): TaskRow {
  return { id: 'x', ref: 'a-1', initiative: 'a', title: 't', kind: 'work', status: 'open', badges: [], attention: [], holder: '', ...over };
}

function memory(): ViewStorage & { data: Map<string, string> } {
  const data = new Map<string, string>();
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
  it('writes the status first and the sort after it', () => {
    const state = { filter: 'done' as const, sort: { column: 'ref', dir: 'desc' as const } };
    expect(viewToQuery(state)).toBe('status=done&sort=ref&dir=desc');
    expect(viewHash('phone-onboard', state)).toBe('#/i/phone-onboard?status=done&sort=ref&dir=desc');
  });

  it('writes only the sort when the status is the default, and nothing for the defaults', () => {
    expect(viewToQuery({ filter: 'not-closed', sort: { column: 'kind', dir: 'asc' } })).toBe('sort=kind&dir=asc');
    expect(viewToQuery({ filter: 'all', sort: null })).toBe('status=all');
    expect(viewToQuery({ filter: 'not-closed', sort: null })).toBe('');
    expect(viewHash('a', { filter: 'not-closed', sort: null })).toBe('#/i/a');
  });

  it('reads the status and the sort back from a query', () => {
    expect(viewFromQuery('status=closed&sort=status&dir=desc')).toEqual({ filter: 'closed', sort: { column: 'status', dir: 'desc' } });
    expect(viewFromQuery('sort=title')).toEqual({ filter: 'not-closed', sort: { column: 'title', dir: 'asc' } });
  });

  it('takes the defaults for an unknown status, an unknown column and a bad direction', () => {
    expect(viewFromQuery('status=weird&sort=bogus&dir=asc')).toEqual({ filter: 'not-closed', sort: null });
    expect(viewFromQuery('sort=ref&dir=sideways')).toEqual({ filter: 'not-closed', sort: null });
    expect(viewFromQuery('')).toEqual({ filter: 'not-closed', sort: null });
  });

  it('round-trips every sortable column in both directions under every status', () => {
    for (const filter of ['not-closed', 'done', 'closed', 'all'] as const) {
      for (const column of PAGE_TASK_SORT_COLUMNS) {
        for (const dir of ['asc', 'desc'] as const) {
          const state = { filter, sort: { column, dir } };
          expect(viewFromQuery(viewToQuery(state))).toEqual(state);
        }
      }
    }
  });

  it('says whether the state is not the default', () => {
    expect(isCustomised({ filter: 'not-closed', sort: null })).toBe(false);
    expect(isCustomised({ filter: 'done', sort: null })).toBe(true);
    expect(isCustomised({ filter: 'not-closed', sort: { column: 'ref', dir: 'asc' } })).toBe(true);
  });

  it('knows its own fragment, with or without a query, and no other view or initiative', () => {
    expect(pagePath('a')).toBe('#/i/a');
    expect(isPageHash('a', '#/i/a')).toBe(true);
    expect(isPageHash('a', '#/i/a?sort=ref&dir=asc')).toBe(true);
    expect(isPageHash('a', '#/i/ab')).toBe(false);
    expect(isPageHash('a', '#/tasks?sort=ref')).toBe(false);
  });
});

describe('columns', () => {
  it('names a label for every sortable column, in the table order', () => {
    expect(PAGE_TASK_SORT_LABELS.map((l) => l.column)).toEqual(PAGE_TASK_SORT_COLUMNS);
    expect(PAGE_TASK_SORT_COLUMNS).toEqual(['ref', 'title', 'kind', 'status', 'flags', 'holder']);
    expect(PAGE_TASK_SORT_LABELS.map((l) => l.label)).toEqual(['Ref', 'Title', 'Kind', 'Status', 'Flags', 'Lease holder']);
  });

  it('sorts refs numerically, titles ignoring case, kinds and statuses in their order, empty holders last', () => {
    const rows = [
      row({ id: '1', ref: 'a-10', title: 'beta', kind: 'watch', status: 'done', holder: '' }),
      row({ id: '2', ref: 'a-9', title: 'Alpha', kind: 'work', status: 'active', holder: 'zed' }),
      row({ id: '3', ref: 'a-2', title: 'charlie', kind: 'work', status: 'open', holder: 'amy' }),
    ];
    const ids = (column: string, dir: 'asc' | 'desc') => sortRows(rows, { column, dir }, PAGE_TASK_COLUMNS).map((r) => r.id);
    expect(ids('ref', 'asc')).toEqual(['3', '2', '1']);
    expect(ids('ref', 'desc')).toEqual(['1', '2', '3']);
    expect(ids('title', 'asc')).toEqual(['2', '1', '3']);
    expect(ids('kind', 'asc')).toEqual(['2', '3', '1']);
    expect(ids('kind', 'desc')).toEqual(['1', '2', '3']);
    expect(ids('status', 'asc')).toEqual(['3', '2', '1']);
    expect(ids('holder', 'asc')).toEqual(['3', '2', '1']);
    expect(ids('holder', 'desc')).toEqual(['2', '3', '1']);
  });
});

describe('the Flags column', () => {
  it('sorts by the badge labels, the unflagged rows last in both directions', () => {
    const rows = [
      row({ id: '1', badges: [] }),
      row({ id: '2', badges: [{ key: 'blocked', label: 'blocked', tone: 'warning' }] }),
      row({ id: '3', badges: [{ key: 'active', label: 'active', tone: 'success' }] }),
      row({ id: '4', badges: [] }),
    ];
    const ids = (dir: 'asc' | 'desc') => sortRows(rows, { column: 'flags', dir }, PAGE_TASK_COLUMNS).map((r) => r.id);
    expect(ids('asc')).toEqual(['3', '2', '1', '4']);
    expect(ids('desc')).toEqual(['2', '3', '1', '4']);
    expect(PAGE_TASK_COLUMNS.flags?.type).toBe('text');
  });
});

describe('saved state', () => {
  it('shares one key between every initiative', () => {
    expect(viewKey(INITIATIVE_VIEW)).toBe('board.view.v1.initiative');
  });

  it('opens a bare page with the query saved on another initiative, in place', () => {
    const storage = memory();
    saveViewQuery(storage, INITIATIVE_VIEW, 'status=all&sort=ref&dir=desc');
    const replaced: string[] = [];
    const query = openView({ storage, replaceHash: (h) => replaced.push(h) }, INITIATIVE_VIEW, pagePath('second'), '');
    expect(replaced).toEqual(['#/i/second?status=all&sort=ref&dir=desc']);
    expect(viewFromQuery(query)).toEqual({ filter: 'all', sort: { column: 'ref', dir: 'desc' } });
  });

  it('lets a fragment with a query win and saves it', () => {
    const storage = memory();
    saveViewQuery(storage, INITIATIVE_VIEW, 'sort=ref&dir=asc');
    const query = openView({ storage, replaceHash: () => undefined }, INITIATIVE_VIEW, pagePath('a'), 'status=done');
    expect(viewFromQuery(query)).toEqual({ filter: 'done', sort: null });
    expect(JSON.parse(storage.data.get('board.view.v1.initiative')!)).toEqual({ v: 1, query: 'status=done' });
  });
});
