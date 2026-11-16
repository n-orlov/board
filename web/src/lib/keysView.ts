// The API keys table's sort (PRD R16, SPEC 11): the sortable columns, the phone's Sort choices and the fragment of the
// view (`#/settings/keys?sort=created&dir=desc`), which the shared view-state helper (lib/viewState.ts) saves and restores.
// The sort is lib/tableSort.ts; the list itself is lib/keys.ts. The table has no ref or kind column.
import type { Key } from './keys';
import { sortFromQuery, sortToQuery, type Columns, type SortState } from './tableSort';

/** The name of the view's saved state: the key is `vh.view.v1.keys`. */
export const KEYS_VIEW = 'keys';

/** The route of the view, without a query. */
export const KEYS_ROUTE = '#/settings/keys';

/**
 * The sortable columns of the table, by the name the `sort` query uses: the name and the prefix (text), the status, and the
 * three instants (time). A key that was never used or never expires has an empty cell, which sits last in both directions.
 */
export const KEY_COLUMNS: Columns<Key> = {
  name: { type: 'text', value: (key) => key.name },
  prefix: { type: 'text', value: (key) => key.display_prefix },
  status: { type: 'status', value: (key) => key.status },
  created: { type: 'time', value: (key) => key.created_at },
  used: { type: 'time', value: (key) => key.last_used_at },
  expires: { type: 'time', value: (key) => key.expires_at },
};

/** The column names, in the order of the table. */
export const KEY_SORT_COLUMNS: readonly string[] = Object.keys(KEY_COLUMNS);

/** The choices of the phone's Sort select: a label for each column. */
export const KEY_SORT_LABELS: readonly { column: string; label: string }[] = [
  { column: 'name', label: 'Name' },
  { column: 'prefix', label: 'Prefix' },
  { column: 'status', label: 'Status' },
  { column: 'created', label: 'Created' },
  { column: 'used', label: 'Last used' },
  { column: 'expires', label: 'Expires' },
];

/** The sort a query names: an unknown column or a bad `dir` is the default order (the server's). */
export function sortOfQuery(query: string): SortState {
  return sortFromQuery(query, KEY_SORT_COLUMNS);
}

/** The fragment of the view for a sort: `#/settings/keys`, or `#/settings/keys?sort=<column>&dir=<dir>`. */
export function keysHash(sort: SortState): string {
  const query = sortToQuery(sort);
  return query === '' ? KEYS_ROUTE : `${KEYS_ROUTE}?${query}`;
}

/** The query a fragment carries: everything after its first `?`. */
export function queryOfHash(hash: string): string {
  const at = hash.indexOf('?');
  return at < 0 ? '' : hash.slice(at + 1);
}

/** Whether a fragment is this view's, bare or with a query. */
export function isKeysHash(hash: string): boolean {
  return hash === KEYS_ROUTE || hash.startsWith(`${KEYS_ROUTE}?`);
}
