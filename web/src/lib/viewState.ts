// Saved view state (PRD R16): each view keeps its whole fragment query (filters and sort) in localStorage under one
// versioned key, `board.view.v1.<view>`, so leaving a view and coming back restores it.
//
// - A route opened WITH a query uses it and saves it (a shared link wins).
// - A bare route restores the saved query by rewriting the fragment in place (`history.replaceState`, no history
//   entry, no hashchange) before the view's first fetch, so only one request is made.
// - Clear filters removes the key; signing out removes every `board.view.*` key.
// - A missing, malformed, foreign-version or unreadable value, and a storage that throws, mean "defaults": the view
//   never breaks on its storage.

/** The version of the stored value; a stored value of another version is ignored. */
export const VIEW_STATE_VERSION = 1;
/** Every key this module owns starts with this; sign out removes them all. */
export const VIEW_KEY_FAMILY = 'board.view.';
/** The key of the current version, followed by the view's name. */
export const VIEW_KEY_PREFIX = `${VIEW_KEY_FAMILY}v${VIEW_STATE_VERSION}.`;

/** The slice of `Storage` this module uses: tests pass a fake. */
export interface ViewStorage {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
  removeItem: (key: string) => void;
  key: (index: number) => string | null;
  readonly length: number;
}

/** What opening a view needs from the browser. */
export interface ViewEnv {
  storage: ViewStorage | undefined;
  /** Rewrites the page's fragment in place (history.replaceState): no history entry, no hashchange. */
  replaceHash: (hash: string) => void;
}

/** The storage key of a view. */
export function viewKey(view: string): string {
  return VIEW_KEY_PREFIX + view;
}

/** A query without its leading `?`; the empty string is "no query". */
function bare(query: string): string {
  return query.startsWith('?') ? query.slice(1) : query;
}

/** The query a stored value holds, or null for a value that is malformed or of another version. */
export function parseStored(raw: string | null): string | null {
  if (raw === null) return null;
  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) return null;
    const { v, query } = value as { v?: unknown; query?: unknown };
    if (v !== VIEW_STATE_VERSION || typeof query !== 'string' || query === '') return null;
    return query;
  } catch {
    return null;
  }
}

/** The saved query of a view; null when there is none or it cannot be used. */
export function loadViewQuery(storage: ViewStorage | undefined, view: string): string | null {
  try {
    return storage ? parseStored(storage.getItem(viewKey(view))) : null;
  } catch {
    return null;
  }
}

/** Saves a view's whole query; an empty query is the default and removes the key. A throwing storage is ignored. */
export function saveViewQuery(storage: ViewStorage | undefined, view: string, query: string): void {
  const q = bare(query);
  try {
    if (!storage) return;
    if (q === '') storage.removeItem(viewKey(view));
    else storage.setItem(viewKey(view), JSON.stringify({ v: VIEW_STATE_VERSION, query: q }));
  } catch {
    // Private mode or a full quota: the view works, it just does not remember.
  }
}

/** Clear filters: forgets a view's saved query. */
export function clearViewQuery(storage: ViewStorage | undefined, view: string): void {
  saveViewQuery(storage, view, '');
}

/** Sign out: removes every `board.view.*` key (every version) and leaves other keys alone. */
export function clearAllViews(storage: ViewStorage | undefined): void {
  try {
    if (!storage) return;
    const keys: string[] = [];
    for (let i = 0; i < storage.length; i++) {
      const key = storage.key(i);
      if (key !== null && key.startsWith(VIEW_KEY_FAMILY)) keys.push(key);
    }
    for (const key of keys) storage.removeItem(key);
  } catch {
    // Nothing more can be done with a storage that throws.
  }
}

/**
 * Opens a view and returns the query it starts with. `path` is the view's fragment without a query (`#/tasks`),
 * `query` the one the fragment carries now. With a query, that query wins and is saved. Without one, the saved
 * query (if usable) is written into the fragment by `replaceHash` and returned; else the view starts at its
 * defaults (the empty string). Call it before the view's first fetch.
 */
export function openView(env: ViewEnv, view: string, path: string, query: string): string {
  const q = bare(query);
  if (q !== '') {
    saveViewQuery(env.storage, view, q);
    return q;
  }
  const saved = loadViewQuery(env.storage, view);
  if (saved === null) return '';
  env.replaceHash(`${path}?${saved}`);
  return saved;
}

/** The slice of `window` the browser adapter reads. */
export interface ViewWindow {
  history: { readonly state?: unknown; replaceState: (data: unknown, unused: string, url: string) => void };
  readonly localStorage?: ViewStorage;
}

/** The storage of the page, or undefined where reading it throws (blocked storage). */
export function pageStorage(win: Pick<ViewWindow, 'localStorage'> = window): ViewStorage | undefined {
  try {
    return win.localStorage;
  } catch {
    return undefined;
  }
}

/** The view environment of this page. */
export function currentViewEnv(win: ViewWindow = window): ViewEnv {
  return {
    storage: pageStorage(win),
    replaceHash: (hash) => win.history.replaceState(win.history.state ?? null, '', hash),
  };
}

/**
 * Orders a view's filter changes. The first read waits while a restored filter's board references are checked, and
 * a link opened, a filter applied or Clear filters while it waits begins a newer change. Each change asks `begin()`
 * for a check and applies its answer only while that check is true, so a restoration that answers late never
 * rewrites an explicit query (or a choice made since) as restored state, and never reads under it. Leaving the view
 * calls `end()`, after which no check is true: a late answer never touches the view navigated to.
 */
export class FilterTurns {
  private turn = 0;
  private read = false;
  private ended = false;

  /** Begins a change: the check it returns stays true until a later change begins or the view ends. */
  begin(): () => boolean {
    const mine = ++this.turn;
    return () => !this.ended && mine === this.turn;
  }

  /**
   * Ends the view's turns when it is left (destroyed): a check still running then answers for a view no longer shown,
   * so it must neither rewrite the fragment (now another view's, perhaps an explicit link) nor save nor read.
   */
  end(): void {
    this.ended = true;
    this.turn++;
  }

  /** Records that the view has read under some filter. */
  reading(): void {
    this.read = true;
  }

  /** Whether the view has read yet: a live refresh before then would read under a filter still being checked. */
  hasRead(): boolean {
    return this.read;
  }

  /** Whether a change must read: always before the first read, else only for a filter other than the one shown. */
  mustRead(sameAsShown: boolean): boolean {
    return !this.read || !sameAsShown;
  }
}
