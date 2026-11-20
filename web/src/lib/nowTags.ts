// The tag chips on Now (PRD R9, SPEC 11): the marks of one row, read from the board answer's `task_marks`.
// The Needs me items name their task only by the link (`#/t/<ref>`); In flight and Recently touched name it by ref.
import type { TagMark } from './chipFilter';

/** The ref a task link (`#/t/<ref>`) names; undefined for no link or any other fragment. */
export function taskOfHref(href: string | undefined): string | undefined {
  if (href === undefined || !href.startsWith('#/t/')) return undefined;
  try {
    return decodeURIComponent(href.slice(4));
  } catch {
    return undefined;
  }
}

/** The marks of the task a row is on: none for an item on no task, or while the read has not answered. */
export function marksOf(marks: Readonly<Record<string, readonly TagMark[]>>, ref: string | undefined): readonly TagMark[] {
  return ref === undefined ? [] : (marks[ref] ?? []);
}
