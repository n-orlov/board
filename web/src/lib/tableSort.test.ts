import { describe, expect, it } from 'vitest';
import {
  ariaSort,
  choiceOf,
  compareCells,
  compareRows,
  nextSort,
  sortChoices,
  sortFromQuery,
  sortOfChoice,
  sortRows,
  sortToQuery,
  type Columns,
  type SortState,
} from './tableSort';

interface Row {
  id: number;
  ref?: string;
  title?: string | null;
  at?: string | number | null;
  status?: string;
  kind?: string;
}

const columns: Columns<Row> = {
  ref: { type: 'ref', value: (r) => r.ref },
  title: { type: 'text', value: (r) => r.title },
  at: { type: 'time', value: (r) => r.at },
  status: { type: 'status', value: (r) => r.status },
  kind: { type: 'kind', value: (r) => r.kind },
};

const ids = (rows: Row[]) => rows.map((r) => r.id);

describe('ref comparator', () => {
  const rows: Row[] = [
    { id: 1, ref: 'board-10' },
    { id: 2, ref: 'board-9' },
    { id: 3, ref: 'board-100' },
    { id: 4, ref: 'api-2' },
  ];
  it('orders by the number, so board-9 sorts before board-10', () => {
    expect(sortRows(rows, { column: 'ref', dir: 'asc' }, columns).map((r) => r.ref)).toEqual(['api-2', 'board-9', 'board-10', 'board-100']);
    expect(sortRows(rows, { column: 'ref', dir: 'desc' }, columns).map((r) => r.ref)).toEqual(['board-100', 'board-10', 'board-9', 'api-2']);
  });
  it('compares two refs directly', () => {
    expect(compareCells('ref', 'board-9', 'board-10')).toBeLessThan(0);
    expect(compareCells('ref', 'board-10', 'board-9')).toBeGreaterThan(0);
    expect(compareCells('ref', 'T7', 'T7')).toBe(0);
  });
  it('falls back to text for a value with no trailing number', () => {
    expect(compareCells('ref', 'alpha', 'beta')).toBeLessThan(0);
    expect(compareCells('ref', 'alpha', 'board-1')).toBeLessThan(0);
    expect(compareCells('ref', 'board-1', 'alpha')).toBeGreaterThan(0);
  });
});

describe('text comparator', () => {
  it('ignores case and uses localeCompare', () => {
    const rows: Row[] = [{ id: 1, title: 'banana' }, { id: 2, title: 'Apple' }, { id: 3, title: 'cherry' }, { id: 4, title: 'apple pie' }];
    expect(ids(sortRows(rows, { column: 'title', dir: 'asc' }, columns))).toEqual([2, 4, 1, 3]);
    expect(ids(sortRows(rows, { column: 'title', dir: 'desc' }, columns))).toEqual([3, 1, 4, 2]);
  });
  it('treats letters that differ only by case as equal', () => {
    expect(compareCells('text', 'Alpha', 'alpha')).toBe(0);
    expect(compareCells('text', 'é', 'f')).toBeLessThan(0);
  });
});

describe('time comparator', () => {
  it('orders chronologically, not as text', () => {
    const rows: Row[] = [
      { id: 1, at: '2026-11-10T09:00:00Z' },
      { id: 2, at: '2026-11-09T23:59:59Z' },
      { id: 3, at: '2026-11-10T09:00:00+02:00' },
      { id: 4, at: '2025-12-31T00:00:00Z' },
    ];
    expect(ids(sortRows(rows, { column: 'at', dir: 'asc' }, columns))).toEqual([4, 2, 3, 1]);
    expect(ids(sortRows(rows, { column: 'at', dir: 'desc' }, columns))).toEqual([1, 3, 2, 4]);
  });
  it('accepts epoch numbers', () => {
    expect(compareCells('time', 5, 20)).toBeLessThan(0);
    expect(compareCells('time', 20, 5)).toBeGreaterThan(0);
  });
  it('puts a time that does not parse last in both directions', () => {
    const rows: Row[] = [{ id: 1, at: 'not a time' }, { id: 2, at: '2026-01-01T00:00:00Z' }];
    expect(ids(sortRows(rows, { column: 'at', dir: 'asc' }, columns))).toEqual([2, 1]);
    expect(ids(sortRows(rows, { column: 'at', dir: 'desc' }, columns))).toEqual([2, 1]);
  });
});

