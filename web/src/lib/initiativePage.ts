// The initiative page's data layer and presentation (PRD R9, SPEC 11): the page shows one initiative and its
// tasks. `InitiativePageStore` loads both and holds the task filter; the functions below turn them into what
// the page prints (counts, targets, one row per task with its badges and live lease holder).
import { countsText, leasesText, type TaskCounts } from './counts';
import { errorText } from './fetches';
import { DEFAULT_TASK_FILTER, filterTasks, type TaskFilter } from './initiativeFilters';
import type { Initiative, InitiativeEdit, InitiativesApi, TaskListItem } from './initiatives';
import { toPatch } from './initiatives';
import { attentionBadges, type AttentionBadge } from './attention';
import type { Tone } from './presentation';
import { DEFAULT_TASKS_FILTER, attentionHref, needsFilterQuery } from './taskFilters';

/** How many of the tasks are in each status. */
export function taskCounts(tasks: readonly { status: string }[]): TaskCounts {
  const counts: TaskCounts = { open: 0, done: 0, closed: 0 };
  for (const task of tasks) {
    if (task.status === 'open' || task.status === 'done' || task.status === 'closed') counts[task.status] += 1;
  }
  return counts;
}

/** How many live leases sit on the tasks. */
export function liveLeaseCount(tasks: readonly Pick<TaskListItem, 'live_leases'>[]): number {
  return tasks.reduce((sum, task) => sum + task.live_leases.length, 0);
}

/** "3 open · 1 done · 2 closed, 1 live lease": the counts line of the page. */
export function summaryText(tasks: readonly TaskListItem[]): string {
  return `${countsText(taskCounts(tasks))}, ${leasesText(liveLeaseCount(tasks))}`;
}

/** The planned window: "2026-01-01 to 2026-02-01", "from 2026-01-01", "until 2026-02-01"; empty when none is set. */
export function targetsText(initiative: Pick<Initiative, 'target_start' | 'target_end'>): string {
  const { target_start: start, target_end: end } = initiative;
  if (start !== undefined && end !== undefined) return `${start} to ${end}`;
  if (start !== undefined) return `from ${start}`;
  if (end !== undefined) return `until ${end}`;
  return '';
}

export interface TaskBadge {
  key: 'active' | 'blocked' | 'overdue';
  label: string;
  tone: Tone;
}

/** The derived flags of a task as badges, in the order active, blocked, overdue; a flag that is off has none. */
export function taskBadges(task: Pick<TaskListItem, 'active' | 'blocked' | 'overdue'>): TaskBadge[] {
  const badges: TaskBadge[] = [];
  if (task.active) badges.push({ key: 'active', label: 'active', tone: 'success' });
  if (task.blocked) badges.push({ key: 'blocked', label: 'blocked', tone: 'warning' });
  if (task.overdue) badges.push({ key: 'overdue', label: 'overdue', tone: 'danger' });
  return badges;
}

/** Who holds the live lease of a task: the oldest lease's actor, "+N" for each further one; empty with none. */
export function leaseHolder(task: Pick<TaskListItem, 'live_leases'>): string {
  const [first, ...rest] = task.live_leases;
  if (first === undefined) return '';
  return rest.length === 0 ? first.actor : `${first.actor} +${rest.length}`;
}

/** A row's flags as one sortable text, "active, blocked"; empty when the task has none. */
export function flagsText(row: Pick<TaskRow, 'badges'>): string {
  return row.badges.map((badge) => badge.label).join(', ');
}

export interface TaskRow {
  id: string;
  ref: string;
  initiative: string;
  title: string;
  kind: string;
  status: string;
  badges: TaskBadge[];
  /** Why the task needs a person, one badge per reason; empty when it needs nothing. */
  attention: AttentionBadge[];
  holder: string;
}

/** The rows the page's tasks table shows under a filter, in the order the API gave them. */
export function taskRows(tasks: readonly TaskListItem[], filter: TaskFilter): TaskRow[] {
  return filterTasks(tasks, filter).map((task) => ({
    id: task.id,
    ref: task.ref,
    initiative: task.initiative,
    title: task.title,
    kind: task.kind,
    status: task.status,
    badges: taskBadges(task),
    attention: attentionBadges(task.attention),
    holder: leaseHolder(task),
  }));
}

