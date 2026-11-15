// Per history entry state (Back and Forward return to the same view): every entry of the page's session history gets
// a key in `history.state`, and what a view wants back when that entry is shown again (the page's scroll position, the
// Timeline's row scroll) is kept in sessionStorage under that key. What a link should carry (filters, sort, the
// Timeline's window) lives in the fragment's query instead; this is for what only makes sense in this tab's history.
//
// - The browser's own scroll restoration is off (`history.scrollRestoration = 'manual'`): the views draw after their
//   data loads, which is too late for it. `installScrollRestore` saves the page's scroll per entry as it moves and puts
//   it back once the page is tall enough, unless the user scrolls first. A new entry (a link followed) starts at the top.
// - Every fragment rewrite must keep the entry's state: use `replaceHash`, never `history.replaceState(null, …)`.
// - A storage or history that throws means "nothing to restore": navigation never breaks on it.

/** Every sessionStorage key this module owns starts with this. */
export const ENTRY_KEY_FAMILY = 'board.entry.';
/** At most this many entries are remembered; older ones are dropped first. */
export const ENTRY_LIMIT = 200;
/** How long a restore waits for the page to grow tall enough before it gives up and scrolls as far as it can. */
export const RESTORE_WAIT_MS = 4000;

/** The slice of `History` this module uses: tests pass a fake. */
export interface EntryHistory {
  readonly state: unknown;
  replaceState: (data: unknown, unused: string, url?: string) => void;
  scrollRestoration?: 'auto' | 'manual';
}

/** The slice of `Storage` this module uses. */
export interface EntryStorage {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
  removeItem: (key: string) => void;
}

interface EntryRecord {
  /** When the record was last written (ms), for dropping the oldest. */
  t: number;
  values: Record<string, unknown>;
}

/** The entry key `history.state` carries, or undefined when it carries none. */
export function keyOf(state: unknown): string | undefined {
  if (typeof state !== 'object' || state === null) return undefined;
  const key = (state as { boardEntry?: unknown }).boardEntry;
  return typeof key === 'string' && key !== '' ? key : undefined;
}

/** A fresh entry key. */
export function newKey(random: () => number = Math.random, now: () => number = Date.now): string {
  return `${now().toString(36)}-${Math.floor(random() * 36 ** 6).toString(36)}`;
}

/** The current entry's key, giving the entry one first (in place, no new entry) when it has none. */
export function ensureKey(history: EntryHistory, make: () => string = newKey): string {
  const had = keyOf(history.state);
  if (had !== undefined) return had;
  const key = make();
  const base = typeof history.state === 'object' && history.state !== null ? history.state : {};
  try {
    history.replaceState({ ...base, boardEntry: key }, '');
  } catch {
    // A history that refuses (a sandboxed frame): the key just does not stick.
  }
  return key;
}

/** Rewrites the fragment in place and keeps the entry's state (its key): no history entry, no hashchange. */
export function replaceHash(hash: string, history: EntryHistory = window.history): void {
  history.replaceState(history.state ?? null, '', hash);
}

/** Whether a parsed value is a record this module wrote. */
function isRecord(value: unknown): value is EntryRecord {
  if (typeof value !== 'object' || value === null) return false;
  const { t, values } = value as Partial<EntryRecord>;
  return typeof t === 'number' && typeof values === 'object' && values !== null;
}

function readRecord(storage: EntryStorage | undefined, key: string): EntryRecord | undefined {
  try {
    const raw = storage?.getItem(ENTRY_KEY_FAMILY + key) ?? null;
    if (raw === null) return undefined;
    const value: unknown = JSON.parse(raw);
    return isRecord(value) ? { t: value.t, values: value.values } : undefined;
  } catch {
    return undefined;
  }
}

/** What was saved under `name` for the entry `key`, or undefined. */
export function loadEntry(storage: EntryStorage | undefined, key: string, name: string): unknown {
  return readRecord(storage, key)?.values[name];
}

/** Saves `value` under `name` for the entry `key`. A throwing storage is ignored. */
export function saveEntry(storage: EntryStorage | undefined, key: string, name: string, value: unknown, now: () => number = Date.now): void {
  if (storage === undefined) return;
  const record = readRecord(storage, key) ?? { t: 0, values: {} };
  record.t = now();
  record.values[name] = value;
  try {
    storage.setItem(ENTRY_KEY_FAMILY + key, JSON.stringify(record));
    trim(storage);
  } catch {
    // A full quota: this entry just does not remember.
  }
}