describe('status and kind comparators', () => {
  it('order status by lifecycle, not by letter', () => {
    const rows: Row[] = [{ id: 1, status: 'closed' }, { id: 2, status: 'open' }, { id: 3, status: 'done' }, { id: 4, status: 'Open' }];
    expect(ids(sortRows(rows, { column: 'status', dir: 'asc' }, columns))).toEqual([2, 4, 3, 1]);
    expect(ids(sortRows(rows, { column: 'status', dir: 'desc' }, columns))).toEqual([1, 3, 2, 4]);
  });
  it('order kind with work before watch', () => {
    const rows: Row[] = [{ id: 1, kind: 'watch' }, { id: 2, kind: 'work' }];
    expect(ids(sortRows(rows, { column: 'kind', dir: 'asc' }, columns))).toEqual([2, 1]);
    expect(ids(sortRows(rows, { column: 'kind', dir: 'desc' }, columns))).toEqual([1, 2]);
  });
  it('put an unknown value after the known ones and order unknown values as text', () => {
    expect(compareCells('status', 'odd', 'open')).toBeGreaterThan(0);
    expect(compareCells('status', 'open', 'odd')).toBeLessThan(0);
    expect(compareCells('status', 'odd', 'strange')).toBeLessThan(0);
    expect(compareCells('kind', 'zzz', 'work')).toBeGreaterThan(0);
  });
});

describe('lifecycle order', () => {
  it('runs open, active, live, pending, answered, done, ended, expired, closed', () => {
    const order = ['open', 'active', 'live', 'pending', 'answered', 'done', 'ended', 'expired', 'closed'];
    for (let i = 1; i < order.length; i++) expect(compareCells('status', order[i - 1]!, order[i]!)).toBeLessThan(0);
    expect(compareCells('kind', 'work', 'watch')).toBeLessThan(0);
  });
  it('reads a ref only from the start of the value', () => {
    expect(compareCells('ref', 'x board-2', 'board-1')).toBeGreaterThan(0);
  });
});

describe('empty values and ties', () => {
  const rows: Row[] = [
    { id: 1, title: 'b' },
    { id: 2, title: null },
    { id: 3, title: 'a' },
    { id: 4, title: '' },
    { id: 5 },
    { id: 6, title: '  ' },
  ];
  it('sit last in ascending order', () => {
    expect(ids(sortRows(rows, { column: 'title', dir: 'asc' }, columns))).toEqual([3, 1, 2, 4, 5, 6]);
  });
  it('sit last in descending order too, in their input order', () => {
    expect(ids(sortRows(rows, { column: 'title', dir: 'desc' }, columns))).toEqual([1, 3, 2, 4, 5, 6]);
  });
  it('treat NaN as empty', () => {
    expect(compareRows('time', 'asc', Number.NaN, 3)).toBeGreaterThan(0);
    expect(compareRows('time', 'desc', 3, Number.NaN)).toBeLessThan(0);
    expect(compareRows('time', 'asc', Number.NaN, Number.NaN)).toBe(0);
  });
  it('keep the input order for equal cells, ascending and descending', () => {
    const tied: Row[] = [{ id: 1, status: 'open' }, { id: 2, status: 'done' }, { id: 3, status: 'open' }, { id: 4, status: 'done' }];
    expect(ids(sortRows(tied, { column: 'status', dir: 'asc' }, columns))).toEqual([1, 3, 2, 4]);
    expect(ids(sortRows(tied, { column: 'status', dir: 'desc' }, columns))).toEqual([2, 4, 1, 3]);
  });
});

