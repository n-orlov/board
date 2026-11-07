// The Edit task form's model (PRD R10): what the dialog collects, and the `PATCH /tasks/<ref>` body it becomes. Only the
// fields the person changed are sent, so a planned time that carries a time of day is not rewritten by an untouched date
// input, and the `task_updated` event names exactly what changed.
import { CADENCE_HINT, dayInstant, parseCadence } from './newTask';
import type { ExternalRef } from './newTask';
import type { TaskDetail, TaskPatch } from './tasks';

/** The most links a task holds (the API refuses more). */
export const MAX_LINKS = 20;

export interface TaskEditForm {
  title: string;
  description: string;
  /** A date (`YYYY-MM-DD`) or empty. */
  plannedStart: string;
  plannedEnd: string;
  /** A watch task's cadence as typed (`15m`, `2h`, `1d`); empty for a work task. */
  cadence: string;
  /** A watch task's next check as `YYYY-MM-DDTHH:MM` (UTC) or empty; empty for a work task. */
  nextCheck: string;
  /** The links as they will be saved: the whole list replaces the old one. */
  links: ExternalRef[];
  /** A link being typed: it counts as added when both parts are filled. */
  linkLabel: string;
  linkUrl: string;
}

/** The minute of an instant as a `datetime-local` value in UTC (`2026-03-01T10:20:30Z` gives `2026-03-01T10:20`). */
export function minuteOf(instant: string | undefined): string {
  return instant === undefined ? '' : instant.slice(0, 16);
}

/** The date part of an instant (`2026-03-01T00:00:00Z` gives `2026-03-01`); empty when there is none. */
export function dayOf(instant: string | undefined): string {
  return instant === undefined ? '' : instant.slice(0, 10);
}

/** A cadence in seconds as the text the form takes back: the largest of d, h, m, s that divides it. */
export function cadenceInput(seconds: number | undefined): string {
  if (seconds === undefined) return '';
  const units: [number, string][] = [[86400, 'd'], [3600, 'h'], [60, 'm']];
  for (const [size, unit] of units) {
    if (seconds >= size && seconds % size === 0) return `${seconds / size}${unit}`;
  }
  return `${seconds}s`;
}

/** The edit form as typed for a task as it is now. */
export function toTaskEditForm(
  task: Pick<TaskDetail, 'title' | 'description'> & Partial<Pick<TaskDetail, 'planned_start' | 'planned_end' | 'cadence_seconds' | 'next_check_at' | 'external_refs'>>,
): TaskEditForm {
  return {
    title: task.title,
    description: task.description,
    plannedStart: dayOf(task.planned_start),
    plannedEnd: dayOf(task.planned_end),
    cadence: cadenceInput(task.cadence_seconds),
    nextCheck: minuteOf(task.next_check_at),
    links: (task.external_refs ?? []).map((link) => ({ ...link })),
    linkLabel: '',
    linkUrl: '',
  };
}

/** A watch task's cadence cannot be cleared; an emptied field is refused in the form. */
export const CADENCE_REQUIRED = 'A watch task needs a cadence: a number and a unit, such as 15m, 2h or 1d.';

export type TaskPatchResult = { ok: true; body: TaskPatch } | { ok: false; error: string };

/** A planned time: the start of the day typed, or `null` (clear it) when the field was emptied. */
function planned(date: string): string | null {
  const day = date.trim();
  return day === '' ? null : dayInstant(day);
}

function cadencePatch(form: TaskEditForm, initial: TaskEditForm, kind: string): TaskPatchResult {
  const text = form.cadence.trim();
  if (kind !== 'watch' || text === initial.cadence.trim()) return { ok: true, body: {} };
  if (text === '') return { ok: false, error: CADENCE_REQUIRED };
  const seconds = parseCadence(text);
  return seconds === undefined ? { ok: false, error: CADENCE_HINT } : { ok: true, body: { cadence_seconds: seconds } };
}

export const NEXT_CHECK_HINT = 'The next check is a date and a time, such as 2026-03-01 10:20 (UTC).';

