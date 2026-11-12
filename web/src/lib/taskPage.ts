// The task page's data layer and presentation (PRD R10, SPEC 11): `TaskPageStore` loads one task and its events and runs
// the page's writes (Edit, Done, Reopen, Close); the functions turn the task into what the page prints (planned window,
// live leases with "expires in", the event timeline newest first).
import { cadenceText, expiresIn } from './durations';
import { errorText } from './fetches';
import { absoluteTime, relativeTime } from './format';
import { toTaskPatch, type TaskEditForm } from './taskEdit';
import { targetsText } from './initiativePage';
import { answerProblem, reasonProblem, type Ask } from './asks';
import { pickerOptions } from './newTask';
import { toNotePost, type Note, type NoteForm } from './notes';
import type { Lease, Resolution, TaskDetail, TasksApi, TaskEvent } from './tasks';
import type { TaskListItem } from './initiatives';

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

/** The longest summary or outcome the API takes. */
export const NOTE_LIMIT = 200;

/** Why a summary (a milestone's or a check's) cannot be sent: empty after trimming, or over the API's limit. Undefined when it can. */
export function summaryProblem(text: string): string | undefined {
  const length = [...text.trim()].length;
  if (length === 0) return 'Write a summary.';
  return length > NOTE_LIMIT ? `A summary is at most ${NOTE_LIMIT} characters; this one has ${length}.` : undefined;
}

/** Why an outcome cannot be sent (it may be empty). */
export function outcomeProblem(text: string): string | undefined {
  const length = [...text.trim()].length;
  return length > NOTE_LIMIT ? `An outcome is at most ${NOTE_LIMIT} characters; this one has ${length}.` : undefined;
}

/** The open tasks a dependency can be added on for the typed text: not the task itself and not one it already waits for. */
export function depOptions(candidates: readonly TaskListItem[], task: Pick<TaskDetail, 'ref' | 'deps'>, text: string): TaskListItem[] {
  return pickerOptions(candidates, text, [task.ref, ...task.deps.map((dep) => dep.ref)]);
}

export interface ExpiredRow {
  ref: string;
  holder: string;
  intent: string;
  resources: string;
  expired: string;
}

/** The leases of the task that ran out and nobody acknowledged yet, in the order given (oldest first); `except` is the stranded lease, which has its own panel. */
export function expiredRows(leases: readonly Lease[], now: Date, except?: string): ExpiredRow[] {
  return leases
    .filter((lease) => lease.acked_at === undefined && lease.ref !== except)
    .map((lease) => ({
      ref: lease.ref,
      holder: lease.actor,
      intent: lease.intent,
      resources: lease.resources.join(', '),
      expired: expiresIn(lease.expires_at, now),
    }));
}

export interface StrandedPanel {
  /** The lease's ref, what Acknowledge acts on. */
  lease: string;
  holder: string;
  /** The holder's way back to copy; undefined when the board has none on record. */
  wayBack: string | undefined;
  intent: string;
  /** "renewed 2 minutes ago". */
  renewed: string;
  renewedAt: string;
  renewedTitle: string;
  /** "expired 30s ago". */
  expired: string;
  expiredAt: string;
  expiredTitle: string;
}

/** What the Stranded panel shows for a task: its stranded lease, or undefined when the task is not stranded. */
export function strandedPanel(task: Pick<TaskDetail, 'stranded_lease'>, now: Date): StrandedPanel | undefined {
  const lease = task.stranded_lease;
  if (lease === undefined) return undefined;
  const way = lease.holder.way_back;
  return {
    lease: lease.ref,
    holder: lease.holder.handle,
    wayBack: way === undefined || way === '' ? undefined : way,
    intent: lease.intent,
    renewed: `renewed ${relativeTime(lease.renewed_at, now)}`,
    renewedAt: lease.renewed_at,
    renewedTitle: absoluteTime(lease.renewed_at),
    expired: expiresIn(lease.expired_at, now),
    expiredAt: lease.expired_at,
    expiredTitle: absoluteTime(lease.expired_at),
  };
}

/** The accessible name of the panel's copy button. */
export function copyWayBackLabel(panel: Pick<StrandedPanel, 'holder'>): string {
  return `Copy way back of ${panel.holder}`;
}

/** The body of the Acknowledge confirmation. */
export const ACK_BODY = 'The lease ran out without being ended. Acknowledging it records that you have seen it and takes the task off Needs attention.';

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
  /** The leases of the task that ran out. */
  expired: Lease[];
  /** Every live lease on the board, the conflicts of the task's leases are read from them. */
  live: Lease[];
  loading: boolean;
  /** Why the page could not be read. */
  error: string | undefined;
  /** Why the last action was refused; shown inline (inside the dialog when one is open). */
  actionError: string | undefined;
  /** The open tasks a dependency can be added on; read when the Add dependency dialog opens. */
  candidates: TaskListItem[];
  /** Every ask of the task, in raising order. */
  asks: Ask[];
  /** Every note on the task without bodies, in posting order. */
  notes: Note[];
  /** The bodies read so far, by note ref (a note's body is read when it is expanded). */
  bodies: Record<string, string>;
}

