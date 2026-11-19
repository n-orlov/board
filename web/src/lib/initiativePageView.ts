// The initiative page's own view state (PRD R16 and R7, SPEC 11): the Tasks table's status filter, its tag chips (tags only:
// every task here is in this initiative) and its sort, in one fragment query (`#/i/<slug>?status=done&tag=a&sort=ref&dir=desc`), which the shared view-state helper (lib/viewState.ts) saves and
// restores. Every initiative shares ONE saved key, `vh.view.v1.initiative`, so a sort chosen on one initiative is the
// sort another one opens with. The filter is lib/initiativeFilters.ts, the sort is lib/tableSort.ts; this module joins the
// two. Sorting is by the view: the page holds all of its tasks, and the Needs attention section above never sorts.
import { chipsFromQuery, mergeChoices, type TagChoice } from './chipFilter';
import { DEFAULT_TASK_FILTER, TASK_FILTERS, type TaskFilter } from './initiativeFilters';
import { flagsText, type TaskRow } from './initiativePage';
import { sortFromQuery, sortToQuery, type Columns, type SortState } from './tableSort';

/** The name of the view's saved state: the key is `vh.view.v1.initiative`, shared by every initiative. */
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

/** The options of the shared sort control: a label for each column. */
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
  /** The tags chosen: a task must carry every one (its own, its initiative's, or an actor kind). */
  tags: string[];
  sort: SortState;
}

/** The task filter a query names (`status=`); an unknown value is the default. */
function filterFromQuery(query: string): TaskFilter {
  const raw = new URLSearchParams(query).get('status');
  return TASK_FILTERS.find((option) => option.value === raw)?.value ?? DEFAULT_TASK_FILTER;
}

/** The state a query names: an unknown status or sort column is the default. */
export function viewFromQuery(query: string): PageViewState {
  return { filter: filterFromQuery(query), tags: chipsFromQuery(query).tags, sort: sortFromQuery(query, PAGE_TASK_SORT_COLUMNS) };
}

/** The query of a state: only what differs from the defaults, the status first, then each `tag`, then `sort` and `dir`. */
export function viewToQuery(state: PageViewState): string {
  const params = new URLSearchParams();
  if (state.filter !== DEFAULT_TASK_FILTER) params.set('status', state.filter);
  for (const tag of state.tags) params.append('tag', tag);
  return [params.toString(), sortToQuery(state.sort)].filter((part) => part !== '').join('&');
}

/** The rows that carry every tag chosen (all of them when none is chosen), in the order given. */
export function filterRowsByTags<T extends Pick<TaskRow, 'marks'>>(rows: readonly T[], tags: readonly string[]): T[] {
  return rows.filter((row) => tags.every((tag) => row.marks.some((mark) => mark.tag === tag)));
}

/** The tag chips on offer: every mark of the tasks read (whatever the filter), plus the chosen ones, sorted by tag. */
export function pageTagChoices(rows: readonly Pick<TaskRow, 'marks'>[], chosen: readonly string[]): TagChoice[] {
  return mergeChoices(
    [],
    rows.flatMap((row) => row.marks),
    chosen,
  );
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
