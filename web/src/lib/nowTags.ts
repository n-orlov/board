// The tag chips on Now (PRD R9, SPEC 11): which tasks the view shows, so one read can fetch their marks, and the marks of one row.
// The Needs me items name their task only by the link (`#/t/<ref>`); In flight and Recently touched name it by ref.
import type { TagMark } from './chipFilter';
import { BANDS, bandRows } from './nowBands';
import type { Attention } from './attentionView';

/** The ref a task link (`#/t/<ref>`) names; undefined for no link or any other fragment. */
export function taskOfHref(href: string | undefined): string | undefined {
  if (href === undefined || !href.startsWith('#/t/')) return undefined;
  try {
    return decodeURIComponent(href.slice(4));
  } catch {
    return undefined;
  }
}

/** The refs of every task Now shows: those its Needs me items are on, then In flight, then Recently touched; each once. */
export function nowTaskRefs(attention: Attention | undefined, flight: readonly string[], recent: readonly string[], now: Date): string[] {
  const needs = attention === undefined ? [] : BANDS.flatMap((band) => bandRows(band, attention, now).map(({ row }) => taskOfHref(row.href)));
  return [...new Set([...needs, ...flight, ...recent].filter((ref): ref is string => ref !== undefined))];
}

/** The marks of the task a row is on: none for an item on no task, or while the read has not answered. */
export function marksOf(marks: Readonly<Record<string, readonly TagMark[]>>, ref: string | undefined): readonly TagMark[] {
  return ref === undefined ? [] : (marks[ref] ?? []);
}
