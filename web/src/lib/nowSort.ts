// The one sort of the Now view (PRD R3, SPEC 11): the shared sort control above the bands orders the rows inside every
// band, the four Needs me bands and both lists of the right column alike. Newest first is the default, then oldest first
// and by actor (the holder or asker handle, then newest first). The choice is remembered per browser under `vh.now.sort.v1`.
import type { SortChoice, SortState } from './tableSort';
import type { SortColumn } from './sortControl';

export type NowOrder = 'newest' | 'oldest' | 'actor';

export const NOW_ORDERS: readonly NowOrder[] = ['newest', 'oldest', 'actor'];
export const DEFAULT_ORDER: NowOrder = 'newest';
/** The versioned storage key of the order. */
export const ORDER_STORAGE_KEY = 'vh.now.sort.v1';

/** The columns the shared control is given: its three options are the default (newest first), time ascending and actor. */
export const NOW_COLUMNS: readonly SortColumn[] = [
  { column: 'time', label: 'Time' },
  { column: 'actor', label: 'Actor' },
];

/** The options the control shows, in words that fit the three orders. */
export const NOW_CHOICES: readonly SortChoice[] = [
  { value: '', label: 'Newest first' },
  { value: 'time:asc', label: 'Oldest first' },
  { value: 'actor:asc', label: 'By actor' },
];

/** The sort state the shared control shows for an order. */
export function orderToSort(order: NowOrder): SortState {
  if (order === 'oldest') return { column: 'time', dir: 'asc' };
  return order === 'actor' ? { column: 'actor', dir: 'asc' } : null;
}

/** The order a control choice names: the default order is newest first. */
export function sortToOrder(sort: SortState): NowOrder {
  if (sort === null) return 'newest';
  if (sort.column === 'actor') return 'actor';
  return sort.column === 'time' && sort.dir === 'asc' ? 'oldest' : 'newest';
}

/** A stored value as an order: anything that is not one of the three (or absent) is the default. */
export function parseOrder(raw: string | null | undefined): NowOrder {
  return NOW_ORDERS.find((o) => o === raw) ?? DEFAULT_ORDER;
}

/** The slice of `Storage` the preference uses. */
export type OrderStorage = Pick<Storage, 'getItem' | 'setItem'>;

export function loadOrder(storage: OrderStorage | undefined): NowOrder {
  try {
    return parseOrder(storage?.getItem(ORDER_STORAGE_KEY));
  } catch {
    return DEFAULT_ORDER; // storage can be blocked
  }
}

export function saveOrder(storage: OrderStorage | undefined, order: NowOrder): void {
  try {
    storage?.setItem(ORDER_STORAGE_KEY, order);
  } catch {
    // The choice still applies to this page view.
  }
}

/** What the order reads off an item: when it happened (none sorts last) and the handle of the actor it is about. */
export interface OrderKey {
  at?: string | undefined;
  by?: string | undefined;
}

function time(at: string | undefined): number {
  return at === undefined ? Number.NaN : Date.parse(at);
}

// An item without a time sits last whichever the direction; ties return 0 so they keep the order the server gave.
function byTime(a: OrderKey, b: OrderKey, newestFirst: boolean): number {
  const x = time(a.at);
  const y = time(b.at);
  if (Number.isNaN(x) || Number.isNaN(y)) return Number(Number.isNaN(x)) - Number(Number.isNaN(y));
  return newestFirst ? y - x : x - y;
}

function byHandle(a: OrderKey, b: OrderKey): number {
  const x = a.by ?? '';
  const y = b.by ?? '';
  if (x === '' || y === '') return Number(x === '') - Number(y === '');
  return x.localeCompare(y, undefined, { sensitivity: 'base' });
}

/** A sorted copy of the items in one band (or list). Stable: items that tie keep their input order. */
export function sortByOrder<T>(items: readonly T[], order: NowOrder, key: (item: T) => OrderKey): T[] {
  const compare = (a: T, b: T): number => {
    const x = key(a);
    const y = key(b);
    if (order === 'actor') return byHandle(x, y) || byTime(x, y, true);
    return byTime(x, y, order === 'newest');
  };
  return [...items].sort(compare);
}
