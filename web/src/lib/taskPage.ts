// The task page's data layer and presentation (PRD R10, SPEC 11): `TaskPageStore` loads one task and its events and runs
// the page's writes (Edit, Done, Reopen, Close); the functions turn the task into what the page prints (planned window,
// live leases with "expires in", the event timeline newest first).
import { cadenceText, expiresIn } from './durations';
import { errorText } from './fetches';
import { absoluteTime, relativeTime } from './format';
import { toTaskPatch, type TaskEditForm } from './taskEdit';
import { targetsText } from './initiativePage';
import type { Resolution, TaskDetail, TasksApi, TaskEvent } from './tasks';

export const RESOLUTIONS: readonly { value: Resolution; label: string }[] = [
  { value: 'completed', label: 'Completed' },
  { value: 'dropped', label: 'Dropped' },
  { value: 'superseded', label: 'Superseded' },
  { value: 'duplicate', label: 'Duplicate' },
];

/** The planned window: "2026-01-01 to 2026-02-01", "from 2026-01-01", "until 2026-02-01"; empty when none is set. */
export function plannedText(task: Pick<TaskDetail, 'planned_start' | 'planned_end'>): string {
  return targetsText({ target_start: task.planned_start?.slice(0, 10), target_end: task.planned_end?.slice(0, 10) });
}

/** What a task's kind says on the page: "work", or "watch, every 5 minutes". */
export function kindText(task: Pick<TaskDetail, 'kind' | 'cadence_seconds'>): string {
  const cadence = cadenceText(task.cadence_seconds);
  return cadence === '' ? task.kind : `${task.kind}, ${cadence}`;
}

/** The address of a task's page. */
export function taskHref(ref: string): string {
  return `#/t/${ref}`;
}

export interface LeaseRow {
  ref: string;
  holder: string;
  intent: string;
  resources: string;
  expires: string;
}

/** One row per live lease of the task: who holds it, why, which resources, and when it runs out. */
export function leaseRows(task: Pick<TaskDetail, 'live_leases'>, now: Date): LeaseRow[] {
  return task.live_leases.map((lease) => ({
    ref: lease.ref,
    holder: lease.actor,
    intent: lease.intent,
    resources: lease.resources.join(', '),
    expires: expiresIn(lease.expires_at, now),
  }));
}

export interface EventRow {
  id: string;
  ref: string;
  type: string;
  summary: string;
  actor: string;
  at: string;
  when: string;
  title: string;
}

/** The timeline: the events newest first (the API gives them in the order they were written). */
export function eventRows(events: readonly TaskEvent[], now: Date): EventRow[] {
  return events
    .map((event) => ({
      id: event.id,
      ref: event.ref,
      type: event.type,
      summary: event.summary,
      actor: event.actor,
      at: event.at,
      when: relativeTime(event.at, now),
      title: absoluteTime(event.at),
    }))
    .reverse();
}

export interface TaskPageState {
  task: TaskDetail | undefined;
  /** In the order they were written. */
  events: TaskEvent[];
  loading: boolean;
  /** Why the page could not be read. */
  error: string | undefined;
  /** Why the last action was refused; shown inline (inside the dialog when one is open). */
  actionError: string | undefined;
}

type PageApi = Pick<TasksApi, 'get' | 'events' | 'patch' | 'done' | 'reopen' | 'close'>;

export class TaskPageStore {
  private readonly api: PageApi;
  private readonly onChange: (state: TaskPageState) => void;
  private current: TaskPageState = { task: undefined, events: [], loading: true, error: undefined, actionError: undefined };

  constructor(api: PageApi, onChange: (state: TaskPageState) => void) {
    this.api = api;
    this.onChange = onChange;
  }

  get state(): TaskPageState {
    return this.current;
  }

  private set(patch: Partial<TaskPageState>): void {
    this.current = { ...this.current, ...patch };
    this.onChange(this.current);
  }

  /** Reads the task and its events; either failing shows one error and no half page. */
  async load(ref: string): Promise<void> {
    this.set({ loading: true, actionError: undefined });
    await this.read(ref);
  }

  private async read(ref: string): Promise<void> {
    try {
      const [task, events] = await Promise.all([this.api.get(ref), this.api.events(ref)]);
      this.set({ task, events, loading: false, error: undefined });
    } catch (e) {
      this.set({ task: undefined, events: [], loading: false, error: errorText(e) });
    }
  }

  /** Runs a write; on success the page is read again (without the skeleton), on a refusal `actionError` says why. Whether it worked. */
  private async write(ref: string, action: () => Promise<unknown>): Promise<boolean> {
    try {
      await action();
    } catch (e) {
      this.set({ actionError: errorText(e) });
      return false;
    }
    this.set({ actionError: undefined });
    await this.read(ref);
    return true;
  }

  /** Saves the edit form against the task as it was shown; a form that cannot be sent is a refusal too. */
  edit(ref: string, form: TaskEditForm, initial: TaskEditForm, kind: string): Promise<boolean> {
    const patch = toTaskPatch(form, initial, kind);
    if (!patch.ok) {
      this.set({ actionError: patch.error });
      return Promise.resolve(false);
    }
    return this.write(ref, () => this.api.patch(ref, patch.body));
  }

  done(ref: string): Promise<boolean> {
    return this.write(ref, () => this.api.done(ref));
  }

  reopen(ref: string): Promise<boolean> {
    return this.write(ref, () => this.api.reopen(ref));
  }

  close(ref: string, resolution: Resolution): Promise<boolean> {
    return this.write(ref, () => this.api.close(ref, resolution));
  }

  /** Forgets a refusal, for a dialog that is opened or an action that is tried again. */
  clearActionError(): void {
    this.set({ actionError: undefined });
  }
}
