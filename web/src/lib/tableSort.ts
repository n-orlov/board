// Sorting for the tables (PRD R16): one comparator per column type, the asc -> desc -> default cycle, and the
// `sort=<column>&dir=asc|desc` query that keeps the order across a reload. The default order is the one the
// server returns, so it is never written to the URL.

export type ColumnType = 'ref' | 'text' | 'time' | 'status' | 'kind' | 'number';
export type Direction = 'asc' | 'desc';
export interface Sort {
  column: string;
  dir: Direction;
}
/** The state of a table's sort: `null` is the default order. */
export type SortState = Sort | null;

/** A cell's value as the comparator reads it; null, undefined and the empty string are "empty". */
export type Cell = string | number | null | undefined;

/** Status in lifecycle order: a task is worked on, then done, then closed; an ask is asked, then answered. */
export const STATUS_ORDER: readonly string[] = ['open', 'active', 'live', 'pending', 'answered', 'done', 'ended', 'expired', 'closed'];
/** Kind in the order a board is read: the work first, then what is watched. */
export const KIND_ORDER: readonly string[] = ['work', 'watch'];

const REF = /^(.*?)(\d+)$/;

function isEmpty(cell: Cell): boolean {
  if (cell === null || cell === undefined) return true;
  return typeof cell === 'number' ? Number.isNaN(cell) : cell.trim() === '';
}

function text(a: string, b: string): number {
  return a.localeCompare(b, undefined, { sensitivity: 'base' });
}

/** Refs compare by prefix, then by the number after it, so `board-9` sorts before `board-10`. */
function ref(a: string, b: string): number {
  const x = REF.exec(a.trim());
  const y = REF.exec(b.trim());
  if (!x || !y) return text(a, b);
  return text(x[1]!, y[1]!) || Number(x[2]) - Number(y[2]);
}

function instant(cell: Cell): number {
  return typeof cell === 'number' ? cell : Date.parse(cell as string);
}

function rank(order: readonly string[], a: string, b: string): number {
  const x = order.indexOf(a.trim().toLowerCase());
  const y = order.indexOf(b.trim().toLowerCase());
  if (x < 0 && y < 0) return text(a, b);
  if (x < 0) return 1;
  return y < 0 ? -1 : x - y;
}

/** Compares two non-empty cells of a column type, ascending. */
export function compareCells(type: ColumnType, a: Cell, b: Cell): number {
  switch (type) {
    case 'ref':
      return ref(String(a), String(b));
    case 'time':
      return instant(a) - instant(b);
    case 'number':
      return Number(a) - Number(b);
    case 'status':
      return rank(STATUS_ORDER, String(a), String(b));
    case 'kind':
      return rank(KIND_ORDER, String(a), String(b));
    default:
      return text(String(a), String(b));
  }
}

function blank(type: ColumnType, cell: Cell): boolean {
  return isEmpty(cell) || (type === 'time' && Number.isNaN(instant(cell))) || (type === 'number' && Number.isNaN(Number(cell)));
}

/** Empty cells (and a time that does not parse) sit last, whichever the direction. Ties return 0. */
export function compareRows(type: ColumnType, dir: Direction, a: Cell, b: Cell): number {
  const emptyA = blank(type, a);
  const emptyB = blank(type, b);
  if (emptyA || emptyB) return Number(emptyA) - Number(emptyB);
  const order = compareCells(type, a, b);
  return dir === 'asc' ? order : -order;
}

/** What a table says about one column: how to compare it and how to read the cell of a row. */
export interface Column<T> {
  type: ColumnType;
  value: (row: T) => Cell;
}
export type Columns<T> = Readonly<Record<string, Column<T>>>;

/** A sorted copy of the rows. The default order (no sort, or a column the table does not have) keeps the input's. */
export function sortRows<T>(rows: readonly T[], sort: SortState, columns: Columns<T>): T[] {
  const column = sort ? columns[sort.column] : undefined;
  if (!sort || !column) return [...rows];
  // Array.prototype.sort is stable, and a tie returns 0 in both directions, so ties keep the input order.
  return [...rows].sort((a, b) => compareRows(column.type, sort.dir, column.value(a), column.value(b)));
}

/** The state after a click on a column's header: asc, then desc, then the default; another column starts at asc. */
export function nextSort(current: SortState, column: string): SortState {
  if (!current || current.column !== column) return { column, dir: 'asc' };
  return current.dir === 'asc' ? { column, dir: 'desc' } : null;
}

/** The `aria-sort` value of a column's header. */
export function ariaSort(current: SortState, column: string): 'ascending' | 'descending' | 'none' {
  if (!current || current.column !== column) return 'none';
  return current.dir === 'asc' ? 'ascending' : 'descending';
}

/** The query of a sort: `sort=<column>&dir=asc|desc`, and empty for the default order. */
export function sortToQuery(sort: SortState): string {
  if (!sort) return '';
  return new URLSearchParams({ sort: sort.column, dir: sort.dir }).toString();
}

/** The sort a query names. An unknown column, a bad `dir` or a lone `sort` is the default; a missing `dir` is `asc`. */
export function sortFromQuery(query: string, allowed: readonly string[]): SortState {
  const params = new URLSearchParams(query);
  const column = params.get('sort');
  if (column === null || !allowed.includes(column)) return null;
  const dir = params.get('dir') ?? 'asc';
  return dir === 'asc' || dir === 'desc' ? { column, dir } : null;
}

/** One choice of the phone's Sort select. `value` is `<column>:<dir>`, or the empty string for the default order. */
export interface SortChoice {
  value: string;
  label: string;
}

/** The choices of the Sort select: the default, then each column ascending and descending. */
export function sortChoices(columns: readonly { column: string; label: string }[]): SortChoice[] {
  return [
    { value: '', label: 'Default order' },
    ...columns.flatMap(({ column, label }) => [
      { value: `${column}:asc`, label: `${label} (ascending)` },
      { value: `${column}:desc`, label: `${label} (descending)` },
    ]),
  ];
}

export function choiceOf(sort: SortState): string {
  return sort ? `${sort.column}:${sort.dir}` : '';
}

export function sortOfChoice(value: string): SortState {
  const at = value.lastIndexOf(':');
  if (at < 0) return null;
  const dir = value.slice(at + 1);
  return dir === 'asc' || dir === 'desc' ? { column: value.slice(0, at), dir } : null;
}
