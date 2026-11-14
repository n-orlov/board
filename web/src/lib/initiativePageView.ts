// The initiative page's own view state (PRD R16, SPEC 11): the Tasks table's status filter plus its sort, in one fragment
// query (`#/i/<slug>?status=done&sort=ref&dir=desc`), which the shared view-state helper (lib/viewState.ts) saves and
// restores. Every initiative shares ONE saved key, `board.view.v1.initiative`, so a sort chosen on one initiative is the
// sort another one opens with. The filter is lib/initiativeFilters.ts, the sort is lib/tableSort.ts; this module joins the
// two. Sorting is by the view: the page holds all of its tasks, and the Needs attention section above never sorts.
import { DEFAULT_TASK_FILTER, TASK_FILTERS, type TaskFilter } from './initiativeFilters';
import { flagsText, type TaskRow } from './initiativePage';
import { sortFromQuery, sortToQuery, type Columns, type SortState } from './tableSort';

/** The name of the view's saved state: the key is `board.view.v1.initiative`, shared by every initiative. */
export const INITIATIVE_VIEW = 'initiative';

/** The sortable columns of the Tasks table, by the name the `sort` query uses. Flags sorts by its badge labels, no flag last. */
export const PAGE_TASK_COLUMNS: Columns<TaskRow> = {
  ref: { type: 'ref', value: (row) => row.ref },
  title: { type: 'text', value: (row) => row.title },
  kind: { type: 'kind', value: (row) => row.kind },
  status: { type: 'status', value: (row) => row.status },
  flags: { type: 'text', value: (row) => flagsText(row) },
  holder: { type: 'text', value: (row) => row.holder },
};

/** The column names, in the order of the table. */
export const PAGE_TASK_SORT_COLUMNS: readonly string[] = Object.keys(PAGE_TASK_COLUMNS);

/** The choices of the phone's Sort select: a label for each column. */
export const PAGE_TASK_SORT_LABELS: readonly { column: string; label: string }[] = [
  { column: 'ref', label: 'Ref' },
  { column: 'title', label: 'Title' },
  { column: 'kind', label: 'Kind' },
  { column: 'status', label: 'Status' },
  { column: 'flags', label: 'Flags' },
  { column: 'holder', label: 'Lease holder' },
];

/** Everything an initiative page's fragment holds. */
export interface PageViewState {
  filter: TaskFilter;
  sort: SortState;
}

/** The task filter a query names (`status=`); an unknown value is the default. */
function filterFromQuery(query: string): TaskFilter {
  const raw = new URLSearchParams(query).get('status');
  return TASK_FILTERS.find((option) => option.value === raw)?.value ?? DEFAULT_TASK_FILTER;
}

/** The state a query names: an unknown status or sort column is the default. */
export function viewFromQuery(query: string): PageViewState {
  return { filter: filterFromQuery(query), sort: sortFromQuery(query, PAGE_TASK_SORT_COLUMNS) };
}

/** The query of a state: only what differs from the defaults, the status first, then `sort` and `dir`. */
export function viewToQuery(state: PageViewState): string {
  const status = state.filter === DEFAULT_TASK_FILTER ? '' : new URLSearchParams({ status: state.filter }).toString();
  return [status, sortToQuery(state.sort)].filter((part) => part !== '').join('&');
}

/** The path of an initiative's page: `#/i/<slug>`. */
export function pagePath(slug: string): string {
  return `#/i/${slug}`;
}

/** The fragment of the page of `slug` for a state: `#/i/<slug>`, or `#/i/<slug>?...`. */
export function viewHash(slug: string, state: PageViewState): string {
  const query = viewToQuery(state);
  return query === '' ? pagePath(slug) : `${pagePath(slug)}?${query}`;
}

/** Whether the state differs from the defaults, which is when Clear filters has something to clear. */
export function isCustomised(state: PageViewState): boolean {
  return viewToQuery(state) !== '';
}

/** Whether a fragment is the page of `slug` (with or without a query): the page ignores the hash changes of other views. */
export function isPageHash(slug: string, hash: string): boolean {
  const path = pagePath(slug);
  return hash === path || hash.startsWith(`${path}?`);
}