type PageApi = Pick<
  TasksApi,
  'get' | 'events' | 'asksOf' | 'notesOf' | 'note' | 'answerAsk' | 'withdrawAsk' | 'postNote' | 'expiredLeases' | 'liveLeases' | 'openTasks' | 'patch' | 'done' | 'reopen' | 'close' | 'addDep' | 'removeDep' | 'milestone' | 'recordCheck' | 'endLease' | 'ackLease'
>;

export class TaskPageStore {
  private readonly api: PageApi;
  private readonly onChange: (state: TaskPageState) => void;
  private current: TaskPageState = { task: undefined, events: [], expired: [], live: [], loading: true, error: undefined, actionError: undefined, candidates: [], asks: [], notes: [], bodies: {} };

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

  /**
   * Reads the page again for a live event, without the skeleton: an open dialog, its typed fields and the scroll position
   * stay, and a failed read keeps the page it had and shows the error above it.
   */
  async refresh(ref: string): Promise<void> {
    await this.read(ref, true);
  }

  private async read(ref: string, keep = false): Promise<void> {
    try {
      const [task, events, expired, live, asks, notes] = await Promise.all([
        this.api.get(ref),
        this.api.events(ref),
        this.api.expiredLeases(ref),
        this.api.liveLeases(),
        this.api.asksOf(ref),
        this.api.notesOf(ref),
      ]);
      this.set({ task, events, expired, live, asks, notes, loading: false, error: undefined });
    } catch (e) {
      if (keep) this.set({ error: errorText(e) });
      else this.set({ task: undefined, events: [], expired: [], live: [], asks: [], notes: [], loading: false, error: errorText(e) });
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

  /** A write whose input is checked first: a problem with it is a refusal too, and nothing is sent. */
  private checked(ref: string, problem: string | undefined, action: () => Promise<unknown>): Promise<boolean> {
    if (problem === undefined) return this.write(ref, action);
    this.set({ actionError: problem });
    return Promise.resolve(false);
  }

  addDep(ref: string, blockedBy: string): Promise<boolean> {
    return this.write(ref, () => this.api.addDep(ref, blockedBy));
  }

  removeDep(ref: string, blockedBy: string): Promise<boolean> {
    return this.write(ref, () => this.api.removeDep(ref, blockedBy));
  }

  milestone(ref: string, summary: string): Promise<boolean> {
    return this.checked(ref, summaryProblem(summary), () => this.api.milestone(ref, summary.trim()));
  }

  recordCheck(ref: string, summary: string): Promise<boolean> {
    return this.checked(ref, summaryProblem(summary), () => this.api.recordCheck(ref, summary.trim()));
  }

  endLease(ref: string, lease: string, outcome: string): Promise<boolean> {
    return this.checked(ref, outcomeProblem(outcome), () => this.api.endLease(lease, outcome.trim()));
  }

  ackLease(ref: string, lease: string): Promise<boolean> {
    return this.write(ref, () => this.api.ackLease(lease));
  }

  answer(ref: string, ask: string, answer: string): Promise<boolean> {
    return this.checked(ref, answerProblem(answer), () => this.api.answerAsk(ask, answer.trim()));
  }

  withdraw(ref: string, ask: string, reason: string): Promise<boolean> {
    return this.checked(ref, reasonProblem(reason), () => this.api.withdrawAsk(ask, reason.trim()));
  }

  /** Posts a note on the task; a form that cannot be sent is a refusal too. */
  postNote(ref: string, form: NoteForm): Promise<boolean> {
    const post = toNotePost(form, { task: ref });
    if (!post.ok) {
      this.set({ actionError: post.error });
      return Promise.resolve(false);
    }
    return this.write(ref, () => this.api.postNote(post.body));
  }

  /** Reads the body of a note when it is expanded; a note whose body is held is not read again. A failure is shown as an action error. */
  async loadBody(note: string): Promise<void> {
    if (this.current.bodies[note] !== undefined) return;
    try {
      const read = await this.api.note(note);
      this.set({ bodies: { ...this.current.bodies, [note]: read.body ?? '' } });
    } catch (e) {
      this.set({ actionError: errorText(e) });
    }
  }

  /** Reads the open tasks the Add dependency dialog offers; a failure is a refusal shown in the dialog. */
  async loadCandidates(): Promise<void> {
    try {
      this.set({ candidates: await this.api.openTasks() });
    } catch (e) {
      this.set({ actionError: errorText(e) });
    }
  }

  /** Forgets a refusal, for a dialog that is opened or an action that is tried again. */
  clearActionError(): void {
    this.set({ actionError: undefined });
  }
}

/**
 * Whether a live event can change the page of task `ref` (written in either letter case): an event that names the task, a task
 * event of another task (a blocker finishing changes what this one waits for) and any lease event (the conflicts read every live lease).
 */
export function affectsTaskPage(ref: string): (event: { type: string; task?: string }) => boolean {
  const own = ref.toLowerCase();
  return (event) => event.task?.toLowerCase() === own || event.type.startsWith('task_') || event.type.startsWith('lease_');
}
