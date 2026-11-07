// The Edit task form's model (PRD R10): what the dialog collects, and the `PATCH /tasks/<ref>` body it becomes. Only the
// fields the person changed are sent, so a planned time that carries a time of day is not rewritten by an untouched date
// input, and the `task_updated` event names exactly what changed.
import { CADENCE_HINT, dayInstant, parseCadence } from './newTask';
import type { TaskDetail, TaskPatch } from './tasks';

export interface TaskEditForm {
  title: string;
  description: string;
  /** A date (`YYYY-MM-DD`) or empty. */
  plannedStart: string;
  plannedEnd: string;
  /** A watch task's cadence as typed (`15m`, `2h`, `1d`); empty for a work task. */
  cadence: string;
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
export function toTaskEditForm(task: Pick<TaskDetail, 'title' | 'description' | 'planned_start' | 'planned_end' | 'cadence_seconds'>): TaskEditForm {
  return {
    title: task.title,
    description: task.description,
    plannedStart: dayOf(task.planned_start),
    plannedEnd: dayOf(task.planned_end),
    cadence: cadenceInput(task.cadence_seconds),
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
 * time is `null` (which clears it). A watch cadence cannot be cleared: an emptied one, or one that is not a number and a
 * unit, is an error shown in the form.
 */
export function toTaskPatch(form: TaskEditForm, initial: TaskEditForm, kind: string): TaskPatchResult {
  const cadence = cadencePatch(form, initial, kind);
  return cadence.ok ? { ok: true, body: { ...fieldsPatch(form, initial), ...cadence.body } } : cadence;
}
