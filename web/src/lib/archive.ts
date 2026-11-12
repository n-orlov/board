// The Archive view's data layer and mapping (PRD R7, SPEC 11): the closed tasks and closed initiatives of a time
// window, newest closed first. The window is held in the fragment query (`#/archive?window=7`); the default window
// (30 days) leaves the query empty. `ArchiveApi` reads the two lists and reopens; `ArchiveStore` keeps the newest
// answer and the refusal of the last reopen.
import type { components } from './api/schema';
import { errorText, Fetches } from './fetches';
import { absoluteTime, relativeTime } from './format';
import { taskHref } from './taskPage';
import { ThreadApi } from './threads';
import type { BoardEvent } from './live';

type Task = components['schemas']['TaskListItem'];
type Initiative = components['schemas']['InitiativeListItem'];

/** How far back the view looks: 7, 30 or 90 days, or every closed item. */
export type ArchiveWindow = '7' | '30' | '90' | 'all';

export const ARCHIVE_WINDOWS: readonly { value: ArchiveWindow; label: string }[] = [
  { value: '7', label: '7 days' },
  { value: '30', label: '30 days' },
  { value: '90', label: '90 days' },
  { value: 'all', label: 'All' },
];

export const DEFAULT_ARCHIVE_WINDOW: ArchiveWindow = '30';

/** The window a raw query value names; a missing or unknown value is the default. */
export function parseWindow(raw: string | null | undefined): ArchiveWindow {
  return ARCHIVE_WINDOWS.find((w) => w.value === raw)?.value ?? DEFAULT_ARCHIVE_WINDOW;
}

/** The window held by a query (`window=7`, with or without its leading `?`). */
export function windowFromQuery(query: string): ArchiveWindow {
  return parseWindow(new URLSearchParams(query).get('window'));
}

/** The query of a window for the URL fragment: empty for the default, so `#/archive` is the default view. */
export function windowToQuery(window: ArchiveWindow): string {
  return window === DEFAULT_ARCHIVE_WINDOW ? '' : new URLSearchParams({ window }).toString();
}

/** The fragment of the view under a window. */
export function archiveHash(window: ArchiveWindow): string {
  const query = windowToQuery(window);
  return query === '' ? '#/archive' : `#/archive?${query}`;
}

/** The window held by a fragment: everything after the first `?` is the query. */
export function windowFromHash(hash: string): ArchiveWindow {
  const at = hash.indexOf('?');
  return windowFromQuery(at < 0 ? '' : hash.slice(at + 1));
}

/** The `GET /tasks` and `GET /initiatives` query of a window: closed items, and `closed_within` unless it is all. */
export function archiveQuery(window: ArchiveWindow): URLSearchParams {
  const params = new URLSearchParams({ status: 'closed' });
  if (window !== 'all') params.set('closed_within', `${window}d`);
  return params;
}

/** One closed item as the view draws it. */
export interface ArchiveRow {
  kind: 'task' | 'initiative';
  /** Unique across both kinds: the task ref or the initiative slug, prefixed by the kind. */
  key: string;
  /** The task ref (`E1-3`) or the initiative slug. */
  ref: string;
  title: string;
  /** The fragment of the item's page. */
  href: string;
  /** How a task was closed; an initiative has no resolution. */
  resolution: string;
  closedAt: string;
  /** "3 days ago", and the absolute time for its title. */
  closed: string;
  closedTitle: string;
  closedBy: string;
}

function ms(row: { closed_at?: string }): number {
  return row.closed_at === undefined ? 0 : Date.parse(row.closed_at);
}

function taskRow(task: Task, now: Date): ArchiveRow {
  return {
    kind: 'task',
    key: `task:${task.ref}`,
    ref: task.ref,
    title: task.title,
    href: taskHref(task.ref),
    resolution: task.resolution ?? 'closed',
    closedAt: task.closed_at ?? '',
    closed: task.closed_at === undefined ? '' : relativeTime(task.closed_at, now),
    closedTitle: task.closed_at === undefined ? '' : absoluteTime(task.closed_at),
    closedBy: task.closed_by ?? '',
  };
}

function initiativeRow(initiative: Initiative, now: Date): ArchiveRow {
  return {
    kind: 'initiative',
    key: `initiative:${initiative.slug}`,
    ref: initiative.slug,
    title: initiative.title,
    href: `#/i/${encodeURIComponent(initiative.slug)}`,
    resolution: 'initiative closed',
    closedAt: initiative.closed_at ?? '',
    closed: initiative.closed_at === undefined ? '' : relativeTime(initiative.closed_at, now),
    closedTitle: initiative.closed_at === undefined ? '' : absoluteTime(initiative.closed_at),
    closedBy: initiative.closed_by ?? '',
  };
}

/** The rows of the view: tasks and initiatives together, `closed_at` newest first, ties by key. `now` phrases the ages. */
export function archiveRows(tasks: readonly Task[], initiatives: readonly Initiative[], now: Date): ArchiveRow[] {
  const closed = [
    ...tasks.map((t) => ({ at: ms(t), row: taskRow(t, now) })),
    ...initiatives.map((i) => ({ at: ms(i), row: initiativeRow(i, now) })),
  ];
  closed.sort((a, b) => b.at - a.at || (a.row.key < b.row.key ? -1 : a.row.key > b.row.key ? 1 : 0));
  return closed.map((c) => c.row);
}

