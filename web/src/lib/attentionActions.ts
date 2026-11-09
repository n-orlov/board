// The Attention view's inline actions (PRD R9, SPEC 11): which actions an item offers, what each one sends, and the
// controller that runs one at a time. Answer and Withdraw act on an ask, Close on a done task, Acknowledge on an expired
// lease and Record check on an overdue watch. A refusal (from the API or from the input) comes back as text and the
// success as a toast line; the item itself leaves its section through the live refresh, not through this module.
import type { components } from './api/schema';
import { mountPrefix } from './apibase';
import type { SectionKey } from './attention';
import { errorText } from './fetches';
import { problemText } from './keys';
import type { Browser } from './session';
import { RESOLUTIONS, summaryProblem } from './taskPage';
import type { Resolution } from './tasks';

export type Ask = components['schemas']['Ask'];
export type ActionKind = 'answer' | 'withdraw' | 'close' | 'acknowledge' | 'check';

/** The most an answer may hold, and the most a withdrawal's reason may hold. */
export const ANSWER_LIMIT = 20000;
export const REASON_LIMIT = 200;

const ACTIONS: Record<SectionKey, readonly ActionKind[]> = {
  open_asks: ['answer', 'withdraw'],
  done_not_closed: ['close'],
  expired_leases: ['acknowledge'],
  overdue_watches: ['check'],
  conflicts: [],
};

const LABELS: Record<ActionKind, string> = {
  answer: 'Answer',
  withdraw: 'Withdraw',
  close: 'Close',
  acknowledge: 'Acknowledge',
  check: 'Record check',
};

/** The actions an item of a section offers, in the order they are drawn. */
export function actionsFor(section: SectionKey): readonly ActionKind[] {
  return ACTIONS[section];
}

/** The button text of an action. */
export function actionLabel(kind: ActionKind): string {
  return LABELS[kind];
}

/** The accessible name of an item's action button: the label and the ref it acts on. */
export function actionName(kind: ActionKind, target: string): string {
  return `${LABELS[kind]} ${target}`;
}

/** The slug of the initiative a task ref (`<slug>-<n>`) belongs to; undefined for anything else. */
export function initiativeOfTask(ref: string): string | undefined {
  const cut = ref.lastIndexOf('-');
  return cut > 0 && /^\d+$/.test(ref.slice(cut + 1)) ? ref.slice(0, cut) : undefined;
}

/** The slug of the initiative an item's task link (`#/t/<ref>`) is in; undefined for an item with no task link. */
export function initiativeOfHref(href: string | undefined): string | undefined {
  if (href === undefined || !href.startsWith('#/t/')) return undefined;
  try {
    return initiativeOfTask(decodeURIComponent(href.slice(4)));
  } catch {
    return undefined;
  }
}

/** The address of an initiative's page. */
export function initiativeHref(slug: string): string {
  return `#/i/${encodeURIComponent(slug)}`;
}

/** Why an answer cannot be sent: empty after trimming, or over the API's limit. Undefined when it can. */
export function answerProblem(text: string): string | undefined {
  const length = [...text.trim()].length;
  if (length === 0) return 'Write an answer or pick one of the options.';
  return length > ANSWER_LIMIT ? `An answer is at most ${ANSWER_LIMIT} characters; this one has ${length}.` : undefined;
}

/** Why a resolution cannot be sent: it is not one of the four. Undefined when it can. */
export function resolutionProblem(value: string): string | undefined {
  return RESOLUTIONS.some((r) => r.value === value) ? undefined : 'Pick a resolution.';
}

/** Why a withdrawal's reason cannot be sent (it may be empty). */
export function reasonProblem(text: string): string | undefined {
  const length = [...text.trim()].length;
  return length > REASON_LIMIT ? `A reason is at most ${REASON_LIMIT} characters; this one has ${length}.` : undefined;
}

/** What the action sends: the input check, the call and the toast that says it worked. */
export interface Plan {
  problem: string | undefined;
  call: () => Promise<unknown>;
  done: string;
}

/** What the controller calls; `ActionsApi` is the real one. */
export interface ActionApi {
  ask(ref: string): Promise<Ask>;
  answer(ref: string, answer: string): Promise<unknown>;
  withdraw(ref: string, reason: string): Promise<unknown>;
  close(task: string, resolution: Resolution): Promise<unknown>;
  acknowledge(lease: string): Promise<unknown>;
  check(task: string, summary: string): Promise<unknown>;
}

const PLANS: Record<ActionKind, (api: ActionApi, target: string, input: string) => Plan> = {
  answer: (api, ref, text) => ({ problem: answerProblem(text), call: () => api.answer(ref, text.trim()), done: `Answered ${ref}` }),
  withdraw: (api, ref, text) => ({ problem: reasonProblem(text), call: () => api.withdraw(ref, text.trim()), done: `Withdrew ${ref}` }),
  close: (api, task, resolution) => ({ problem: resolutionProblem(resolution), call: () => api.close(task, resolution as Resolution), done: `Task ${task} closed as ${resolution}` }),
  acknowledge: (api, lease) => ({ problem: undefined, call: () => api.acknowledge(lease), done: `Lease ${lease} acknowledged` }),
  check: (api, task, text) => ({ problem: summaryProblem(text), call: () => api.check(task, text.trim()), done: `Check recorded on ${task}` }),
};

