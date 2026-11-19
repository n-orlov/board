// The shared sort control (PRD R7, SPEC 11): one select, shown at every width, that offers the default order and each
// sortable column ascending and descending. A page gives its columns; this module gives the options, reads the choice back
// and keeps the order in the fragment's `sort=<column>&dir=asc|desc` query, which the view's saved query holds too. Built
// on lib/tableSort.ts (the comparators and the cycle), so the other pages can adopt it by passing their own columns.
import { choiceOf, sortChoices, sortFromQuery, sortOfChoice, sortToQuery, type SortChoice, type SortState } from './tableSort';

/** A sortable column and the words the control uses for it. */
export interface SortColumn {
  column: string;
  label: string;
}

/** The options of the control: the default order first, then each column ascending and descending. */
export function sortOptions(columns: readonly SortColumn[]): SortChoice[] {
  return sortChoices(columns);
}

/** The option value that shows `sort`: the empty string for the default order. */
export function optionValue(sort: SortState): string {
  return choiceOf(sort);
}

/** The sort an option value chooses. A value naming a column the page does not have is the default order. */
export function pickOption(value: string, columns: readonly SortColumn[]): SortState {
  const sort = sortOfChoice(value);
  return sort !== null && columns.some((c) => c.column === sort.column) ? sort : null;
}

/** The label of the chosen option, for a screen reader's status line. */
export function optionLabel(sort: SortState, columns: readonly SortColumn[]): string {
  const value = optionValue(sort);
  return sortOptions(columns).find((o) => o.value === value)?.label ?? 'Default order';
}

/** The sort part of the fragment: `sort=ref&dir=desc`, empty for the default order. */
export function sortFragment(sort: SortState): string {
  return sortToQuery(sort);
}

/** The sort a fragment query names, or the default order when it names none or an unknown column. */
export function sortFromFragment(query: string, columns: readonly SortColumn[]): SortState {
  return sortFromQuery(
    query,
    columns.map((c) => c.column),
  );
}

/** A query with its `sort` and `dir` replaced by `sort`'s: what a view saves after the control changes. */
export function withSort(query: string, sort: SortState): string {
  const params = new URLSearchParams(query);
  params.delete('sort');
  params.delete('dir');
  const rest = params.toString();
  return [rest, sortFragment(sort)].filter((part) => part !== '').join('&');
}