/** The words of the Reopen confirmation. */
export function reopenBody(row: Pick<ArchiveRow, 'kind' | 'ref'>): string {
  return row.kind === 'task'
    ? `Task ${row.ref} goes back to open and shows in the Tasks view again.`
    : `Initiative ${row.ref} goes back to open and takes tasks again. Its closed tasks stay closed.`;
}

/** The confirm button of the Reopen dialog. */
export function reopenLabel(row: Pick<ArchiveRow, 'kind'>): string {
  return row.kind === 'task' ? 'Reopen task' : 'Reopen initiative';
}

/** The toast after a reopen. */
export function reopenedToast(row: Pick<ArchiveRow, 'kind' | 'ref'>): string {
  return row.kind === 'task' ? `Task ${row.ref} reopened` : `Initiative ${row.ref} reopened`;
}

/** Whether a live event can change the archive: a task or initiative closed or reopened. */
export function affectsArchive(event: Pick<BoardEvent, 'type'>): boolean {
  return ['task_closed', 'task_reopened', 'initiative_closed', 'initiative_reopened'].includes(event.type);
}

export class ArchiveApi extends ThreadApi {
  /** The closed tasks of a window. */
  tasks(window: ArchiveWindow): Promise<Task[]> {
    return this.pages<Task>('/api/v1/tasks', archiveQuery(window), 'Listing the closed tasks');
  }

  /** The closed initiatives of a window. */
  initiatives(window: ArchiveWindow): Promise<Initiative[]> {
    return this.pages<Initiative>('/api/v1/initiatives', archiveQuery(window), 'Listing the closed initiatives');
  }

  private async reopenPath(path: string, what: string): Promise<void> {
    const headers = { Accept: 'application/json', 'X-CSRF-Token': this.csrf() };
    await this.call(path, what, { method: 'POST', headers });
  }

  /** Reopens a closed task. */
  reopenTask(ref: string): Promise<void> {
    return this.reopenPath(`/api/v1/tasks/${encodeURIComponent(ref)}/reopen`, 'Reopening the task');
  }

  /** Reopens a closed initiative; its tasks stay closed. */
  reopenInitiative(slug: string): Promise<void> {
    return this.reopenPath(`/api/v1/initiatives/${encodeURIComponent(slug)}/reopen`, 'Reopening the initiative');
  }
}

export interface ArchiveState {
  window: ArchiveWindow;
  tasks: Task[];
  initiatives: Initiative[];
  /** Whether the first read has landed. */
  loaded: boolean;
  loading: boolean;
  /** The refusal of the last read, empty when it worked. */
  error: string;
  /** Why the last reopen was refused; shown inside its dialog, empty otherwise. */
  dialogError: string;
}

type StoreApi = Pick<ArchiveApi, 'tasks' | 'initiatives' | 'reopenTask' | 'reopenInitiative'>;

export class ArchiveStore {
  private readonly api: StoreApi;
  private readonly onChange: (state: ArchiveState) => void;
  private current: ArchiveState;
  private readonly fetches = new Fetches();

  constructor(api: StoreApi, onChange: (state: ArchiveState) => void, window: ArchiveWindow = DEFAULT_ARCHIVE_WINDOW) {
    this.api = api;
    this.onChange = onChange;
    this.current = { window, tasks: [], initiatives: [], loaded: false, loading: true, error: '', dialogError: '' };
  }

  get state(): ArchiveState {
    return this.current;
  }

  private publish(change: Partial<ArchiveState>): void {
    this.current = { ...this.current, ...change };
    this.onChange(this.current);
  }

  /** Reads the window. A refresh keeps what is shown until the answer lands; an answer older than one already shown is dropped. */
  async load(): Promise<void> {
    const ticket = this.fetches.begin();
    const { window } = this.current;
    this.publish({ loading: true });
    let outcome: Partial<ArchiveState>;
    try {
      const [tasks, initiatives] = await Promise.all([this.api.tasks(window), this.api.initiatives(window)]);
      outcome = { tasks, initiatives, loaded: true, error: '' };
    } catch (e) {
      outcome = { error: errorText(e) };
    }
    const show = this.fetches.end(ticket);
    this.publish({ ...(show ? outcome : {}), loading: this.fetches.busy });
  }

  /** Changes the window and reads it; the same window is not read again. */
  async setWindow(window: ArchiveWindow): Promise<void> {
    if (window === this.current.window) return;
    this.publish({ window });
    await this.load();
  }

  /** Reopens the row's item and reads the archive again; false, with the refusal in `dialogError`, when the server refuses. */
  async reopen(row: Pick<ArchiveRow, 'kind' | 'ref'>): Promise<boolean> {
    try {
      await (row.kind === 'task' ? this.api.reopenTask(row.ref) : this.api.reopenInitiative(row.ref));
    } catch (e) {
      this.publish({ dialogError: errorText(e) });
      return false;
    }
    this.publish({ dialogError: '' });
    await this.load();
    return true;
  }

  /** Forgets the refusal shown in a dialog (the dialog was closed or opened). */
  clearDialogError(): void {
    if (this.current.dialogError !== '') this.publish({ dialogError: '' });
  }
}
