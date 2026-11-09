// The Attention view's presentation and filters (PRD R9, SPEC 5.3 and 11): the five sections in order with their
// empty states, the age of an ask, the asker-waiting label, and the tag/initiative filter's encoding in the URL
// fragment's query (`#/attention?tag=work&tag=home&initiative=aa`) and in the query of `GET /attention`.
import type { components } from './api/schema';

export type AttentionCounts = components['schemas']['AttentionCounts'];
export type SectionKey = 'open_asks' | 'done_not_closed' | 'expired_leases' | 'overdue_watches' | 'conflicts';

export interface SectionInfo {
  key: SectionKey;
  title: string;
  /** What the section says when it has no items. */
  empty: string;
}

/** The five sections in SPEC 5.3 order, each with its calm empty state. */
export const SECTIONS: readonly SectionInfo[] = [
  { key: 'open_asks', title: 'Open asks', empty: 'No open asks. Nobody is waiting on an answer.' },
  { key: 'done_not_closed', title: 'Done, not closed', empty: 'Nothing is waiting to be closed.' },
  { key: 'expired_leases', title: 'Expired leases', empty: 'No expired leases to acknowledge.' },
  { key: 'overdue_watches', title: 'Overdue watches', empty: 'No watch is overdue.' },
  { key: 'conflicts', title: 'Conflicts', empty: 'No conflicts. Every resource has one holder at most.' },
];

/** The empty-state text of a section. */
export function emptyText(key: SectionKey): string {
  return SECTIONS.find((s) => s.key === key)!.empty;
}

/** The count a section shows in its heading and the nav badge shows as the total. */
export function sectionCount(counts: AttentionCounts, key: SectionKey): number {
  return counts[key];
}

const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

function ago(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? '' : 's'} ago`;
}

/** How long ago an ask was raised, from its `age_seconds`: "just now" (under a minute), "5 minutes ago", "3 hours ago", "2 days ago". */
export function ageText(seconds: number): string {
  if (seconds < MINUTE) return 'just now';
  if (seconds < HOUR) return ago(Math.floor(seconds / MINUTE), 'minute');
  if (seconds < DAY) return ago(Math.floor(seconds / HOUR), 'hour');
  return ago(Math.floor(seconds / DAY), 'day');
}

/** Whether the asker of an open ask is still waiting for the answer (a `poll` ask whose poll is in flight or just ended). */
export function askerWaitingLabel(waiting: boolean): string {
  return waiting ? 'asker waiting' : 'asker not waiting';
}

/** The filter of the view: the initiatives and the tags to keep; each list is empty for no filter on it. */
export interface AttentionFilter {
  /** Initiative slugs, any of them. */
  initiatives: string[];
  /** Tags, all of them (SPEC 5.3: the initiative carries all of them). */
  tags: string[];
}

export const DEFAULT_ATTENTION_FILTER: AttentionFilter = { initiatives: [], tags: [] };

/** A list without blanks (after trimming) or repeats, in first-seen order. */
function clean(values: readonly string[]): string[] {
  return [...new Set(values.map((v) => v.trim()).filter((v) => v !== ''))];
}

/** The filter a query names; repeated `tag` and `initiative` keys each add one value. */
export function filterFromQuery(query: string): AttentionFilter {
  const params = new URLSearchParams(query);
  return { initiatives: clean(params.getAll('initiative')), tags: clean(params.getAll('tag')) };
}

function repeated(params: URLSearchParams, name: string, values: readonly string[]): void {
  for (const value of clean(values)) params.append(name, value);
}

/** The query of a filter for the URL fragment: `tag` keys, then `initiative` keys, so the default is empty. */
export function filterToQuery(filter: AttentionFilter): string {
  const params = new URLSearchParams();
  repeated(params, 'tag', filter.tags);
  repeated(params, 'initiative', filter.initiatives);
  return params.toString();
}

/** The fragment of the view under a filter: `#/attention`, or `#/attention?...` when a value is set. */
export function attentionHash(filter: AttentionFilter): string {
  const query = filterToQuery(filter);
  return query === '' ? '#/attention' : `#/attention?${query}`;
}

/** The filter held by a fragment: everything after the first `?` is the query. */
export function filterFromHash(hash: string): AttentionFilter {
  const at = hash.indexOf('?');
  return filterFromQuery(at < 0 ? '' : hash.slice(at + 1));
}

/** The query of `GET /attention` for a filter: the same repeated `tag` and `initiative` keys. */
export function apiQuery(filter: AttentionFilter): URLSearchParams {
  return new URLSearchParams(filterToQuery(filter));
}

/** Whether any value is set (the view offers "clear filters" then). */
export function isFiltered(filter: AttentionFilter): boolean {
  return clean(filter.tags).length + clean(filter.initiatives).length > 0;
}

/** The list with `value` added when absent, or removed when present (a tag chip toggles). */
export function toggled(values: readonly string[], value: string): string[] {
  return values.includes(value) ? values.filter((v) => v !== value) : [...values, value];
}
