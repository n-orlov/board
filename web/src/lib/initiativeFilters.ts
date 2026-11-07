// The filters of the initiative views (PRD R9): the Initiatives list filters by status (open / closed / all) and
// the initiative page filters its tasks (not closed / done / closed / all). Each filter is a closed set of values
// with a label for the select, a parser for a value read from the URL, and the status it asks the API for.
import type { components } from './api/schema';

export type InitiativeFilter = 'open' | 'closed' | 'all';
export type TaskFilter = 'not-closed' | 'done' | 'closed' | 'all';
export type TaskStatus = keyof components['schemas']['TaskCounts'];

export interface FilterOption<T extends string> {
  value: T;
  label: string;
}

export const INITIATIVE_FILTERS: readonly FilterOption<InitiativeFilter>[] = [
  { value: 'open', label: 'Open' },
  { value: 'closed', label: 'Closed' },
  { value: 'all', label: 'All' },
];

export const TASK_FILTERS: readonly FilterOption<TaskFilter>[] = [
  { value: 'not-closed', label: 'Not closed' },
  { value: 'done', label: 'Done' },
  { value: 'closed', label: 'Closed' },
  { value: 'all', label: 'All' },
];

/** The initiative list's filter before anyone chooses: the API's own default. */
export const DEFAULT_INITIATIVE_FILTER: InitiativeFilter = 'open';
/** The initiative page's task filter before anyone chooses: the work that is still live. */
export const DEFAULT_TASK_FILTER: TaskFilter = 'not-closed';

function parse<T extends string>(options: readonly FilterOption<T>[], raw: string | null | undefined, fallback: T): T {
  return options.find((o) => o.value === raw)?.value ?? fallback;
}

/** The initiative filter a raw value names; anything unknown is the default. */
export function parseInitiativeFilter(raw: string | null | undefined): InitiativeFilter {
  return parse(INITIATIVE_FILTERS, raw, DEFAULT_INITIATIVE_FILTER);
}

/** The task filter a raw value names; anything unknown is the default. */
export function parseTaskFilter(raw: string | null | undefined): TaskFilter {
  return parse(TASK_FILTERS, raw, DEFAULT_TASK_FILTER);
}

/** The `status` query value of `GET /initiatives` for a filter (the API accepts open, closed and all). */
export function initiativeStatusParam(filter: InitiativeFilter): string {
  return filter;
}

const TASK_STATUSES: Record<TaskFilter, readonly TaskStatus[]> = {
  'not-closed': ['open', 'done'],
  done: ['done'],
  closed: ['closed'],
  all: ['open', 'done', 'closed'],
};

/** Whether an initiative of this status is shown under the filter. */
export function showsInitiative(filter: InitiativeFilter, status: string): boolean {
  return filter === 'all' || filter === status;
}

/** Whether a task of this status is shown under the filter. */
export function showsTask(filter: TaskFilter, status: string): boolean {
  return (TASK_STATUSES[filter] as readonly string[]).includes(status);
}

/** The items a task filter keeps, in order. */
export function filterTasks<T extends { status: string }>(tasks: readonly T[], filter: TaskFilter): T[] {
  return tasks.filter((task) => showsTask(filter, task.status));
}