export interface InitiativePageState {
  initiative: Initiative | undefined;
  tasks: TaskListItem[];
  filter: TaskFilter;
  loading: boolean;
  /** Why the page could not be read. */
  error: string | undefined;
  /** Why the last edit, close or reopen was refused; shown inside its dialog, which keeps what was typed. */
  dialogError: string | undefined;
}

type PageApi = Pick<InitiativesApi, 'get' | 'tasks' | 'patch' | 'close' | 'reopen'>;

export class InitiativePageStore {
  private readonly api: PageApi;
  private readonly onChange: (state: InitiativePageState) => void;
  private current: InitiativePageState = { initiative: undefined, tasks: [], filter: DEFAULT_TASK_FILTER, loading: true, error: undefined, dialogError: undefined };

  constructor(api: PageApi, onChange: (state: InitiativePageState) => void) {
    this.api = api;
    this.onChange = onChange;
  }

  get state(): InitiativePageState {
    return this.current;
  }

  private set(patch: Partial<InitiativePageState>): void {
    this.current = { ...this.current, ...patch };
    this.onChange(this.current);
  }

  /** Reads the initiative and its tasks; either failing shows one error and no half page. */
  async load(slug: string): Promise<void> {
    this.set({ loading: true, dialogError: undefined });
    await this.read(slug);
  }

  private async read(slug: string): Promise<void> {
    try {
      const [initiative, tasks] = await Promise.all([this.api.get(slug), this.api.tasks(slug)]);
      this.set({ initiative, tasks, loading: false, error: undefined });
    } catch (e) {
      this.set({ initiative: undefined, tasks: [], loading: false, error: errorText(e) });
    }
  }

  /** Runs a write; on success the page is read again (without the skeleton), on a refusal `dialogError` says why. Whether it worked. */
  private async write(slug: string, action: () => Promise<unknown>): Promise<boolean> {
    try {
      await action();
    } catch (e) {
      this.set({ dialogError: errorText(e) });
      return false;
    }
    this.set({ dialogError: undefined });
    await this.read(slug);
    return true;
  }

  /** Reads the page again without the skeleton (after a task was created). */
  reload(slug: string): Promise<void> {
    return this.read(slug);
  }

  /** Saves the edit form. */
  edit(slug: string, form: InitiativeEdit): Promise<boolean> {
    return this.write(slug, () => this.api.patch(slug, toPatch(form)));
  }

  /** Closes the initiative; `force` drops the tasks that are not closed first. */
  close(slug: string, force: boolean): Promise<boolean> {
    return this.write(slug, () => this.api.close(slug, force));
  }

  /** Reopens the closed initiative. */
  reopen(slug: string): Promise<boolean> {
    return this.write(slug, () => this.api.reopen(slug));
  }

  /** Forgets a refusal, for a dialog that is opened again. */
  clearDialogError(): void {
    this.set({ dialogError: undefined });
  }

  /** Chooses which tasks the table shows; no fetch is needed, the page holds all of them. */
  setFilter(filter: TaskFilter): void {
    this.set({ filter });
  }
}

/** The live-event predicate of the page of initiative `slug`: events that name that initiative (its tasks, leases, asks, notes). */
export function concerns(slug: string): (event: { initiative?: string }) => boolean {
  return (event) => event.initiative === slug;
}

/** The Tasks view's filter that matches the initiative page's task filter on initiative `slug`. */
function tasksFilterOf(slug: string, filter: TaskFilter) {
  return { ...DEFAULT_TASKS_FILTER, initiatives: [slug], status: filter };
}

/** The query of the page's Needs attention section: its initiative's tasks under the page's task filter, with `attention=true`. */
export function pageNeedsQuery(slug: string, filter: TaskFilter): URLSearchParams {
  return needsFilterQuery(tasksFilterOf(slug, filter))!;
}

/** The fragment of the Tasks view that lists the initiative's tasks that need attention (what `+K more` links to). */
export function pageNeedsHref(slug: string, filter: TaskFilter): string {
  return attentionHref(tasksFilterOf(slug, filter));
}