describe('sortRows', () => {
  const rows: Row[] = [{ id: 1, title: 'b' }, { id: 2, title: 'a' }];
  it('returns a copy in the input order for the default', () => {
    const out = sortRows(rows, null, columns);
    expect(ids(out)).toEqual([1, 2]);
    expect(out).not.toBe(rows);
  });
  it('keeps the input order for a column the table lacks', () => {
    expect(ids(sortRows(rows, { column: 'nope', dir: 'asc' }, columns))).toEqual([1, 2]);
  });
  it('never changes the input', () => {
    sortRows(rows, { column: 'title', dir: 'asc' }, columns);
    expect(ids(rows)).toEqual([1, 2]);
  });
});

describe('the cycle', () => {
  it('goes asc, desc, default, and a new column starts at asc', () => {
    const first = nextSort(null, 'title');
    expect(first).toEqual({ column: 'title', dir: 'asc' });
    const second = nextSort(first, 'title');
    expect(second).toEqual({ column: 'title', dir: 'desc' });
    expect(nextSort(second, 'title')).toBeNull();
    expect(nextSort(second, 'ref')).toEqual({ column: 'ref', dir: 'asc' });
  });
  it('names the aria-sort of each header', () => {
    const sort: SortState = { column: 'title', dir: 'desc' };
    expect(ariaSort(sort, 'title')).toBe('descending');
    expect(ariaSort({ column: 'title', dir: 'asc' }, 'title')).toBe('ascending');
    expect(ariaSort(sort, 'ref')).toBe('none');
    expect(ariaSort(null, 'title')).toBe('none');
  });
});

describe('the query', () => {
  const allowed = ['ref', 'title', 'at'];
  it('is absent for the default order', () => {
    expect(sortToQuery(null)).toBe('');
  });
  it('round trips every column and direction', () => {
    for (const column of allowed) {
      for (const dir of ['asc', 'desc'] as const) {
        const query = sortToQuery({ column, dir });
        expect(query).toBe(`sort=${column}&dir=${dir}`);
        expect(sortFromQuery(query, allowed)).toEqual({ column, dir });
      }
    }
    expect(sortFromQuery(sortToQuery(null), allowed)).toBeNull();
  });
  it('reads a query among other parameters', () => {
    expect(sortFromQuery('status=done&sort=at&dir=desc&q=x', allowed)).toEqual({ column: 'at', dir: 'desc' });
  });
  it('falls back to the default for an unknown column or direction, and asc for a missing direction', () => {
    expect(sortFromQuery('sort=bogus&dir=asc', allowed)).toBeNull();
    expect(sortFromQuery('sort=ref&dir=sideways', allowed)).toBeNull();
    expect(sortFromQuery('dir=desc', allowed)).toBeNull();
    expect(sortFromQuery('sort=ref', allowed)).toEqual({ column: 'ref', dir: 'asc' });
    expect(sortFromQuery('', allowed)).toBeNull();
  });
});

describe('the phone Sort select', () => {
  const cols = [{ column: 'ref', label: 'Ref' }, { column: 'title', label: 'Title' }];
  it('offers the default and both directions of each column', () => {
    expect(sortChoices(cols)).toEqual([
      { value: '', label: 'Default order' },
      { value: 'ref:asc', label: 'Ref (ascending)' },
      { value: 'ref:desc', label: 'Ref (descending)' },
      { value: 'title:asc', label: 'Title (ascending)' },
      { value: 'title:desc', label: 'Title (descending)' },
    ]);
  });
  it('maps a choice to a sort and back', () => {
    expect(sortOfChoice('title:desc')).toEqual({ column: 'title', dir: 'desc' });
    expect(sortOfChoice('')).toBeNull();
    expect(sortOfChoice('title:up')).toBeNull();
    expect(choiceOf({ column: 'ref', dir: 'asc' })).toBe('ref:asc');
    expect(choiceOf(null)).toBe('');
    for (const choice of sortChoices(cols)) expect(choiceOf(sortOfChoice(choice.value))).toBe(choice.value);
  });
});
