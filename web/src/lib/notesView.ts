// The Notes view's data layer (PRD R10, SPEC 11): `NotesApi` reads the notes under the view's filter, `NotesViewStore`
// holds them newest first and draws them a page at a time. The API lists notes oldest first and has no reverse
// order, so the store reads every page of the filtered list (summaries only, bodies are read when a note is
// expanded), turns it around and reveals `PAGE_SIZE` notes at a time: "Load more" shows the next page.
import { errorText, Fetches } from './fetches';
import { apiQuery, filterFromQuery, filterToQuery, newestFirst, toNotePost, type Note, type NoteForm, type NotesFilter } from './notes';
import { ThreadApi } from './threads';
import { clearViewQuery, openView, saveViewQuery, type ViewEnv, type ViewStorage } from './viewState';

/** How many notes the view shows first and each "Load more" adds. */
export const PAGE_SIZE = 20;

/** Where a note is posted from the Notes view: a task ref and an initiative slug, both optional (neither is a broadcast). */
export interface NoteScope {
  task: string;
  initiative: string;
}

export class NotesApi extends ThreadApi {
  /** Every note under the filter, without bodies, in posting order. */
  filtered(filter: NotesFilter): Promise<Note[]> {
    const query = apiQuery(filter);
    query.set('fields', 'summary');
    query.set('limit', '1000');
    return this.pages<Note>('/api/v1/notes', query, 'Reading the notes');
  }
}

export interface NotesViewState {
  filter: NotesFilter;
  /** Every note under the filter, newest first. */
  notes: Note[];
  /** How many of them are drawn. */
  shown: number;
  /** The bodies read so far, by note ref. */
  bodies: Record<string, string>;
  loading: boolean;
  error: string | undefined;
  /** Why the last write was refused; shown inside its dialog. */
  actionError: string | undefined;
}

/** The notes drawn: the first `shown` of the list. */
export function visibleNotes(state: Pick<NotesViewState, 'notes' | 'shown'>): Note[] {
  return state.notes.slice(0, state.shown);
}

/** How many notes "Load more" would add now, 0 when everything is drawn. */
export function moreCount(state: Pick<NotesViewState, 'notes' | 'shown'>): number {
  return Math.min(PAGE_SIZE, Math.max(0, state.notes.length - state.shown));
}

/** The scope of a post: the fields that are filled, trimmed. */
export function postScope(scope: NoteScope): { task?: string; initiative?: string } {
  const task = scope.task.trim();
  const initiative = scope.initiative.trim();
  return { ...(task === '' ? {} : { task }), ...(initiative === '' ? {} : { initiative }) };
}

type Api = Pick<NotesApi, 'filtered' | 'note' | 'postNote'>;

export class NotesViewStore {
  private readonly api: Api;
  private readonly onChange: (state: NotesViewState) => void;
  private readonly fetches = new Fetches();
  private current: NotesViewState;

  constructor(api: Api, onChange: (state: NotesViewState) => void, filter: NotesFilter) {
    this.api = api;
    this.onChange = onChange;
    this.current = { filter, notes: [], shown: PAGE_SIZE, bodies: {}, loading: true, error: undefined, actionError: undefined };
  }

  get state(): NotesViewState {
    return this.current;
  }

  private publish(change: Partial<NotesViewState>): void {
    this.current = { ...this.current, ...change };
    this.onChange(this.current);
  }

  /** Reads the notes under the current filter; a refresh keeps what is drawn (and how much) until the answer lands. */
  async load(): Promise<void> {
    const ticket = this.fetches.begin();
    const filter = this.current.filter;
    this.publish({ loading: true });
    let outcome: Partial<NotesViewState>;
    try {
      outcome = { notes: newestFirst(await this.api.filtered(filter)), error: undefined };
    } catch (e) {
      outcome = { notes: [], error: errorText(e) };
    }
    const show = this.fetches.end(ticket) && filter === this.current.filter;
    this.publish({ ...(show ? outcome : {}), loading: this.fetches.busy });
  }

  /**
   * Reads the notes again for a live event: what is drawn stays (how many, the bodies read, no `loading`), a read that
   * fails keeps the notes it had and says why in the inline error, and an answer for a filter since replaced is dropped.
   */
  async refresh(): Promise<void> {
    const ticket = this.fetches.begin();
    const filter = this.current.filter;
    let outcome: Partial<NotesViewState>;
    try {
      outcome = { notes: newestFirst(await this.api.filtered(filter)), error: undefined };
    } catch (e) {
      outcome = { error: errorText(e) };
    }
    const show = this.fetches.end(ticket) && filter === this.current.filter;
    this.publish({ ...(show ? outcome : {}), loading: this.fetches.busy && this.current.loading });
  }

  /** A new filter starts again from the first page. */
  async setFilter(filter: NotesFilter): Promise<void> {
    this.current = { ...this.current, filter, shown: PAGE_SIZE };
    await this.load();
  }

  /** Draws the next page of the notes already read. */
  loadMore(): void {
    this.publish({ shown: this.current.shown + moreCount(this.current) });
  }

  /** Posts a note from the form; on success the list is read again. Whether it was posted. */
  async post(form: NoteForm, scope: NoteScope): Promise<boolean> {
    const post = toNotePost(form, postScope(scope));
    if (!post.ok) {
      this.publish({ actionError: post.error });
      return false;
    }
    try {
      await this.api.postNote(post.body);
    } catch (e) {
      this.publish({ actionError: errorText(e) });
      return false;
    }
    this.publish({ actionError: undefined });
    await this.load();
    return true;
  }

  /** Reads the body of a note when it is expanded; a body already held is not read again. */
  async loadBody(ref: string): Promise<void> {
    if (this.current.bodies[ref] !== undefined) return;
    try {
      const read = await this.api.note(ref);
      this.publish({ bodies: { ...this.current.bodies, [ref]: read.body ?? '' } });
    } catch (e) {
      this.publish({ error: errorText(e) });
    }
  }

  clearActionError(): void {
    this.publish({ actionError: undefined });
  }
}

/** Whether a live event can change the Notes view: a note posted (the filter decides whether it shows). */
export function affectsNotes(event: { type: string }): boolean {
  return event.type.startsWith('note_');
}

/** The name of the view's saved state (PRD R16): the key is `board.view.v1.notes`. */
export const NOTES_VIEW = 'notes';

/**
 * Opens the view for the fragment it is on, before its first fetch, and returns the filter to start with. A fragment
 * with a query wins and is saved; a bare one (`#/notes`) restores the saved query by rewriting the fragment in place, so
 * the view reads `GET /notes` once, under the restored filter.
 */
export function openNotes(env: ViewEnv, hash: string): NotesFilter {
  const at = hash.indexOf('?');
  const own = at < 0 ? '' : hash.slice(at + 1);
  return filterFromQuery(openView(env, NOTES_VIEW, '#/notes', own));
}

/** Saves the filter's query after a change (the empty filter is the default and removes the key). */
export function saveNotes(storage: ViewStorage | undefined, filter: NotesFilter): void {
  saveViewQuery(storage, NOTES_VIEW, filterToQuery(filter));
}

/** Clear filters: forgets the saved query. */
export function forgetNotes(storage: ViewStorage | undefined): void {
  clearViewQuery(storage, NOTES_VIEW);
}
