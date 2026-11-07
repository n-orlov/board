// The Tasks view's data layer (PRD R10, SPEC 11). `TasksApi` wraps the task operations the view and the New task
// dialog use; `TasksStore` holds the list under a filter; `NewTaskStore` holds what the dialog needs (the open
// initiatives and open tasks to pick from) and the refusal of the last create.
import { mountPrefix } from './apibase';
import type { components } from './api/schema';
import { errorText, Fetches } from './fetches';
import { problemText } from './keys';
import { InitiativesApi, InitiativesError, type InitiativeListItem, type TaskListItem } from './initiatives';
import { toTaskCreate, type NewTaskForm, type TaskCreate } from './newTask';
import { apiQuery, DEFAULT_TASKS_FILTER, type TasksFilter } from './taskFilters';
import type { Browser } from './session';

export type TaskDetail = components['schemas']['TaskDetail'];

export class TasksApi {
  private readonly browser: Browser;
  private readonly csrf: () => string;

  /** `csrf` is read at each write, so a page that rotates its token is followed. */
  constructor(browser: Browser, csrf: () => string) {
    this.browser = browser;
    this.csrf = csrf;
  }

  private async call(path: string, what: string, init: RequestInit): Promise<Response> {
    let response: Response;
    try {
      response = await this.browser.fetch(mountPrefix(this.browser.pathname) + path, { ...init, credentials: 'same-origin' });
    } catch {
      throw new InitiativesError(0, 'The server is not reachable.');
    }
    if (!response.ok) throw new InitiativesError(response.status, await problemText(response, what));
    return response;
  }

  /** Every task the query keeps, following `next_after` through the pages. */
  async listQuery(query: URLSearchParams): Promise<TaskListItem[]> {
    const items: TaskListItem[] = [];
    let after: string | undefined;
    do {
      const page = new URLSearchParams(query);
      if (after !== undefined) page.set('after', after);
      const response = await this.call(`/api/v1/tasks?${page.toString()}`, 'Listing the tasks', { headers: { Accept: 'application/json' } });
      const body = (await response.json()) as components['schemas']['TaskList'];
      items.push(...body.items);
      after = body.next_after;
    } while (after !== undefined);
    return items;
  }

  /** The tasks a filter keeps, across initiatives. */
  list(filter: TasksFilter): Promise<TaskListItem[]> {
    return this.listQuery(apiQuery(filter));
  }

  /** The open tasks of every initiative: what a new task can be blocked by. */
  openTasks(): Promise<TaskListItem[]> {
    return this.listQuery(new URLSearchParams({ status: 'open' }));
  }

  async create(body: TaskCreate): Promise<TaskDetail> {
    const response = await this.call('/api/v1/tasks', 'Creating the task', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrf() },
      body: JSON.stringify(body),
    });
    return (await response.json()) as TaskDetail;
  }
}

export interface TasksState {
  tasks: TaskListItem[];
  filter: TasksFilter;
  loading: boolean;
  /** Why the list could not be read. */
  error: string | undefined;
}

type ListApi = Pick<TasksApi, 'list'>;

export class TasksStore {
  private readonly api: ListApi;
  private readonly onChange: (state: TasksState) => void;
  private current: TasksState;
  private readonly fetches = new Fetches();

  constructor(api: ListApi, onChange: (state: TasksState) => void, filter: TasksFilter = DEFAULT_TASKS_FILTER) {
    this.api = api;
    this.onChange = onChange;
    this.current = { tasks: [], filter, loading: true, error: undefined };
  }

  get state(): TasksState {
    return this.current;
  }

  private set(patch: Partial<TasksState>): void {
    this.current = { ...this.current, ...patch };
    this.onChange(this.current);
  }

  /** Fetches the list under the current filter; of overlapping fetches the newest outcome to land is shown. */
  async load(): Promise<void> {
    const ticket = this.fetches.begin();
    this.set({ loading: true });
    let outcome: Partial<TasksState>;
    try {
      outcome = { tasks: await this.api.list(this.current.filter), error: undefined };
    } catch (e) {
      outcome = { error: errorText(e) };
    }
    const shown = this.fetches.end(ticket) ? outcome : {};
    this.set({ ...shown, loading: this.fetches.busy });
  }

  /** Switches to a filter and fetches the list under it. */
  async setFilter(filter: TasksFilter): Promise<void> {
    this.set({ filter });
    await this.load();
  }
}

export interface NewTaskState {
  /** The open initiatives the task can go in. */
  initiatives: InitiativeListItem[];
  /** The open tasks the task can be blocked by. */
  openTasks: TaskListItem[];
  loading: boolean;
  /** Why the lists could not be read, or why the last create was refused; shown in the form, which keeps what was typed. */
  error: string | undefined;
}

type NewTaskApi = Pick<TasksApi, 'openTasks' | 'create'> & { initiatives: () => Promise<InitiativeListItem[]> };

/** The New task dialog's `initiatives` source: the open initiatives. */
export function openInitiatives(api: Pick<InitiativesApi, 'list'>): () => Promise<InitiativeListItem[]> {
  return () => api.list('open');
}

export class NewTaskStore {
  private readonly api: NewTaskApi;
  private readonly onChange: (state: NewTaskState) => void;
  private current: NewTaskState = { initiatives: [], openTasks: [], loading: true, error: undefined };

  constructor(api: NewTaskApi, onChange: (state: NewTaskState) => void) {
    this.api = api;
    this.onChange = onChange;
  }

  get state(): NewTaskState {
    return this.current;
  }

  private set(patch: Partial<NewTaskState>): void {
    this.current = { ...this.current, ...patch };
    this.onChange(this.current);
  }

  /** Reads what the pickers offer. */
  async load(): Promise<void> {
    try {
      const [initiatives, openTasks] = await Promise.all([this.api.initiatives(), this.api.openTasks()]);
      this.set({ initiatives, openTasks, loading: false, error: undefined });
    } catch (e) {
      this.set({ loading: false, error: errorText(e) });
    }
  }

  /** Makes the task. Its ref when it worked; undefined on a refusal, and `error` says why. */
  async create(form: NewTaskForm): Promise<string | undefined> {
    const built = toTaskCreate(form);
    if (!built.ok) {
      this.set({ error: built.error });
      return undefined;
    }
    try {
      const task = await this.api.create(built.body);
      this.set({ error: undefined });
      return task.ref;
    } catch (e) {
      this.set({ error: errorText(e) });
      return undefined;
    }
  }

  /** Forgets a refusal. */
  clearError(): void {
    this.set({ error: undefined });
  }
}