const MINUTE_SHAPE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/;

/** A watch task's next check: sent when it changed, `null` (clear it) when emptied; a work task has none. */
function nextCheckPatch(form: TaskEditForm, initial: TaskEditForm, kind: string): TaskPatchResult {
  const text = form.nextCheck.trim();
  if (kind !== 'watch' || text === initial.nextCheck) return { ok: true, body: {} };
  if (text === '') return { ok: true, body: { next_check_at: null } };
  return MINUTE_SHAPE.test(text) ? { ok: true, body: { next_check_at: `${text}:00Z` } } : { ok: false, error: NEXT_CHECK_HINT };
}

/** The link typed but not yet added: undefined with both parts blank, null with only one filled. */
function draftLink(form: Pick<TaskEditForm, 'linkLabel' | 'linkUrl'>): ExternalRef | null | undefined {
  const label = form.linkLabel.trim();
  const url = form.linkUrl.trim();
  if (label === '' && url === '') return undefined;
  return label === '' || url === '' ? null : { label, url };
}

/** The form with the typed link added to the list and the draft cleared; unchanged when the draft is not complete. */
export function addEditLink(form: TaskEditForm): TaskEditForm {
  const draft = draftLink(form);
  if (draft === undefined || draft === null) return form;
  return { ...form, links: [...form.links, draft], linkLabel: '', linkUrl: '' };
}

/** The form without the link at `index`. */
export function removeEditLink(form: TaskEditForm, index: number): TaskEditForm {
  return { ...form, links: form.links.filter((_, i) => i !== index) };
}

export const LINK_INCOMPLETE = 'A link needs both a label and a URL.';
export const TOO_MANY_LINKS = `A task holds at most ${MAX_LINKS} links.`;

const sameLinks = (a: readonly ExternalRef[], b: readonly ExternalRef[]) => a.length === b.length && a.every((link, i) => link.label === b[i]!.label && link.url === b[i]!.url);

/** The links: the list as typed (a draft counts as added, labels and URLs trimmed), sent whole when it differs from before. */
function linksPatch(form: TaskEditForm, initial: TaskEditForm): TaskPatchResult {
  const draft = draftLink(form);
  if (draft === null) return { ok: false, error: LINK_INCOMPLETE };
  const typed = [...form.links, ...(draft === undefined ? [] : [draft])].map((link) => ({ label: link.label.trim(), url: link.url.trim() }));
  if (typed.length > MAX_LINKS) return { ok: false, error: TOO_MANY_LINKS };
  return sameLinks(typed, initial.links) ? { ok: true, body: {} } : { ok: true, body: { external_refs: typed } };
}

function fieldsPatch(form: TaskEditForm, initial: TaskEditForm): TaskPatch {
  const body: TaskPatch = {};
  if (form.title.trim() !== initial.title.trim()) body.title = form.title.trim();
  if (form.description.trim() !== initial.description.trim()) body.description = form.description.trim();
  if (form.plannedStart.trim() !== initial.plannedStart) body.planned_start = planned(form.plannedStart);
  if (form.plannedEnd.trim() !== initial.plannedEnd) body.planned_end = planned(form.plannedEnd);
  return body;
}

/**
 * The patch that turns `initial` into `form`: text is trimmed, a field that did not change is left out, an emptied planned
 * time or next check is `null` (which clears it), and the links are sent whole when they differ. A watch cadence cannot be
 * cleared: an emptied one, or one that is not a number and a unit, is an error shown in the form; so is a next check
 * that is not a date and a time, and a link with only one of its two parts.
 */
export function toTaskPatch(form: TaskEditForm, initial: TaskEditForm, kind: string): TaskPatchResult {
  let body: TaskPatch = fieldsPatch(form, initial);
  for (const part of [cadencePatch(form, initial, kind), nextCheckPatch(form, initial, kind), linksPatch(form, initial)]) {
    if (!part.ok) return part;
    body = { ...body, ...part.body };
  }
  return { ok: true, body };
}
