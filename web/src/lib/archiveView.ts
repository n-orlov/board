// The Archive view's own state (PRD R16, SPEC 11): the closed-within window plus the table's sort, in one fragment query
// (`#/archive?window=7&sort=closed&dir=desc`), which the shared view-state helper (lib/viewState.ts) saves and restores. The
// window is lib/archive.ts, the sort is lib/tableSort.ts; this module joins the two. Sorting is by the view: the lists are
// read once per window, and the default order (newest closed first) is the one a sort replaces.
import { windowFromQuery, windowToQuery, type ArchiveRow, type ArchiveWindow } from './archive';
import { sortFromQuery, sortToQuery, type Columns, type SortState } from './tableSort';

/** The name of the view's saved state: the key is `board.view.v1.archive`. */
export const ARCHIVE_VIEW = 'archive';

/**
 * The sortable columns of the table, by the name the `sort` query uses: the title (text), the kind (task or initiative), the
 * resolution (status) and when it was closed (time). The Action column holds no value.
 */
export const ARCHIVE_COLUMNS: Columns<ArchiveRow> = {
  item: { type: 'text', value: (row) => row.title },
  kind: { type: 'kind', value: (row) => row.kind },
  resolution: { type: 'status', value: (row) => row.resolution },
  closed: { type: 'time', value: (row) => row.closedAt },
};

/** The column names, in the order of the table. */
export const ARCHIVE_SORT_COLUMNS: readonly string[] = Object.keys(ARCHIVE_COLUMNS);

/** The choices of the phone's Sort select: a label for each column. */
export const ARCHIVE_SORT_LABELS: readonly { column: string; label: string }[] = [
  { column: 'item', label: 'Item' },
  { column: 'kind', label: 'Kind' },
  { column: 'resolution', label: 'Resolution' },
  { column: 'closed', label: 'Closed' },
];

/** Everything an Archive fragment holds. */
export interface ArchiveViewState {
  window: ArchiveWindow;
  sort: SortState;
}

/** The state a query names: an unknown window is 30 days, an unknown sort column or a bad direction is the default order. */
export function viewFromQuery(query: string): ArchiveViewState {
  return { window: windowFromQuery(query), sort: sortFromQuery(query, ARCHIVE_SORT_COLUMNS) };
}

/** The query of a state: only what differs from the defaults, the window first, then `sort` and `dir`. */
export function viewToQuery(state: ArchiveViewState): string {
  return [windowToQuery(state.window), sortToQuery(state.sort)].filter((part) => part !== '').join('&');
}

/** The fragment of the view for a state: `#/archive`, or `#/archive?...`. */
export function archiveViewHash(state: ArchiveViewState): string {
  const query = viewToQuery(state);
  return query === '' ? '#/archive' : `#/archive?${query}`;
}

/** The query a fragment carries: everything after its first `?`. */
export function queryOfHash(hash: string): string {
  const at = hash.indexOf('?');
  return at < 0 ? '' : hash.slice(at + 1);
}
