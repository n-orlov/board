// The Initiatives view's own state (PRD R16, SPEC 11): the status filter plus the table's sort, in one fragment query
// (`#/initiatives?status=closed&sort=title&dir=desc`), which the shared view-state helper (lib/viewState.ts) saves and
// restores. The filter is lib/initiativeFilters.ts, the sort is lib/tableSort.ts; this module joins the two. Sorting is by
// the view: the server keeps returning its own order, and the default order (attention first) is the one a sort replaces.
import { DEFAULT_INITIATIVE_FILTER, parseInitiativeFilter, type InitiativeFilter } from './initiativeFilters';
import { totalTasks } from './counts';
import type { InitiativeListItem } from './initiatives';
import { sortFromQuery, sortToQuery, type Columns, type SortState } from './tableSort';

/** The name of the view's saved state: the key is `board.view.v1.initiatives`. */
export const INITIATIVES_VIEW = 'initiatives';

/** The sortable columns of the table, by the name the `sort` query uses. Tags sorts by the tags as one text (they are kept sorted), none last; Tasks by the number of tasks in all. */
export const INITIATIVE_COLUMNS: Columns<InitiativeListItem> = {
  title: { type: 'text', value: (row) => row.title },
  status: { type: 'status', value: (row) => row.status },
  tags: { type: 'text', value: (row) => row.tags.join(', ') },
  tasks: { type: 'number', value: (row) => totalTasks(row.counts) },
  created: { type: 'time', value: (row) => row.created_at },
};

/** The column names, in the order of the table. */
export const INITIATIVE_SORT_COLUMNS: readonly string[] = Object.keys(INITIATIVE_COLUMNS);

/** The choices of the phone's Sort select: a label for each column. */
export const INITIATIVE_SORT_LABELS: readonly { column: string; label: string }[] = [
  { column: 'title', label: 'Title' },
  { column: 'status', label: 'Status' },
  { column: 'tags', label: 'Tags' },
  { column: 'tasks', label: 'Tasks' },
  { column: 'created', label: 'Created' },
];

/** Everything an Initiatives fragment holds. */
export interface InitiativesViewState {
  filter: InitiativeFilter;
  sort: SortState;
}

/** The state a query names: an unknown status or sort column is the default. */
export function viewFromQuery(query: string): InitiativesViewState {
  return { filter: parseInitiativeFilter(new URLSearchParams(query).get('status')), sort: sortFromQuery(query, INITIATIVE_SORT_COLUMNS) };
}

/** The query of a state: only what differs from the defaults, the status first, then `sort` and `dir`. */
export function viewToQuery(state: InitiativesViewState): string {
  const status = state.filter === DEFAULT_INITIATIVE_FILTER ? '' : new URLSearchParams({ status: state.filter }).toString();
  return [status, sortToQuery(state.sort)].filter((part) => part !== '').join('&');
}

/** The fragment of the view for a state: `#/initiatives`, or `#/initiatives?...`. */
export function viewHash(state: InitiativesViewState): string {
  const query = viewToQuery(state);
  return query === '' ? '#/initiatives' : `#/initiatives?${query}`;
}

/** Whether the state differs from the defaults, which is when Clear filters has something to clear. */
export function isCustomised(state: InitiativesViewState): boolean {
  return viewToQuery(state) !== '';
}
