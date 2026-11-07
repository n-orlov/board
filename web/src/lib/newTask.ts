// The New task form's model (PRD R10): what the dialog collects, and the `POST /tasks` body it becomes. The dialog is
// opened from the Tasks view and from an initiative page, which presets the initiative.
import type { components } from './api/schema';

export type TaskCreate = components['schemas']['TaskCreate'];
export type ExternalRef = components['schemas']['ExternalRef'];

export interface NewTaskForm {
  /** The slug of the initiative the task goes in. */
  initiative: string;
  title: string;
  description: string;
  kind: 'work' | 'watch';
  /** A watch task's cadence as typed: a number and a unit, `15m`, `2h`, `1d`. */
  cadence: string;
  /** A date (`YYYY-MM-DD`) or empty. */
  plannedStart: string;
  plannedEnd: string;
  /** Refs of the open tasks the new one starts out blocked by. */
  blockedBy: string[];
  links: ExternalRef[];
  /** A link being typed: it counts as added when both parts are filled. */
  linkLabel: string;
  linkUrl: string;
}

/** A form with nothing typed, in `initiative` (empty when the person has to choose). */
export function emptyNewTask(initiative: string): NewTaskForm {
  return {
    initiative,
    title: '',
    description: '',
    kind: 'work',
    cadence: '',
    plannedStart: '',
    plannedEnd: '',
    blockedBy: [],
    links: [],
    linkLabel: '',
    linkUrl: '',
  };
}

const UNIT_SECONDS: Record<string, number> = { s: 1, m: 60, h: 3600, d: 86400 };

export const CADENCE_HINT = 'Cadence is a number and a unit, such as 15m, 2h or 1d.';

/** A cadence typed as `15m`, `2h`, `1d` or `90s` in seconds; undefined when it is not that shape. */
export function parseCadence(text: string): number | undefined {
  const match = /^(\d+)\s*([smhd])$/i.exec(text.trim());
  if (match === null) return undefined;
  return Number(match[1]) * UNIT_SECONDS[match[2]!.toLowerCase()]!;
}

/** The instant a planned date stands for: the start of that day in UTC. */
export function dayInstant(date: string): string {
  return `${date.trim()}T00:00:00Z`;
}

/** The link the form holds as typed, if any: undefined with both parts blank, null with only one filled. */
function draftLink(form: NewTaskForm): ExternalRef | null | undefined {
  const label = form.linkLabel.trim();
  const url = form.linkUrl.trim();
  if (label === '' && url === '') return undefined;
  return label === '' || url === '' ? null : { label, url };
}

export type TaskCreateResult = { ok: true; body: TaskCreate } | { ok: false; error: string };

function plannedFields(form: NewTaskForm): Pick<TaskCreate, 'planned_start' | 'planned_end'> {
  const fields: Pick<TaskCreate, 'planned_start' | 'planned_end'> = {};
  if (form.plannedStart.trim() !== '') fields.planned_start = dayInstant(form.plannedStart);
  if (form.plannedEnd.trim() !== '') fields.planned_end = dayInstant(form.plannedEnd);
  return fields;
}

/**
 * The request body for a form: text is trimmed, a blank optional field is left out, the cadence is sent for a watch task
 * only, and a link typed but not yet added is added. A cadence or link that cannot be sent is an error shown in the form.
 */
export function toTaskCreate(form: NewTaskForm): TaskCreateResult {
  const body: TaskCreate = { initiative: form.initiative, title: form.title.trim(), kind: form.kind, ...plannedFields(form) };
  const description = form.description.trim();
  if (description !== '') body.description = description;
  if (form.kind === 'watch') {
    const seconds = parseCadence(form.cadence);
    if (seconds === undefined) return { ok: false, error: CADENCE_HINT };
    body.cadence_seconds = seconds;
  }
  if (form.blockedBy.length > 0) body.blocked_by = [...form.blockedBy];
  const draft = draftLink(form);
  if (draft === null) return { ok: false, error: 'A link needs both a label and a URL.' };
  const links = draft === undefined ? form.links : [...form.links, draft];
  if (links.length > 0) body.external_refs = links.map((link) => ({ ...link }));
  return { ok: true, body };
}

/** The form with the draft link added to the list and the draft cleared; unchanged when the draft is not complete. */
export function addLink(form: NewTaskForm): NewTaskForm {
  const draft = draftLink(form);
  if (draft === undefined || draft === null) return form;
  return { ...form, links: [...form.links, draft], linkLabel: '', linkUrl: '' };
}

/** The form without the link at `index`. */
export function removeLink(form: NewTaskForm, index: number): NewTaskForm {
  return { ...form, links: form.links.filter((_, i) => i !== index) };
}

/** The form with `ref` added to the blockers (once) or, when it is already one, removed. */
export function toggleBlocker(form: NewTaskForm, ref: string): NewTaskForm {
  const has = form.blockedBy.includes(ref);
  return { ...form, blockedBy: has ? form.blockedBy.filter((r) => r !== ref) : [...form.blockedBy, ref] };
}

/** The open tasks the picker offers for the typed text, minus the chosen ones: ref or title contains it, ignoring case. */
export function pickerOptions<T extends { ref: string; title: string }>(tasks: readonly T[], text: string, chosen: readonly string[], limit = 8): T[] {
  const needle = text.trim().toLowerCase();
  return tasks
    .filter((task) => !chosen.includes(task.ref))
    .filter((task) => needle === '' || task.ref.toLowerCase().includes(needle) || task.title.toLowerCase().includes(needle))
    .slice(0, limit);
}
