import { describe, expect, it } from 'vitest';
import type { InitiativeListItem } from './initiatives';
import { sortRows } from './tableSort';
import { openView, saveViewQuery, viewKey, type ViewStorage } from './viewState';
import {
  INITIATIVES_VIEW,
  INITIATIVE_COLUMNS,
  INITIATIVE_SORT_COLUMNS,
  INITIATIVE_SORT_LABELS,
  isCustomised,
  viewFromQuery,
  viewHash,
  viewToQuery,
} from './initiativesView';

function row(over: Partial<InitiativeListItem>): InitiativeListItem {
  return { slug: 'a', title: 't', status: 'open', tags: [], counts: { open: 0, done: 0, closed: 0 }, created_at: '2026-01-01T00:00:00Z', ...over } as InitiativeListItem;
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
  it('writes the status first and the sort after it', () => {
    const state = { filter: 'closed' as const, sort: { column: 'title', dir: 'desc' as const } };
    expect(viewToQuery(state)).toBe('status=closed&sort=title&dir=desc');
    expect(viewHash(state)).toBe('#/initiatives?status=closed&sort=title&dir=desc');
  });

  it('writes only the sort when the status is the default, and nothing for the defaults', () => {
    expect(viewToQuery({ filter: 'open', sort: { column: 'created', dir: 'asc' } })).toBe('sort=created&dir=asc');
    expect(viewToQuery({ filter: 'all', sort: null })).toBe('status=all');
    expect(viewToQuery({ filter: 'open', sort: null })).toBe('');
    expect(viewHash({ filter: 'open', sort: null })).toBe('#/initiatives');
  });

  it('reads the status and the sort back from a query', () => {
    expect(viewFromQuery('status=all&sort=status&dir=desc')).toEqual({ filter: 'all', sort: { column: 'status', dir: 'desc' } });
    expect(viewFromQuery('sort=created')).toEqual({ filter: 'open', sort: { column: 'created', dir: 'asc' } });
  });

  it('takes the defaults for an unknown status, an unknown column and a bad direction', () => {
    expect(viewFromQuery('status=weird&sort=bogus&dir=asc')).toEqual({ filter: 'open', sort: null });
    expect(viewFromQuery('sort=title&dir=sideways')).toEqual({ filter: 'open', sort: null });
    expect(viewFromQuery('')).toEqual({ filter: 'open', sort: null });
  });

  it('round-trips every sortable column in both directions under every status', () => {
    for (const filter of ['open', 'closed', 'all'] as const) {
      for (const column of INITIATIVE_SORT_COLUMNS) {
        for (const dir of ['asc', 'desc'] as const) {
          const state = { filter, sort: { column, dir } };
          expect(viewFromQuery(viewToQuery(state))).toEqual(state);
        }
      }
    }
  });

  it('says whether the state is not the default', () => {
    expect(isCustomised({ filter: 'open', sort: null })).toBe(false);
    expect(isCustomised({ filter: 'closed', sort: null })).toBe(true);
    expect(isCustomised({ filter: 'open', sort: { column: 'title', dir: 'asc' } })).toBe(true);
  });
});

describe('columns', () => {
  it('names a label for every sortable column, in the table order', () => {
    expect(INITIATIVE_SORT_LABELS.map((l) => l.column)).toEqual(INITIATIVE_SORT_COLUMNS);
    expect(INITIATIVE_SORT_COLUMNS).toEqual(['title', 'status', 'tags', 'tasks', 'created']);
    expect(INITIATIVE_SORT_LABELS.map((l) => l.label)).toEqual(['Title', 'Status', 'Tags', 'Tasks', 'Created']);
    expect(INITIATIVES_VIEW).toBe('initiatives');
  });

  it('sorts titles ignoring case, statuses in lifecycle order and creation times chronologically', () => {
    const rows = [
      row({ slug: 'b', title: 'beta', status: 'closed', created_at: '2026-03-01T00:00:00Z' }),
      row({ slug: 'a', title: 'Alpha', status: 'open', created_at: '2026-05-01T00:00:00Z' }),
      row({ slug: 'c', title: 'charlie', status: 'open', created_at: '2026-04-01T00:00:00Z' }),
    ];
    const slugs = (column: string, dir: 'asc' | 'desc') => sortRows(rows, { column, dir }, INITIATIVE_COLUMNS).map((r) => r.slug);
    expect(slugs('title', 'asc')).toEqual(['a', 'b', 'c']);
    expect(slugs('title', 'desc')).toEqual(['c', 'b', 'a']);
    expect(slugs('status', 'asc')).toEqual(['a', 'c', 'b']);
    expect(slugs('status', 'desc')).toEqual(['b', 'a', 'c']);
    expect(slugs('created', 'asc')).toEqual(['b', 'c', 'a']);
    expect(slugs('created', 'desc')).toEqual(['a', 'c', 'b']);
  });
});

