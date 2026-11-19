import { describe, expect, it } from 'vitest';
import type { TaskRow } from './initiativePage';
import { sortRows } from './tableSort';
import { openView, saveViewQuery, viewKey, type ViewStorage } from './viewState';
import {
  INITIATIVE_VIEW,
  PAGE_TASK_COLUMNS,
  PAGE_TASK_SORT_COLUMNS,
  PAGE_TASK_SORT_LABELS,
  filterRowsByTags,
  isCustomised,
  pageTagChoices,
  isPageHash,
  pagePath,
  viewFromQuery,
  viewHash,
  viewToQuery,
} from './initiativePageView';

function row(over: Partial<TaskRow>): TaskRow {
  return { id: 'x', ref: 'a-1', initiative: 'a', title: 't', kind: 'work', status: 'open', badges: [], attention: [], holder: '', marks: [], ...over };
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
    const state = { filter: 'done' as const, tags: [], sort: { column: 'ref', dir: 'desc' as const } };
    expect(viewToQuery(state)).toBe('status=done&sort=ref&dir=desc');
    expect(viewHash('phone-onboard', state)).toBe('#/i/phone-onboard?status=done&sort=ref&dir=desc');
  });

  it('writes only the sort when the status is the default, and nothing for the defaults', () => {
    expect(viewToQuery({ filter: 'not-closed', tags: [], sort: { column: 'kind', dir: 'asc' } })).toBe('sort=kind&dir=asc');
    expect(viewToQuery({ filter: 'all', tags: [], sort: null })).toBe('status=all');
    expect(viewToQuery({ filter: 'not-closed', tags: [], sort: null })).toBe('');
    expect(viewHash('a', { filter: 'not-closed', tags: [], sort: null })).toBe('#/i/a');
  });

  it('reads the status and the sort back from a query', () => {
    expect(viewFromQuery('status=closed&sort=status&dir=desc')).toEqual({ filter: 'closed', tags: [], sort: { column: 'status', dir: 'desc' } });
    expect(viewFromQuery('sort=title')).toEqual({ filter: 'not-closed', tags: [], sort: { column: 'title', dir: 'asc' } });
  });

  it('takes the defaults for an unknown status, an unknown column and a bad direction', () => {
    expect(viewFromQuery('status=weird&sort=bogus&dir=asc')).toEqual({ filter: 'not-closed', tags: [], sort: null });
    expect(viewFromQuery('sort=ref&dir=sideways')).toEqual({ filter: 'not-closed', tags: [], sort: null });
    expect(viewFromQuery('')).toEqual({ filter: 'not-closed', tags: [], sort: null });
  });

  it('round-trips every sortable column in both directions under every status', () => {
    for (const filter of ['not-closed', 'done', 'closed', 'all'] as const) {
      for (const column of PAGE_TASK_SORT_COLUMNS) {
        for (const dir of ['asc', 'desc'] as const) {
          const state = { filter, tags: [], sort: { column, dir } };
          expect(viewFromQuery(viewToQuery(state))).toEqual(state);
        }
      }
    }
  });

  it('says whether the state is not the default', () => {
    expect(isCustomised({ filter: 'not-closed', tags: [], sort: null })).toBe(false);
    expect(isCustomised({ filter: 'done', tags: [], sort: null })).toBe(true);
    expect(isCustomised({ filter: 'not-closed', tags: [], sort: { column: 'ref', dir: 'asc' } })).toBe(true);
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

describe('the Flags column, whole label lists', () => {
  const badge = (label: 'active' | 'blocked' | 'overdue') => ({ key: label, label, tone: 'neutral' as const });
  const ids = (rows: TaskRow[], dir: 'asc' | 'desc') => sortRows(rows, { column: 'flags', dir }, PAGE_TASK_COLUMNS).map((r) => r.id);

  it('sorts by the whole label list, so a task with two flags is compared on both', () => {
    const rows = [
      row({ id: '1', badges: [badge('active'), badge('blocked')] }),
      row({ id: '2', badges: [badge('active')] }),
      row({ id: '3', badges: [badge('active'), badge('overdue')] }),
    ];
    expect(ids(rows, 'asc')).toEqual(['2', '1', '3']);
    expect(ids(rows, 'desc')).toEqual(['3', '1', '2']);
  });

  it('keeps the input order when every task is unflagged, in both directions', () => {
    const rows = [row({ id: '1' }), row({ id: '2' }), row({ id: '3' })];
    expect(ids(rows, 'asc')).toEqual(['1', '2', '3']);
    expect(ids(rows, 'desc')).toEqual(['1', '2', '3']);
  });

  it('keeps tasks with equal flags in the input order', () => {
    const rows = [
      row({ id: '1', badges: [badge('overdue')] }),
      row({ id: '2', badges: [badge('active')] }),
      row({ id: '3', badges: [badge('overdue')] }),
    ];
    expect(ids(rows, 'asc')).toEqual(['2', '1', '3']);
    expect(ids(rows, 'desc')).toEqual(['1', '3', '2']);
  });
});

describe('every column is sortable and survives a reload', () => {
  it('has a phone label, a query encoding and a saved-state restore for each column', () => {
    for (const column of PAGE_TASK_SORT_COLUMNS) {
      expect(PAGE_TASK_SORT_LABELS.some((l) => l.column === column)).toBe(true);
      const state = { filter: 'not-closed' as const, tags: [], sort: { column, dir: 'desc' as const } };
      expect(viewToQuery(state)).toBe(`sort=${column}&dir=desc`);
      const storage = memory();
      saveViewQuery(storage, INITIATIVE_VIEW, viewToQuery(state));
      const query = openView({ storage, replaceHash: () => undefined }, INITIATIVE_VIEW, pagePath('other'), '');
      expect(viewFromQuery(query)).toEqual(state);
    }
  });
});

describe('saved state', () => {
  it('shares one key between every initiative', () => {
    expect(viewKey(INITIATIVE_VIEW)).toBe('vh.view.v1.initiative');
  });

  it('opens a bare page with the query saved on another initiative, in place', () => {
    const storage = memory();
    saveViewQuery(storage, INITIATIVE_VIEW, 'status=all&sort=ref&dir=desc');
    const replaced: string[] = [];
    const query = openView({ storage, replaceHash: (h) => replaced.push(h) }, INITIATIVE_VIEW, pagePath('second'), '');
    expect(replaced).toEqual(['#/i/second?status=all&sort=ref&dir=desc']);
    expect(viewFromQuery(query)).toEqual({ filter: 'all', tags: [], sort: { column: 'ref', dir: 'desc' } });
  });

  it('lets a fragment with a query win and saves it', () => {
    const storage = memory();
    saveViewQuery(storage, INITIATIVE_VIEW, 'sort=ref&dir=asc');
    const query = openView({ storage, replaceHash: () => undefined }, INITIATIVE_VIEW, pagePath('a'), 'status=done');
    expect(viewFromQuery(query)).toEqual({ filter: 'done', tags: [], sort: null });
    expect(JSON.parse(storage.data.get('vh.view.v1.initiative')!)).toEqual({ v: 1, query: 'status=done' });
  });
});

describe('the tag chips (tags only, no initiative dimension)', () => {
  const marks = (...pairs: [string, 'task' | 'initiative' | 'actor'][]) => pairs.map(([tag, source]) => ({ tag, source }));
  const rows = [
    row({ id: '1', marks: marks(['web', 'initiative'], ['ui', 'task']) }),
    row({ id: '2', marks: marks(['web', 'initiative'], ['api', 'task'], ['ui', 'task']) }),
    row({ id: '3', marks: marks(['web', 'initiative'], ['claude', 'actor']) }),
  ];
  const ids = (tags: string[]) => filterRowsByTags(rows, tags).map((r) => r.id);

  it('keeps every row when no tag is chosen', () => {
    expect(ids([])).toEqual(['1', '2', '3']);
  });

  it('keeps the rows with the tag, from any source: own, the initiative\'s or an actor kind', () => {
    expect(ids(['ui'])).toEqual(['1', '2']);
    expect(ids(['web'])).toEqual(['1', '2', '3']);
    expect(ids(['claude'])).toEqual(['3']);
  });

  it('combines tags with AND: a row must carry every tag chosen', () => {
    expect(ids(['ui', 'api'])).toEqual(['2']);
    expect(ids(['api', 'claude'])).toEqual([]);
    expect(ids(['nope'])).toEqual([]);
  });

  it('offers every tag of the rows, sorted, with its sources, plus a chosen tag no row has', () => {
    expect(pageTagChoices(rows, [])).toEqual([
      { tag: 'api', sources: ['task'] },
      { tag: 'claude', sources: ['actor'] },
      { tag: 'ui', sources: ['task'] },
      { tag: 'web', sources: ['initiative'] },
    ]);
    expect(pageTagChoices([], ['gone'])).toEqual([{ tag: 'gone', sources: [] }]);
  });

  it('keeps the tags in the fragment, repeated and after the status, and reads them back', () => {
    const state = { filter: 'done' as const, tags: ['ui', 'api'], sort: { column: 'ref', dir: 'asc' as const } };
    expect(viewToQuery(state)).toBe('status=done&tag=ui&tag=api&sort=ref&dir=asc');
    expect(viewHash('a', { filter: 'not-closed', tags: ['ui'], sort: null })).toBe('#/i/a?tag=ui');
    expect(viewFromQuery('status=done&tag=ui&tag=api&sort=ref&dir=asc')).toEqual(state);
    expect(isCustomised({ filter: 'not-closed', tags: ['ui'], sort: null })).toBe(true);
  });

  it('ignores an initiative parameter and blank or repeated tags', () => {
    expect(viewFromQuery('initiative=x&tag=&tag=ui&tag=ui')).toEqual({ filter: 'not-closed', tags: ['ui'], sort: null });
  });

  it('saves the tags with the view and restores them on a bare route', () => {
    const storage = memory();
    saveViewQuery(storage, INITIATIVE_VIEW, 'tag=ui&tag=api&sort=ref&dir=desc');
    const replaced: string[] = [];
    const query = openView({ storage, replaceHash: (h) => replaced.push(h) }, INITIATIVE_VIEW, pagePath('a'), '');
    expect(replaced).toEqual(['#/i/a?tag=ui&tag=api&sort=ref&dir=desc']);
    expect(viewFromQuery(query).tags).toEqual(['ui', 'api']);
  });
});
