import { describe, expect, it } from 'vitest';
import type { ArchiveRow } from './archive';
import { ARCHIVE_WINDOWS } from './archive';
import {
  ARCHIVE_COLUMNS,
  ARCHIVE_SORT_COLUMNS,
  ARCHIVE_SORT_LABELS,
  ARCHIVE_VIEW,
  archiveTagChoices,
  archiveViewHash,
  queryOfHash,
  viewFromQuery,
  viewToQuery,
} from './archiveView';
import { NO_CHIPS } from './chipFilter';
import { sortRows } from './tableSort';
import { openView, saveViewQuery, viewKey, type ViewStorage } from './viewState';

function row(over: Partial<ArchiveRow>): ArchiveRow {
  return {
    kind: 'task',
    key: 'task:a',
    ref: 'a',
    title: 't',
    href: '#/t/a',
    resolution: 'completed',
    closedAt: '2026-03-01T00:00:00Z',
    closed: '',
    closedTitle: '',
    closedBy: '',
    marks: [],
    ...over,
  };
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

describe('window and sort <-> query encoding', () => {
  it('writes the window first and the sort after it', () => {
    const state = { window: '7' as const, chips: NO_CHIPS, sort: { column: 'closed', dir: 'desc' as const } };
    expect(viewToQuery(state)).toBe('window=7&sort=closed&dir=desc');
    expect(archiveViewHash(state)).toBe('#/archive?window=7&sort=closed&dir=desc');
  });

  it('writes only the sort when the window is the default, only the window when there is no sort, and nothing for the defaults', () => {
    expect(viewToQuery({ window: '30', chips: NO_CHIPS, sort: { column: 'item', dir: 'asc' } })).toBe('sort=item&dir=asc');
    expect(viewToQuery({ window: 'all', chips: NO_CHIPS, sort: null })).toBe('window=all');
    expect(viewToQuery({ window: '30', chips: NO_CHIPS, sort: null })).toBe('');
    expect(archiveViewHash({ window: '30', chips: NO_CHIPS, sort: null })).toBe('#/archive');
    expect(archiveViewHash({ window: '90', chips: NO_CHIPS, sort: null })).toBe('#/archive?window=90');
  });

  it('reads the window and the sort back from a query', () => {
    expect(viewFromQuery('window=all&sort=resolution&dir=desc')).toEqual({ window: 'all', chips: NO_CHIPS, sort: { column: 'resolution', dir: 'desc' } });
    expect(viewFromQuery('sort=kind')).toEqual({ window: '30', chips: NO_CHIPS, sort: { column: 'kind', dir: 'asc' } });
    expect(viewFromQuery('?window=7')).toEqual({ window: '7', chips: NO_CHIPS, sort: null });
  });

  it('takes the defaults for an unknown window, an unknown column and a bad direction', () => {
    expect(viewFromQuery('window=14&sort=action&dir=asc')).toEqual({ window: '30', chips: NO_CHIPS, sort: null });
    expect(viewFromQuery('window=7&sort=item&dir=sideways')).toEqual({ window: '7', chips: NO_CHIPS, sort: null });
    expect(viewFromQuery('')).toEqual({ window: '30', chips: NO_CHIPS, sort: null });
  });

  it('round-trips every window with every sortable column in both directions', () => {
    for (const { value: window } of ARCHIVE_WINDOWS) {
      for (const column of ARCHIVE_SORT_COLUMNS) {
        for (const dir of ['asc', 'desc'] as const) {
          const state = { window, chips: NO_CHIPS, sort: { column, dir } };
          expect(viewFromQuery(viewToQuery(state))).toEqual(state);
          expect(viewFromQuery(queryOfHash(archiveViewHash(state)))).toEqual(state);
        }
      }
    }
  });

  it('takes the query of a fragment after its first question mark', () => {
    expect(queryOfHash('#/archive')).toBe('');
    expect(queryOfHash('#/archive?window=7&sort=item')).toBe('window=7&sort=item');
    expect(queryOfHash('#/archive?x=?y')).toBe('x=?y');
    expect(queryOfHash('?sort=item')).toBe('sort=item');
  });
});

describe('columns', () => {
  it('names a label for every sortable column, in the table order', () => {
    expect(ARCHIVE_SORT_LABELS.map((l) => l.column)).toEqual(ARCHIVE_SORT_COLUMNS);
    expect(ARCHIVE_SORT_COLUMNS).toEqual(['item', 'kind', 'resolution', 'closed']);
    expect(ARCHIVE_SORT_LABELS.map((l) => l.label)).toEqual(['Item', 'Kind', 'Resolution', 'Closed']);
    expect(ARCHIVE_VIEW).toBe('archive');
  });

  it('gives each column its comparator: text, kind, status and time', () => {
    expect(Object.fromEntries(Object.entries(ARCHIVE_COLUMNS).map(([name, c]) => [name, c.type]))).toEqual({ item: 'text', kind: 'kind', resolution: 'status', closed: 'time' });
  });

  const rows = [
    row({ key: 'task:b', ref: 'b', title: 'beta', kind: 'task', resolution: 'dropped', closedAt: '2026-03-02T00:00:00Z' }),
    row({ key: 'initiative:a', ref: 'a', title: 'Alpha', kind: 'initiative', resolution: 'initiative closed', closedAt: '2026-03-03T00:00:00Z' }),
    row({ key: 'task:c', ref: 'c', title: 'charlie', kind: 'task', resolution: 'completed', closedAt: '2026-03-01T00:00:00Z' }),
  ];
  const refs = (column: string, dir: 'asc' | 'desc') => sortRows(rows, { column, dir }, ARCHIVE_COLUMNS).map((r) => r.ref);

  it('sorts titles ignoring case and closing times chronologically', () => {
    expect(refs('item', 'asc')).toEqual(['a', 'b', 'c']);
    expect(refs('item', 'desc')).toEqual(['c', 'b', 'a']);
    expect(refs('closed', 'asc')).toEqual(['c', 'b', 'a']);
    expect(refs('closed', 'desc')).toEqual(['a', 'b', 'c']);
  });

  it('sorts resolutions by text and kinds by text, ties keeping the input order', () => {
    expect(refs('resolution', 'asc')).toEqual(['c', 'b', 'a']);
    expect(refs('resolution', 'desc')).toEqual(['a', 'b', 'c']);
    expect(refs('kind', 'asc')).toEqual(['a', 'b', 'c']);
    expect(refs('kind', 'desc')).toEqual(['b', 'c', 'a']);
  });

  it('puts a row with no closing time last in both directions', () => {
    const open = [row({ ref: 'x', closedAt: '' }), ...rows];
    for (const dir of ['asc', 'desc'] as const) {
      expect(sortRows(open, { column: 'closed', dir }, ARCHIVE_COLUMNS).at(-1)?.ref).toBe('x');
    }
  });
});

describe('saved state', () => {
  it('opens a bare route with the saved window and sort, in place', () => {
    const storage = memory();
    saveViewQuery(storage, ARCHIVE_VIEW, 'window=all&sort=item&dir=desc');
    expect(viewKey(ARCHIVE_VIEW)).toBe('vh.view.v1.archive');
    expect(storage.data.has('vh.view.v1.archive')).toBe(true);
    const replaced: string[] = [];
    const query = openView({ storage, replaceHash: (h) => replaced.push(h) }, ARCHIVE_VIEW, '#/archive', '');
    expect(replaced).toEqual(['#/archive?window=all&sort=item&dir=desc']);
    expect(viewFromQuery(query)).toEqual({ window: 'all', chips: NO_CHIPS, sort: { column: 'item', dir: 'desc' } });
  });

  it('lets a fragment with a query win and saves it', () => {
    const storage = memory();
    saveViewQuery(storage, ARCHIVE_VIEW, 'sort=item&dir=asc');
    const query = openView({ storage, replaceHash: () => undefined }, ARCHIVE_VIEW, '#/archive', 'window=7');
    expect(viewFromQuery(query)).toEqual({ window: '7', chips: NO_CHIPS, sort: null });
    expect(JSON.parse(storage.data.get('vh.view.v1.archive')!)).toEqual({ v: 1, query: 'window=7' });
  });

  it('forgets the key when the state is back to the defaults', () => {
    const storage = memory();
    saveViewQuery(storage, ARCHIVE_VIEW, viewToQuery({ window: '90', chips: NO_CHIPS, sort: null }));
    expect(storage.data.has('vh.view.v1.archive')).toBe(true);
    saveViewQuery(storage, ARCHIVE_VIEW, viewToQuery({ window: '30', chips: NO_CHIPS, sort: null }));
    expect(storage.data.has('vh.view.v1.archive')).toBe(false);
  });
});

describe('the chip filter in the view state', () => {
  const chips = { tags: ['x', 'y'], initiatives: ['b', 'a'] };

  it('writes the window, then the repeated initiative and tag, then the sort, in the order chosen', () => {
    const state = { window: '7' as const, chips, sort: { column: 'closed', dir: 'desc' as const } };
    expect(viewToQuery(state)).toBe('window=7&initiative=b&initiative=a&tag=x&tag=y&sort=closed&dir=desc');
    expect(archiveViewHash({ window: '30', chips: { tags: ['x'], initiatives: [] }, sort: null })).toBe('#/archive?tag=x');
    expect(archiveViewHash({ window: '30', chips: NO_CHIPS, sort: null })).toBe('#/archive');
  });

  it('reads the chips back from a query, keeping each once and dropping blanks', () => {
    expect(viewFromQuery('tag=x&tag=y&initiative=b&initiative=a&tag=x&tag=')).toEqual({ window: '30', chips, sort: null });
    for (const state of [{ window: '90' as const, chips, sort: { column: 'item', dir: 'asc' as const } }, { window: '30' as const, chips, sort: null }]) {
      expect(viewFromQuery(queryOfHash(archiveViewHash(state)))).toEqual(state);
    }
  });

  it('offers the tags the closed tasks and initiatives carry, with their sources, plus the chosen ones', () => {
    const tasks = [{ tag_marks: [{ tag: 'b', source: 'task' as const }, { tag: 'a', source: 'initiative' as const }] }, { tag_marks: [{ tag: 'claude', source: 'actor' as const }] }];
    const initiatives = [{ tags: ['a', 'z'] }];
    expect(archiveTagChoices([], tasks, initiatives, ['gone'])).toEqual([
      { tag: 'a', sources: ['initiative'] },
      { tag: 'b', sources: ['task'] },
      { tag: 'claude', sources: ['actor'] },
      { tag: 'gone', sources: [] },
      { tag: 'z', sources: ['initiative'] },
    ]);
  });

  it('keeps the chips already on offer when a filtered read holds fewer tasks', () => {
    const known = [{ tag: 'b', sources: ['task' as const] }];
    expect(archiveTagChoices(known, [], [], []).map((c) => c.tag)).toEqual(['b']);
  });
});