/** The plan of one action on one target with the typed input. */
export function planFor(api: ActionApi, kind: ActionKind, target: string, input: string): Plan {
  return PLANS[kind](api, target, input);
}

export interface ActionsState {
  /** The dialog that is open: the action and the ref it acts on. */
  open: { kind: ActionKind; target: string } | undefined;
  /** The ask the Answer dialog shows (question, details, options); undefined until read. */
  ask: Ask | undefined;
  /** Why the last action was refused, shown inside the open dialog. */
  error: string | undefined;
  /** Why the last action on an item with no dialog (Acknowledge) was refused, and the item. */
  itemError: { target: string; text: string } | undefined;
}

export const NO_ACTIONS: ActionsState = { open: undefined, ask: undefined, error: undefined, itemError: undefined };

export class AttentionActions {
  private readonly api: ActionApi;
  private readonly onChange: (state: ActionsState) => void;
  private current: ActionsState = NO_ACTIONS;

  constructor(api: ActionApi, onChange: (state: ActionsState) => void) {
    this.api = api;
    this.onChange = onChange;
  }

  get state(): ActionsState {
    return this.current;
  }

  private set(patch: Partial<ActionsState>): void {
    this.current = { ...this.current, ...patch };
    this.onChange(this.current);
  }

  /** Opens an action's dialog; the Answer dialog reads the ask first, a failure to read it is shown inside. */
  async open(kind: ActionKind, target: string): Promise<void> {
    this.set({ open: { kind, target }, ask: undefined, error: undefined, itemError: undefined });
    if (kind !== 'answer') return;
    try {
      const ask = await this.api.ask(target);
      if (this.current.open?.target === target) this.set({ ask });
    } catch (e) {
      if (this.current.open?.target === target) this.set({ error: errorText(e) });
    }
  }

  /** Closes the dialog, with whatever refusal it showed. */
  cancel(): void {
    this.set({ open: undefined, ask: undefined, error: undefined });
  }

  /**
   * Runs an action with the typed input. On success the dialog closes and the toast text comes back; on a refusal the
   * reason is kept (inside the dialog, or on the item when no dialog is open) and nothing comes back.
   */
  async run(kind: ActionKind, target: string, input = ''): Promise<string | undefined> {
    const plan = planFor(this.api, kind, target, input);
    let problem = plan.problem;
    if (problem === undefined) {
      try {
        await plan.call();
      } catch (e) {
        problem = errorText(e);
      }
    }
    if (problem !== undefined) {
      this.set(this.current.open === undefined ? { itemError: { target, text: problem } } : { error: problem });
      return undefined;
    }
    this.set({ open: undefined, ask: undefined, error: undefined, itemError: undefined });
    return plan.done;
  }
}

/** The real calls: the ask and task operations of the API, with the CSRF token of the session on writes. */
export class ActionsApi implements ActionApi {
  private readonly browser: Browser;
  private readonly csrf: () => string;

  constructor(browser: Browser, csrf: () => string) {
    this.browser = browser;
    this.csrf = csrf;
  }

  private async send<T>(method: string, path: string, what: string, body?: object): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (method !== 'GET') headers['X-CSRF-Token'] = this.csrf();
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    let response: Response;
    try {
      response = await this.browser.fetch(mountPrefix(this.browser.pathname) + path, {
        method,
        headers,
        credentials: 'same-origin',
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
    } catch {
      throw new Error('The server is not reachable.');
    }
    if (!response.ok) throw new Error(await problemText(response, what));
    return (await response.json()) as T;
  }

  ask(ref: string): Promise<Ask> {
    return this.send('GET', `/api/v1/asks/${encodeURIComponent(ref)}`, 'Reading the ask');
  }

  answer(ref: string, answer: string): Promise<unknown> {
    return this.send('POST', `/api/v1/asks/${encodeURIComponent(ref)}/answer`, 'Answering the ask', { answer });
  }

  withdraw(ref: string, reason: string): Promise<unknown> {
    return this.send('POST', `/api/v1/asks/${encodeURIComponent(ref)}/withdraw`, 'Withdrawing the ask', reason === '' ? {} : { reason });
  }

  close(task: string, resolution: Resolution): Promise<unknown> {
    return this.send('POST', `/api/v1/tasks/${encodeURIComponent(task)}/close`, 'Closing the task', { resolution });
  }

  acknowledge(lease: string): Promise<unknown> {
    return this.send('POST', `/api/v1/leases/${encodeURIComponent(lease)}/ack`, 'Acknowledging the lease');
  }

  check(task: string, summary: string): Promise<unknown> {
    return this.send('POST', `/api/v1/tasks/${encodeURIComponent(task)}/checks`, 'Recording the check', { summary });
  }
}
