// The Tasks view's own state (PRD R16, SPEC 11): the filters plus the table's sort, in one fragment query
// (`#/tasks?status=done&sort=ref&dir=desc`), which the shared view-state helper (lib/viewState.ts) saves and restores.
// Filters are lib/taskFilters.ts, the sort is lib/tableSort.ts; this module joins the two. Sorting is by the view:
// the server keeps returning its own order, and the Needs attention section never sorts.
import { sortFromQuery, sortToQuery, type Columns, type SortState } from './tableSort';
import { flagsText, type TaskRow } from './initiativePage';
import { filterFromQuery, filterToQuery, type TasksFilter } from './taskFilters';
import type { TagMark } from './chipFilter';

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

const NO_TAGS: ReadonlySet<string> = new Set();

/**
 * The initiative source of each of `tags` some initiative of `known` lists, so a chosen chip names it even when no task of that
 * initiative is in the rows (or none exists): the tasks' marks give the other sources.
 */
export function listedMarks(tags: readonly string[], known: readonly { tags: readonly string[] }[]): TagMark[] {
  const listed = new Set(known.flatMap((i) => i.tags));
  return [...new Set(tags)].filter((tag) => listed.has(tag)).map((tag) => ({ tag, source: 'initiative' as const }));
}

/**
 * A restored state without the initiatives the board no longer has (deleted since it was saved) and without the tags nothing
 * carries any more: those chips go, so the table is not an empty list under a filter nobody can see. `known` is the board's
 * initiatives with their tags; `carried` the restored tags some task carries as its own or as a derived actor kind (or whose
 * check could not be made), which the initiatives do not list. The other values were already checked when the query was read.
 */
export function dropUnknown(
  state: TasksViewState,
  known: readonly { slug: string; tags: readonly string[] }[],
  carried: ReadonlySet<string> = NO_TAGS,
): TasksViewState {
  const slugs = new Set(known.map((i) => i.slug));
  const tags = new Set([...known.flatMap((i) => i.tags), ...carried]);
  const initiatives = state.filter.initiatives.filter((slug) => slugs.has(slug));
  const kept = state.filter.tags.filter((tag) => tags.has(tag));
  if (initiatives.length === state.filter.initiatives.length && kept.length === state.filter.tags.length) return state;
  return { ...state, filter: { ...state.filter, initiatives, tags: kept } };
}