/** The keys this module holds in a full Storage (one that can list its keys). */
function ownKeys(storage: EntryStorage): string[] {
  const listing = storage as Partial<Pick<Storage, 'key' | 'length'>>;
  if (typeof listing.key !== 'function' || typeof listing.length !== 'number') return [];
  const keys: string[] = [];
  for (let i = 0; i < listing.length; i++) {
    const k = listing.key(i);
    if (k !== null && k.startsWith(ENTRY_KEY_FAMILY)) keys.push(k);
  }
  return keys;
}

/** Drops the oldest records past `ENTRY_LIMIT`. */
function trim(storage: EntryStorage): void {
  const keys = ownKeys(storage);
  if (keys.length <= ENTRY_LIMIT) return;
  const dated = keys.map((k) => ({ k, t: readRecord(storage, k.slice(ENTRY_KEY_FAMILY.length))?.t ?? 0 })).sort((a, b) => a.t - b.t);
  for (const { k } of dated.slice(0, keys.length - ENTRY_LIMIT)) storage.removeItem(k);
}

/** The session storage of the page, or undefined where reading it throws. */
export function sessionStore(win: { readonly sessionStorage?: EntryStorage } = window): EntryStorage | undefined {
  try {
    return win.sessionStorage;
  } catch {
    return undefined;
  }
}

/** The current entry's saved value under `name` (the entry gets a key first if it has none). */
export function loadCurrent(name: string, win: Window = window): unknown {
  return loadEntry(sessionStore(win), ensureKey(win.history), name);
}

/** Saves `value` under `name` for the current entry. */
export function saveCurrent(name: string, value: unknown, win: Window = window): void {
  saveEntry(sessionStore(win), ensureKey(win.history), name, value);
}

/** The name the page's scroll position is saved under. */
export const SCROLL = 'scroll';

/** A saved scroll position, or undefined for anything else. */
export function scrollOf(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : undefined;
}

/**
 * Saves the page's scroll per history entry and restores it on Back and Forward. Returns the function that removes
 * its listeners. Call once, before the first view draws.
 */
export function installScrollRestore(win: Window = window): () => void {
  try {
    win.history.scrollRestoration = 'manual';
  } catch {
    // Not settable here: the browser's own restoration stays, which is no worse than before.
  }
  let key = ensureKey(win.history);
  let restoring: (() => void) | undefined;
  let frame = 0;

  const stopRestore = () => {
    restoring?.();
    restoring = undefined;
  };

  const save = () => {
    frame = 0;
    if (restoring === undefined) saveEntry(sessionStore(win), key, SCROLL, Math.round(win.scrollY));
  };
  const onScroll = () => {
    if (frame === 0) frame = win.requestAnimationFrame(save);
  };

  // Scrolls to `y` once the page is tall enough for it (the view's data has landed), or after RESTORE_WAIT_MS as far
  // as it can. Anything the user does to scroll meanwhile wins and ends the wait.
  const restore = (y: number) => {
    stopRestore();
    const started = Date.now();
    let raf = 0;
    const tick = () => {
      const room = win.document.documentElement.scrollHeight - win.innerHeight;
      if (room >= y || Date.now() - started > RESTORE_WAIT_MS) {
        win.scrollTo(0, Math.min(y, Math.max(0, room)));
        finish();
        return;
      }
      raf = win.requestAnimationFrame(tick);
    };
    const userMoved = () => finish();
    const events = ['wheel', 'touchstart', 'keydown', 'mousedown'] as const;
    const finish = () => {
      win.cancelAnimationFrame(raf);
      for (const e of events) win.removeEventListener(e, userMoved, true);
      if (restoring === finish) restoring = undefined;
      save();
    };
    for (const e of events) win.addEventListener(e, userMoved, { capture: true, passive: true });
    restoring = finish;
    raf = win.requestAnimationFrame(tick);
  };

  // A hashchange is a link followed (a new entry, no key yet: start at the top) or Back/Forward to an entry that has
  // one (go back to where it was).
  const onNavigate = () => {
    const had = keyOf(win.history.state);
    key = ensureKey(win.history);
    const saved = had === undefined ? undefined : scrollOf(loadEntry(sessionStore(win), key, SCROLL));
    if (saved === undefined) {
      stopRestore();
      win.scrollTo(0, 0);
    } else {
      restore(saved);
    }
  };

  win.addEventListener('scroll', onScroll, { passive: true });
  win.addEventListener('popstate', onNavigate);
  // A reload lands on an entry that already has a key: put its scroll back too.
  const first = scrollOf(loadEntry(sessionStore(win), key, SCROLL));
  if (first !== undefined && first > 0) restore(first);
  return () => {
    stopRestore();
    win.removeEventListener('scroll', onScroll);
    win.removeEventListener('popstate', onNavigate);
  };
}
