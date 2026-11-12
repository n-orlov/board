// The filters of the Tasks view (PRD R10): status, kind, initiative, active, blocked, overdue and a text search.
// They live in the URL fragment's query (`#/tasks?status=done&kind=watch`) so a reload keeps them. This module
// turns the filter into that query and back, and into the query of `GET /tasks`.
import type { TaskFilter } from './initiativeFilters';

export type TaskStatusFilter = TaskFilter | 'open';
export type TaskKindFilter = '' | 'work' | 'watch';
/** A yes/no flag filter: '' keeps both, 'yes' keeps the tasks with the flag, 'no' those without. */
export type FlagFilter = '' | 'yes' | 'no';
export type FlagName = 'active' | 'blocked' | 'overdue' | 'attention';

export interface TasksFilter {
  status: TaskStatusFilter;
  kind: TaskKindFilter;
  /** An initiative slug; empty for every initiative. */
  initiative: string;
  active: FlagFilter;
  blocked: FlagFilter;
  overdue: FlagFilter;
  /** 'yes' keeps the tasks that need a person (stranded, asking, done and not closed), 'no' those that do not. */
  attention: FlagFilter;
  /** The text the title or description contains; empty for no search. */
  text: string;
}

export const DEFAULT_TASKS_FILTER: TasksFilter = {
  status: 'not-closed',
  kind: '',
  initiative: '',
  active: '',
  blocked: '',
  overdue: '',
  attention: '',
  text: '',
};

export const STATUS_OPTIONS: readonly { value: TaskStatusFilter; label: string }[] = [
  { value: 'not-closed', label: 'Not closed' },
  { value: 'open', label: 'Open' },
  { value: 'done', label: 'Done' },
  { value: 'closed', label: 'Closed' },
  { value: 'all', label: 'All' },
];

export const KIND_OPTIONS: readonly { value: TaskKindFilter; label: string }[] = [
  { value: '', label: 'Any kind' },
  { value: 'work', label: 'Work' },
  { value: 'watch', label: 'Watch' },
];

/** The options of the initiative select: any, then each slug (the one the URL names too, when the list lacks it), sorted. */
export function initiativeOptions(slugs: readonly string[], current: string): { value: string; label: string }[] {
  const all = new Set(slugs);
  if (current !== '') all.add(current);
  return [{ value: '', label: 'Any initiative' }, ...[...all].sort().map((slug) => ({ value: slug, label: slug }))];
}

/** The options of a flag select: `noun` is the flag's own word ("active"). */
export function flagOptions(noun: string): readonly { value: FlagFilter; label: string }[] {
  return [
    { value: '', label: `Any ${noun}` },
    { value: 'yes', label: noun[0]!.toUpperCase() + noun.slice(1) },
    { value: 'no', label: `Not ${noun}` },
  ];
}

/** The options of the attention select: the tasks that need a person, or the ones that do not. */
export const ATTENTION_OPTIONS: readonly { value: FlagFilter; label: string }[] = [
  { value: '', label: 'Any' },
  { value: 'yes', label: 'Needs attention' },
  { value: 'no', label: 'Needs nothing' },
];

const FLAGS: readonly FlagName[] = ['active', 'blocked', 'overdue', 'attention'];

function pick<T extends string>(allowed: readonly { value: T }[], raw: string | null, fallback: T): T {
  return allowed.find((o) => o.value === raw)?.value ?? fallback;
}

function flagFromParam(raw: string | null): FlagFilter {
  if (raw === 'true') return 'yes';
  return raw === 'false' ? 'no' : '';
}

function flagToParam(flag: FlagFilter): string {
  return flag === 'yes' ? 'true' : 'false';
}

/** The filter a query names; an absent or unknown value is the default for that field. */
export function filterFromQuery(query: string): TasksFilter {
  const params = new URLSearchParams(query);
  return {
    status: pick(STATUS_OPTIONS, params.get('status'), DEFAULT_TASKS_FILTER.status),
    kind: pick(KIND_OPTIONS, params.get('kind'), ''),
    initiative: params.get('initiative') ?? '',
    active: flagFromParam(params.get('active')),
    blocked: flagFromParam(params.get('blocked')),
    overdue: flagFromParam(params.get('overdue')),
    attention: flagFromParam(params.get('attention')),
    text: params.get('q') ?? '',
  };
}

/** The query of a filter for the URL fragment: only the fields that differ from the default, so the default is empty. */
export function filterToQuery(filter: TasksFilter): string {
  const params = new URLSearchParams();
  if (filter.status !== DEFAULT_TASKS_FILTER.status) params.set('status', filter.status);
  if (filter.kind !== '') params.set('kind', filter.kind);
  if (filter.initiative !== '') params.set('initiative', filter.initiative);
  for (const flag of FLAGS) {
    if (filter[flag] !== '') params.set(flag, flagToParam(filter[flag]));
  }
  if (filter.text !== '') params.set('q', filter.text);
  return params.toString();
}

/** The fragment of the Tasks view under a filter: `#/tasks`, or `#/tasks?...` when a field is set. */
export function tasksHash(filter: TasksFilter): string {
  const query = filterToQuery(filter);
  return query === '' ? '#/tasks' : `#/tasks?${query}`;
}

/** The filter held by a fragment: everything after the first `?` is the query. */
export function filterFromHash(hash: string): TasksFilter {
  const at = hash.indexOf('?');
  return filterFromQuery(at < 0 ? '' : hash.slice(at + 1));
}

/** The query of `GET /tasks` for a filter. The API spells the default status `not_closed` and always gets one. */
export function apiQuery(filter: TasksFilter): URLSearchParams {
  const params = new URLSearchParams({ status: filter.status === 'not-closed' ? 'not_closed' : filter.status });
  if (filter.kind !== '') params.set('kind', filter.kind);
  if (filter.initiative !== '') params.set('initiative', filter.initiative);
  for (const flag of FLAGS) {
    if (filter[flag] !== '') params.set(flag, flagToParam(filter[flag]));
  }
  if (filter.text !== '') params.set('q', filter.text);
  return params;
}

/** Whether any field differs from the default (the view offers "clear filters" then). */
export function isFiltered(filter: TasksFilter): boolean {
  return filterToQuery(filter) !== '';
}

/**
 * The query of the Needs attention section for a filter: the same filters with `attention=true` (the section then adds the
 * limit), or `null` when the filter keeps only tasks that need nothing, which the section cannot hold.
 */
export function needsFilterQuery(filter: TasksFilter): URLSearchParams | null {
  if (filter.attention === 'no') return null;
  const params = apiQuery(filter);
  params.set('attention', 'true');
  return params;
}

/** The fragment of the Tasks view filtered to the tasks that need attention (what `+K more` links to). */
export function attentionHref(filter: TasksFilter): string {
  return tasksHash({ ...filter, attention: 'yes' });
}
