// The Initiatives view's own state (PRD R16 and R7, SPEC 11): the status filter, the tag chips (tags only: an initiative
// is not in an initiative) and the sort, in one fragment query (`#/initiatives?status=closed&tag=a&tag=b&sort=title&dir=desc`), which the shared view-state helper (lib/viewState.ts) saves and
// restores. The filter is lib/initiativeFilters.ts, the sort is lib/tableSort.ts; this module joins the two. Sorting is by
// the view: the server keeps returning its own order, and the default order (attention first) is the one a sort replaces.
import { DEFAULT_INITIATIVE_FILTER, parseInitiativeFilter, type InitiativeFilter } from './initiativeFilters';
import { totalTasks } from './counts';
import { chipsFromQuery, mergeChoices, type TagChoice } from './chipFilter';
import type { InitiativeListItem } from './initiatives';
import { sortFromQuery, sortToQuery, type Columns, type SortState } from './tableSort';

/** The name of the view's saved state: the key is `vh.view.v1.initiatives`. */
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

/** The options of the shared sort control: a label for each column. */
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
  /** The tags chosen: an initiative must carry every one. */
  tags: string[];
  sort: SortState;
}

/** The state a query names: an unknown status or sort column is the default. */
export function viewFromQuery(query: string): InitiativesViewState {
  return {
    filter: parseInitiativeFilter(new URLSearchParams(query).get('status')),
    tags: chipsFromQuery(query).tags,
    sort: sortFromQuery(query, INITIATIVE_SORT_COLUMNS),
  };
}

/** The query of a state: only what differs from the defaults, the status first, then each `tag`, then `sort` and `dir`. */
export function viewToQuery(state: InitiativesViewState): string {
  const params = new URLSearchParams();
  if (state.filter !== DEFAULT_INITIATIVE_FILTER) params.set('status', state.filter);
  for (const tag of state.tags) params.append('tag', tag);
  return [params.toString(), sortToQuery(state.sort)].filter((part) => part !== '').join('&');
}

/** The initiatives that carry every tag chosen (all of them when none is chosen), in the order given. */
export function filterByTags<T extends Pick<InitiativeListItem, 'tags'>>(rows: readonly T[], tags: readonly string[]): T[] {
  return rows.filter((row) => tags.every((tag) => row.tags.includes(tag)));
}

/** The tag chips on offer: every tag of the initiatives read, plus the chosen ones (a link may name a tag no row has), sorted. */
export function tagChoices(rows: readonly Pick<InitiativeListItem, 'tags'>[], chosen: readonly string[]): TagChoice[] {
  return mergeChoices(
    [],
    rows.flatMap((row) => row.tags.map((tag) => ({ tag, source: 'initiative' as const }))),
    chosen,
  );
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
