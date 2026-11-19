// The Tasks view's own state (PRD R16, SPEC 11): the filters plus the table's sort, in one fragment query
// (`#/tasks?status=done&sort=ref&dir=desc`), which the shared view-state helper (lib/viewState.ts) saves and restores.
// Filters are lib/taskFilters.ts, the sort is lib/tableSort.ts; this module joins the two. Sorting is by the view:
// the server keeps returning its own order, and the Needs attention section never sorts.
import { sortFromQuery, sortToQuery, type Columns, type SortState } from './tableSort';
import { flagsText, type TaskRow } from './initiativePage';
import { filterFromQuery, filterToQuery, type TasksFilter } from './taskFilters';

/** The name of the view's saved state: the key is `vh.view.v1.tasks`. */
export const TASKS_VIEW = 'tasks';

/** The sortable columns of the table, by the name the `sort` query uses. Flags sorts by its badge labels (active, blocked, overdue), no flag last. */
export const TASK_COLUMNS: Columns<TaskRow> = {
  ref: { type: 'ref', value: (row) => row.ref },
  title: { type: 'text', value: (row) => row.title },
  initiative: { type: 'text', value: (row) => row.initiative },
  kind: { type: 'kind', value: (row) => row.kind },
  status: { type: 'status', value: (row) => row.status },
  flags: { type: 'text', value: (row) => flagsText(row) },
  holder: { type: 'text', value: (row) => row.holder },
};

/** The column names, in the order of the table. */
export const TASK_SORT_COLUMNS: readonly string[] = Object.keys(TASK_COLUMNS);

/** The choices of the shared sort control: a label for each column. */
export const TASK_SORT_LABELS: readonly { column: string; label: string }[] = [
  { column: 'ref', label: 'Ref' },
  { column: 'title', label: 'Title' },
  { column: 'initiative', label: 'Initiative' },
  { column: 'kind', label: 'Kind' },
  { column: 'status', label: 'Status' },
  { column: 'flags', label: 'Flags' },
  { column: 'holder', label: 'Lease holder' },
];

/** Everything a Tasks fragment holds. */
export interface TasksViewState {
  filter: TasksFilter;
  sort: SortState;
}

/** The state a query names: an unknown filter value or sort column is the default. */
export function viewFromQuery(query: string): TasksViewState {
  return { filter: filterFromQuery(query), sort: sortFromQuery(query, TASK_SORT_COLUMNS) };
}

/** The query of a state: only what differs from the defaults, filters first, then `sort` and `dir`. */
export function viewToQuery(state: TasksViewState): string {
  return [filterToQuery(state.filter), sortToQuery(state.sort)].filter((part) => part !== '').join('&');
}

/** The fragment of the view for a state: `#/tasks`, or `#/tasks?...`. */
export function viewHash(state: TasksViewState): string {
  const query = viewToQuery(state);
  return query === '' ? '#/tasks' : `#/tasks?${query}`;
}

/** The state a fragment holds: everything after the first `?` is the query. */
export function viewFromHash(hash: string): TasksViewState {
  const at = hash.indexOf('?');
  return viewFromQuery(at < 0 ? '' : hash.slice(at + 1));
}

/**
 * A restored state without the initiatives the board no longer has (deleted since it was saved): those chips go, so the table
 * is not an empty list under a filter nobody can see. The other values were already checked when the query was read.
 */
export function dropUnknownInitiative(state: TasksViewState, slugs: readonly string[]): TasksViewState {
  const kept = state.filter.initiatives.filter((slug) => slugs.includes(slug));
  if (kept.length === state.filter.initiatives.length) return state;
  return { ...state, filter: { ...state.filter, initiatives: kept } };
}
