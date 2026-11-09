// The asks and notes an initiative page shows (PRD R10, SPEC 11): `ThreadApi` reads and writes them through the
// session, and `ThreadStore` holds what the page draws: the open asks of the initiative and its tasks, its notes
// (without bodies, which are read when a note is expanded), and the refusal of the last write.
import { mountPrefix } from './apibase';
import { answerProblem, reasonProblem, type Ask } from './asks';
import { errorText } from './fetches';
import { InitiativesError } from './initiatives';
import { problemText } from './keys';
import { toNotePost, type Note, type NoteForm, type NotePost } from './notes';
import type { Browser } from './session';

const PAGE = '1000';

/** The initiative whose asks and notes are shown. */
export interface ThreadScope {
  initiative: string;
}

export class ThreadApi {
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

  private async pages<T>(path: string, query: URLSearchParams, what: string): Promise<T[]> {
    const items: T[] = [];
    let after: string | undefined;
    do {
      if (after !== undefined) query.set('after', after);
      const response = await this.call(`${path}?${query.toString()}`, what, { headers: { Accept: 'application/json' } });
      const page = (await response.json()) as { items: T[]; next_after?: string };
      items.push(...page.items);
      after = page.next_after;
    } while (after !== undefined);
    return items;
  }

  private async write<T>(path: string, what: string, body: object): Promise<T> {
    const headers = { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrf() };
    const response = await this.call(path, what, { method: 'POST', headers, body: JSON.stringify(body) });
    return (await response.json()) as T;
  }

  /** The open asks on the initiative and its tasks, in raising order. */
  openAsks(scope: ThreadScope): Promise<Ask[]> {
    return this.pages<Ask>('/api/v1/asks', new URLSearchParams({ initiative: scope.initiative, state: 'open', limit: PAGE }), 'Reading the asks');
  }

  /** The notes on the initiative and its tasks, without bodies, in posting order. */
  notes(scope: ThreadScope): Promise<Note[]> {
    return this.pages<Note>('/api/v1/notes', new URLSearchParams({ initiative: scope.initiative, fields: 'summary', limit: PAGE }), 'Reading the notes');
  }

  /** One note with its body. */
  async note(ref: string): Promise<Note> {
    const response = await this.call(`/api/v1/notes/${encodeURIComponent(ref)}`, 'Reading the note', { headers: { Accept: 'application/json' } });
    return (await response.json()) as Note;
  }

  answerAsk(ask: string, answer: string): Promise<Ask> {
    return this.write<Ask>(`/api/v1/asks/${encodeURIComponent(ask)}/answer`, 'Answering the ask', { answer });
  }

  withdrawAsk(ask: string, reason: string): Promise<Ask> {
    return this.write<Ask>(`/api/v1/asks/${encodeURIComponent(ask)}/withdraw`, 'Withdrawing the ask', reason === '' ? {} : { reason });
  }

  postNote(body: NotePost): Promise<Note> {
    return this.write<Note>('/api/v1/notes', 'Posting the note', body);
  }
}

export interface ThreadState {
  asks: Ask[];
  notes: Note[];
  /** The bodies read so far, by note ref. */
  bodies: Record<string, string>;
  /** Why the asks or notes could not be read. */
  error: string | undefined;
  /** Why the last write was refused; shown inside its dialog, which keeps what was typed. */
  actionError: string | undefined;
}

type Api = Pick<ThreadApi, 'openAsks' | 'notes' | 'note' | 'answerAsk' | 'withdrawAsk' | 'postNote'>;

export class ThreadStore {
  private readonly api: Api;
  private readonly onChange: (state: ThreadState) => void;
  private current: ThreadState = { asks: [], notes: [], bodies: {}, error: undefined, actionError: undefined };

  constructor(api: Api, onChange: (state: ThreadState) => void) {
    this.api = api;
    this.onChange = onChange;
  }

  get state(): ThreadState {
    return this.current;
  }

  private set(patch: Partial<ThreadState>): void {
    this.current = { ...this.current, ...patch };
    this.onChange(this.current);
  }

  /** Reads the open asks and the notes; either failing shows one error and neither list. */
  async load(scope: ThreadScope): Promise<void> {
    try {
      const [asks, notes] = await Promise.all([this.api.openAsks(scope), this.api.notes(scope)]);
      this.set({ asks, notes, error: undefined });
    } catch (e) {
      this.set({ asks: [], notes: [], error: errorText(e) });
    }
  }

  /** Runs a write; on success the lists are read again, on a refusal `actionError` says why. Whether it worked. */
  private async write(scope: ThreadScope, action: () => Promise<unknown>): Promise<boolean> {
    try {
      await action();
    } catch (e) {
      this.set({ actionError: errorText(e) });
      return false;
    }
    this.set({ actionError: undefined });
    await this.load(scope);
    return true;
  }

  /** A write whose input is checked first: a problem with it is a refusal too, and nothing is sent. */
  private checked(scope: ThreadScope, problem: string | undefined, action: () => Promise<unknown>): Promise<boolean> {
    if (problem === undefined) return this.write(scope, action);
    this.set({ actionError: problem });
    return Promise.resolve(false);
  }

  answer(scope: ThreadScope, ask: string, answer: string): Promise<boolean> {
    return this.checked(scope, answerProblem(answer), () => this.api.answerAsk(ask, answer.trim()));
  }

  withdraw(scope: ThreadScope, ask: string, reason: string): Promise<boolean> {
    return this.checked(scope, reasonProblem(reason), () => this.api.withdrawAsk(ask, reason.trim()));
  }

  /** Posts a note on the initiative; a form that cannot be sent is a refusal too. */
  postNote(scope: ThreadScope, form: NoteForm): Promise<boolean> {
    const post = toNotePost(form, { initiative: scope.initiative });
    if (!post.ok) {
      this.set({ actionError: post.error });
      return Promise.resolve(false);
    }
    return this.write(scope, () => this.api.postNote(post.body));
  }

  /** Reads the body of a note when it is expanded; a note whose body is held is not read again. */
  async loadBody(note: string): Promise<void> {
    if (this.current.bodies[note] !== undefined) return;
    try {
      const read = await this.api.note(note);
      this.set({ bodies: { ...this.current.bodies, [note]: read.body ?? '' } });
    } catch (e) {
      this.set({ actionError: errorText(e) });
    }
  }

  /** Forgets a refusal, for a dialog that is opened again. */
  clearActionError(): void {
    this.set({ actionError: undefined });
  }
}

