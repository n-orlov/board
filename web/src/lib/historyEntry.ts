// Per history entry state (Back and Forward return to the same view): every entry of the page's session history gets
// a key in `history.state`, and what a view wants back when that entry is shown again (the page's scroll position, the
// Timeline's row scroll) is kept in sessionStorage under that key. What a link should carry (filters, sort, the
// Timeline's window) lives in the fragment's query instead; this is for what only makes sense in this tab's history.
//
// - The browser's own scroll restoration is off (`history.scrollRestoration = 'manual'`): the views draw after their
//   data loads, which is too late for it. `installScrollRestore` saves the page's scroll per entry as it moves and puts
//   it back once the page is tall enough, holding it while the view draws, unless the user scrolls first. A new entry
//   (a link followed) starts at the top.
// - Every fragment rewrite must keep the entry's state: use `replaceHash`, never `history.replaceState(null, …)`.
// - A storage or history that throws means "nothing to restore": navigation never breaks on it.

/** Every sessionStorage key this module owns starts with this. */
export const ENTRY_KEY_FAMILY = 'vh.entry.';
/** At most this many entries are remembered; older ones are dropped first. */
export const ENTRY_LIMIT = 200;
/**
 * How long a restore holds the saved position while the page draws: it waits this long for the page to grow tall
 * enough (then scrolls as far as it can), and puts the position back whenever the page redraws meanwhile.
 */
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

/** The slice of `Window` that `pushHash` uses. */
export interface PushWindow {
  readonly history: EntryHistory & { pushState: (data: unknown, unused: string, url?: string) => void };
  readonly sessionStorage?: EntryStorage;
  dispatchEvent: (event: Event) => boolean;
}

/**
 * Opens a new history entry at `hash` from script (a dialog that Back should close). The new entry starts as a copy of
 * the current one's saved values (scroll, row scroll), since it shows the same view, and carries `extra` in its state.
 * A script push fires neither popstate nor hashchange, so both are dispatched: the scroll restore and the router react
 * as they would to a link.
 */
export function pushHash(hash: string, extra: Record<string, unknown> = {}, win: PushWindow = window, make: () => string = newKey, now: () => number = Date.now): void {
  const key = make();
  copyEntry(sessionStore(win), keyOf(win.history.state), key, now);
  win.history.pushState({ ...extra, boardEntry: key }, '', hash);
  // Plain events: the listeners here read `history.state` and `location`, not the event's fields.
  win.dispatchEvent(new Event('popstate'));
  win.dispatchEvent(new Event('hashchange'));
}

/** Starts the entry `key` with a copy of the values saved for the entry `from` (when there are any). */
function copyEntry(storage: EntryStorage | undefined, from: string | undefined, key: string, now: () => number): void {
  if (storage === undefined || from === undefined) return;
  const copied = readRecord(storage, from);
  if (copied === undefined) return;
  try {
    storage.setItem(ENTRY_KEY_FAMILY + key, JSON.stringify({ t: now(), values: copied.values }));
    trim(storage);
  } catch {
    // A full quota: the new entry starts with nothing saved.
  }
}

/** Whether a parsed value is a record this module wrote. */
function isRecord(value: unknown): value is EntryRecord {
  if (typeof value !== 'object' || value === null) return false;
  const { t, values } = value as Partial<EntryRecord>;
  return typeof t === 'number' && typeof values === 'object' && values !== null;
}

/** The stored text under `key`, or null where there is none or reading it throws. */
function readRaw(storage: EntryStorage | undefined, key: string): string | null {
  try {
    return storage?.getItem(ENTRY_KEY_FAMILY + key) ?? null;
  } catch {
    return null;
  }
}

/** The parsed value of `raw`, or undefined when it is not JSON. */
function parseJSON(raw: string): unknown {
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return undefined;
  }
}

function readRecord(storage: EntryStorage | undefined, key: string): EntryRecord | undefined {
  const raw = readRaw(storage, key);
  const value = raw === null ? undefined : parseJSON(raw);
  return isRecord(value) ? { t: value.t, values: value.values } : undefined;
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

// The entry a Back, a Forward or a reload has put on screen (it already had a key), or undefined while the page shows
// an entry reached by a link. A view opening on a bare fragment asks: a revisited entry is shown as it was left.
let revisitedKey: string | undefined;

/**
 * Whether the current entry is one being revisited (Back, Forward or reload) rather than reached by a new navigation.
 * A revisited entry's own fragment is its state: a bare one means the defaults, not whatever a later visit saved.
 */
export function entryRevisited(history: { readonly state?: unknown } = window.history): boolean {
  return revisitedKey !== undefined && keyOf(history.state) === revisitedKey;
}

/** The name the page's scroll position is saved under. */
export const SCROLL = 'scroll';

/** A saved scroll position, or undefined for anything else. */
export function scrollOf(value: unknown): number | undefined {
  return Number.isFinite(value) && (value as number) >= 0 ? (value as number) : undefined;
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
  revisitedKey = keyOf(win.history.state);
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

  // Scrolls to `y` once the page is tall enough for it, and keeps it there for RESTORE_WAIT_MS: Back and Forward land
  // while the view left may still be drawn (tall enough at once), and the view shown next first draws short while its
  // data loads, which pulls the scroll up. So whenever the page's height changes, `y` is put back as soon as the page
  // is tall enough again; a page that never gets tall enough ends as far down as it goes. Anything the user does to
  // scroll meanwhile wins and ends it. A restore cut short by navigating away (`stopRestore`) saves nothing: the entry
  // keeps the position it was restoring.
  const restore = (y: number) => {
    stopRestore();
    const started = Date.now();
    let raf = 0;
    let placedAt = -1; // the page height when the scroll was last put at `y`
    const tick = () => {
      const height = win.document.documentElement.scrollHeight;
      const room = height - win.innerHeight;
      if (room >= y && height !== placedAt) {
        win.scrollTo(0, y);
        placedAt = height;
      }
      if (Date.now() - started > RESTORE_WAIT_MS) {
        if (placedAt !== height) win.scrollTo(0, Math.max(0, room));
        done();
        return;
      }
      raf = win.requestAnimationFrame(tick);
    };
    const events = ['wheel', 'touchstart', 'keydown', 'mousedown'] as const;
    const end = () => {
      win.cancelAnimationFrame(raf);
      for (const e of events) win.removeEventListener(e, done, true);
      if (restoring === end) restoring = undefined;
    };
    const done = () => {
      end();
      save();
    };
    for (const e of events) win.addEventListener(e, done, { capture: true, passive: true });
    restoring = end;
    raf = win.requestAnimationFrame(tick);
  };

  // A popstate is a link followed (a new entry, no key yet: start at the top) or Back/Forward to an entry that has
  // one (go back to where it was). The entry being left first keeps the scroll it has now: a scroll whose frame has
  // not come yet must neither be lost nor land under the next entry's key.
  const onNavigate = () => {
    win.cancelAnimationFrame(frame);
    save();
    stopRestore();
    const had = keyOf(win.history.state);
    key = ensureKey(win.history);
    revisitedKey = had === undefined ? undefined : key;
    const saved = had === undefined ? undefined : scrollOf(loadEntry(sessionStore(win), key, SCROLL));
    if (saved === undefined) {
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