describe('saved state', () => {
  it('opens a bare route with the saved query, in place', () => {
    const storage = memory();
    saveViewQuery(storage, INITIATIVES_VIEW, 'status=all&sort=title&dir=desc');
    expect(storage.data.has('vh.view.v1.initiatives')).toBe(true);
    expect(viewKey(INITIATIVES_VIEW)).toBe('vh.view.v1.initiatives');
    const replaced: string[] = [];
    const query = openView({ storage, replaceHash: (h) => replaced.push(h) }, INITIATIVES_VIEW, '#/initiatives', '');
    expect(replaced).toEqual(['#/initiatives?status=all&sort=title&dir=desc']);
    expect(viewFromQuery(query)).toEqual({ filter: 'all', sort: { column: 'title', dir: 'desc' } });
  });

  it('lets a fragment with a query win and saves it', () => {
    const storage = memory();
    saveViewQuery(storage, INITIATIVES_VIEW, 'sort=title&dir=asc');
    const query = openView({ storage, replaceHash: () => undefined }, INITIATIVES_VIEW, '#/initiatives', 'status=closed');
    expect(viewFromQuery(query)).toEqual({ filter: 'closed', sort: null });
    expect(JSON.parse(storage.data.get('vh.view.v1.initiatives')!)).toEqual({ v: 1, query: 'status=closed' });
  });

  it('sorts Tags as text with the untagged initiatives last in both directions', () => {
    const rows = [
      row({ slug: 'n1', tags: [] }),
      row({ slug: 'b', tags: ['beta'] }),
      row({ slug: 'a', tags: ['Alpha', 'zed'] }),
      row({ slug: 'n2', tags: [] }),
    ];
    const slugs = (dir: 'asc' | 'desc') => sortRows(rows, { column: 'tags', dir }, INITIATIVE_COLUMNS).map((r) => r.slug);
    expect(slugs('asc')).toEqual(['a', 'b', 'n1', 'n2']);
    expect(slugs('desc')).toEqual(['b', 'a', 'n1', 'n2']);
  });

  it('sorts Tasks by the number of tasks in all, so 10 follows 9, ties in input order', () => {
    const rows = [
      row({ slug: 'ten', counts: { open: 4, done: 3, closed: 3 } }),
      row({ slug: 'nine', counts: { open: 9, done: 0, closed: 0 } }),
      row({ slug: 'none', counts: { open: 0, done: 0, closed: 0 } }),
      row({ slug: 'nine-b', counts: { open: 0, done: 5, closed: 4 } }),
    ];
    const slugs = (dir: 'asc' | 'desc') => sortRows(rows, { column: 'tasks', dir }, INITIATIVE_COLUMNS).map((r) => r.slug);
    expect(slugs('asc')).toEqual(['none', 'nine', 'nine-b', 'ten']);
    expect(slugs('desc')).toEqual(['ten', 'nine', 'nine-b', 'none']);
    expect(INITIATIVE_COLUMNS.tasks?.type).toBe('number');
  });

  it('sorts Tags ignoring letter case, and an initiative with no tags stays last even when it comes first in the input', () => {
    const rows = [
      row({ slug: 'none', tags: [] }),
      row({ slug: 'upper', tags: ['Zulu'] }),
      row({ slug: 'lower', tags: ['alpha'] }),
      row({ slug: 'mid', tags: ['Mike'] }),
    ];
    const slugs = (dir: 'asc' | 'desc') => sortRows(rows, { column: 'tags', dir }, INITIATIVE_COLUMNS).map((r) => r.slug);
    expect(slugs('asc')).toEqual(['lower', 'mid', 'upper', 'none']);
    expect(slugs('desc')).toEqual(['upper', 'mid', 'lower', 'none']);
  });

  it('counts an initiative with no tasks as zero, not as empty: it leads ascending and trails descending', () => {
    const rows = [row({ slug: 'two', counts: { open: 2, done: 0, closed: 0 } }), row({ slug: 'zero' }), row({ slug: 'one', counts: { open: 0, done: 0, closed: 1 } })];
    const slugs = (dir: 'asc' | 'desc') => sortRows(rows, { column: 'tasks', dir }, INITIATIVE_COLUMNS).map((r) => r.slug);
    expect(slugs('asc')).toEqual(['zero', 'one', 'two']);
    expect(slugs('desc')).toEqual(['two', 'one', 'zero']);
  });

  it('gives every column of the table a sort, a phone Sort choice and a place in the query and the saved state', () => {
    for (const column of ['tags', 'tasks']) {
      expect(Object.keys(INITIATIVE_COLUMNS)).toContain(column);
      expect(INITIATIVE_SORT_LABELS.map((l) => l.column)).toContain(column);
      expect(viewToQuery({ filter: 'open', sort: { column, dir: 'desc' } })).toBe(`sort=${column}&dir=desc`);
      const storage = memory();
      saveViewQuery(storage, INITIATIVES_VIEW, `sort=${column}&dir=asc`);
      const replaced: string[] = [];
      openView({ storage, replaceHash: (h) => replaced.push(h) }, INITIATIVES_VIEW, '#/initiatives', '');
      expect(replaced).toEqual([`#/initiatives?sort=${column}&dir=asc`]);
    }
  });
});
