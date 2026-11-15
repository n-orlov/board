import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  ENTRY_KEY_FAMILY,
  ENTRY_LIMIT,
  ensureKey,
  installScrollRestore,
  RESTORE_WAIT_MS,
  SCROLL,
  keyOf,
  loadCurrent,
  loadEntry,
  newKey,
  pushHash,
  replaceHash,
  saveCurrent,
  saveEntry,
  sessionStore,
  scrollOf,
  type EntryHistory,
  type EntryStorage,
} from './historyEntry';

function fakeStorage() {
  const data: Record<string, string> = {};
  const storage: EntryStorage & { data: Record<string, string>; key(i: number): string | null; readonly length: number } = {
    data,
    getItem: (k) => data[k] ?? null,
    setItem: (k, v) => {
      data[k] = v;
    },
    removeItem: (k) => {
      delete data[k];
    },
    key: (i) => Object.keys(data)[i] ?? null,
    get length() {
      return Object.keys(data).length;
    },
  };
  return storage;
}

function fakeHistory(state: unknown = null) {
  const calls: { data: unknown; unused: string; url?: string }[] = [];
  const history: EntryHistory & { calls: typeof calls } = {
    state,
    calls,
    replaceState(data, unused, url) {
      calls.push({ data, unused, url });
      (history as { state: unknown }).state = data;
    },
  };
  return history;
}

describe('keyOf', () => {
  it('reads the entry key from history.state', () => {
    expect(keyOf({ boardEntry: 'k1' })).toBe('k1');
  });
  it('is undefined for no state, another shape or an empty key', () => {
    expect(keyOf(null)).toBeUndefined();
    expect(keyOf('k1')).toBeUndefined();
    expect(keyOf({ boardEntry: 3 })).toBeUndefined();
    expect(keyOf({ boardEntry: '' })).toBeUndefined();
    expect(keyOf(Object.assign(() => undefined, { boardEntry: 'k1' }))).toBeUndefined();
  });
});

describe('newKey', () => {
  it('is built from the time and a random part', () => {
    expect(newKey(() => 0.5, () => 36)).toBe(`10-${Math.floor(0.5 * 36 ** 6).toString(36)}`);
  });
});

describe('ensureKey', () => {
  it('keeps a key the entry already has', () => {
    const h = fakeHistory({ boardEntry: 'old' });
    expect(ensureKey(h, () => 'new')).toBe('old');
    expect(h.calls).toEqual([]);
  });
  it('gives a keyless entry one in place, keeping the rest of its state', () => {
    const h = fakeHistory({ other: 1 });
    expect(ensureKey(h, () => 'new')).toBe('new');
    expect(h.state).toEqual({ other: 1, boardEntry: 'new' });
    expect(h.calls[0]?.url).toBeUndefined();
    expect(h.calls[0]?.unused).toBe('');
  });
  it('starts from an empty state when the entry carries a state that is not an object', () => {
    const h = fakeHistory('text');
    expect(ensureKey(h, () => 'new')).toBe('new');
    expect(h.state).toEqual({ boardEntry: 'new' });
  });
  it('still returns a key when the history refuses', () => {
    const h: EntryHistory = {
      state: null,
      replaceState: () => {
        throw new Error('SecurityError');
      },
    };
    expect(ensureKey(h, () => 'k')).toBe('k');
  });
});

describe('replaceHash', () => {
  it('rewrites the fragment and keeps the entry key', () => {
    const h = fakeHistory({ boardEntry: 'k' });
    replaceHash('#/tasks?q=x', h);
    expect(h.calls).toEqual([{ data: { boardEntry: 'k' }, unused: '', url: '#/tasks?q=x' }]);
  });
});

describe('saveEntry and loadEntry', () => {
  it('keep values per entry and per name', () => {
    const s = fakeStorage();
    saveEntry(s, 'a', 'scroll', 120, () => 1);
    saveEntry(s, 'a', 'rows', 40, () => 2);
    saveEntry(s, 'b', 'scroll', 7, () => 3);
    expect(loadEntry(s, 'a', 'scroll')).toBe(120);
    expect(loadEntry(s, 'a', 'rows')).toBe(40);
    expect(loadEntry(s, 'b', 'scroll')).toBe(7);
    expect(loadEntry(s, 'c', 'scroll')).toBeUndefined();
  });
  it('read a corrupt record as nothing saved', () => {
    const s = fakeStorage();
    s.setItem(`${ENTRY_KEY_FAMILY}a`, '{not json');
    expect(loadEntry(s, 'a', 'scroll')).toBeUndefined();
    s.setItem(`${ENTRY_KEY_FAMILY}a`, JSON.stringify({ t: 'x', values: {} }));
    expect(loadEntry(s, 'a', 'scroll')).toBeUndefined();
    for (const raw of ['null', '7', '"text"', JSON.stringify({ t: 1, values: null }), JSON.stringify({ t: 1, values: 3 }), JSON.stringify({ values: {} })]) {
      s.setItem(`${ENTRY_KEY_FAMILY}a`, raw);
      expect(loadEntry(s, 'a', 'scroll'), raw).toBeUndefined();
    }
    s.setItem(`${ENTRY_KEY_FAMILY}a`, JSON.stringify({ t: 1, values: { scroll: 5 } }));
    expect(loadEntry(s, 'a', 'scroll')).toBe(5);
  });
  it('reads a record without a numeric time or without an object of values as nothing saved', () => {
    const s = fakeStorage();
    const bad = [JSON.stringify({ values: { scroll: 5 } }), JSON.stringify({ t: 'x', values: { scroll: 5 } }), JSON.stringify({ t: 1, values: 'abc' })];
    for (const raw of bad) {
      s.setItem(`${ENTRY_KEY_FAMILY}a`, raw);
      expect(loadEntry(s, 'a', 'scroll'), raw).toBeUndefined();
      expect(loadEntry(s, 'a', 'length'), raw).toBeUndefined();
    }
  });
  it('drop the oldest entries past the limit', () => {
    const s = fakeStorage();
    for (let i = 0; i <= ENTRY_LIMIT; i++) saveEntry(s, `k${i}`, 'scroll', i, () => i);
    expect(Object.keys(s.data)).toHaveLength(ENTRY_LIMIT);
    expect(loadEntry(s, 'k0', 'scroll')).toBeUndefined();
    expect(loadEntry(s, `k${ENTRY_LIMIT}`, 'scroll')).toBe(ENTRY_LIMIT);
  });
  it('drop the entry written longest ago whatever its place in the storage, and keep keys it does not own', () => {
    const s = fakeStorage();
    for (let i = 0; i < 100; i++) s.setItem(`other.${i}`, 'x');
    for (let i = 0; i < ENTRY_LIMIT; i++) s.setItem(`${ENTRY_KEY_FAMILY}k${i}`, JSON.stringify({ t: 1000 - i, values: {} }));
    saveEntry(s, 'new', 'scroll', 1, () => 5000);
    expect(s.getItem(`${ENTRY_KEY_FAMILY}k${ENTRY_LIMIT - 1}`)).toBeNull();
    expect(Object.keys(s.data).filter((k) => k.startsWith(ENTRY_KEY_FAMILY))).toHaveLength(ENTRY_LIMIT);
    expect(Object.keys(s.data).filter((k) => k.startsWith('other.'))).toHaveLength(100);
    expect(loadEntry(s, 'new', 'scroll')).toBe(1);
  });
  it('drop a record it cannot read before any it can', () => {
    const s = fakeStorage();
    for (let i = 0; i < ENTRY_LIMIT - 1; i++) s.setItem(`${ENTRY_KEY_FAMILY}k${i}`, JSON.stringify({ t: 1 + i, values: {} }));
    s.setItem(`${ENTRY_KEY_FAMILY}bad`, '{not json');
    saveEntry(s, 'new', 'scroll', 1, () => 5000);
    expect(s.getItem(`${ENTRY_KEY_FAMILY}bad`)).toBeNull();
    expect(s.getItem(`${ENTRY_KEY_FAMILY}k0`)).not.toBeNull();
    expect(Object.keys(s.data)).toHaveLength(ENTRY_LIMIT);
  });
  it('ignore a missing or throwing storage', () => {
    const boom: EntryStorage = {
      getItem: () => {
        throw new Error('SecurityError');
      },
      setItem: () => {
        throw new Error('QuotaExceeded');
      },
      removeItem: () => undefined,
    };
    expect(() => saveEntry(boom, 'a', 'scroll', 1)).not.toThrow();
    expect(loadEntry(boom, 'a', 'scroll')).toBeUndefined();
    expect(() => saveEntry(undefined, 'a', 'scroll', 1)).not.toThrow();
    expect(loadEntry(undefined, 'a', 'scroll')).toBeUndefined();
  });
});

describe('scrollOf', () => {
  it('accepts a finite non-negative number only', () => {
    expect(scrollOf(10)).toBe(10);
    expect(scrollOf(0)).toBe(0);
    expect(scrollOf(-1)).toBeUndefined();
    expect(scrollOf(Number.NaN)).toBeUndefined();
    expect(scrollOf('10')).toBeUndefined();
  });
});

type Listener = (e?: unknown) => void;

/** A page whose height, scroll and animation frames the test drives; `popTo` stands for Back/Forward or a followed link. */
function fakePage(state: unknown = null) {
  const history = fakeHistory(state) as EntryHistory & { calls: unknown[] };
  const storage = fakeStorage();
  const listeners: Record<string, Listener[]> = {};
  let frames: { id: number; cb: () => void }[] = [];
  let nextId = 1;
  const scrolls: number[] = [];
  const added: { name: string; options: unknown }[] = [];
  const removed: { name: string; options: unknown }[] = [];
  let requested = 0;
  const page = {
    added,
    removed,
    get requested() {
      return requested;
    },
    scrollY: 0,
    height: 5000,
    innerHeight: 800,
    scrolls,
    storage,
    history,
    document: {
      documentElement: {
        get scrollHeight() {
          return page.height;
        },
      },
    },
    sessionStorage: storage,
    addEventListener(name: string, fn: Listener, options?: unknown) {
      added.push({ name, options });
      (listeners[name] ??= []).push(fn);
    },
    removeEventListener(name: string, fn: Listener, options?: unknown) {
      removed.push({ name, options });
      listeners[name] = (listeners[name] ?? []).filter((f) => f !== fn);
    },
    requestAnimationFrame(cb: () => void) {
      requested++;
      frames.push({ id: nextId, cb });
      return nextId++;
    },
    cancelAnimationFrame(id: number) {
      frames = frames.filter((f) => f.id !== id);
    },
    scrollTo(_x: number, y: number) {
      page.scrollY = y;
      scrolls.push(y);
    },
    listening: (name: string) => (listeners[name] ?? []).length,
    emit(name: string) {
      for (const fn of [...(listeners[name] ?? [])]) fn();
    },
    /** Runs the animation frames queued so far. */
    frame() {
      const due = frames;
      frames = [];
      for (const f of due) f.cb();
    },
    /** The user moves to another history entry: `state` is what the browser reports for it. */
    popTo(state: unknown) {
      (history as { state: unknown }).state = state;
      page.emit('popstate');
    },
  };
  return page;
}

function install(page: ReturnType<typeof fakePage>) {
  return installScrollRestore(page as unknown as Window);
}

describe('installScrollRestore', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('turns the browser restoration off and gives the first entry a key', () => {
    const page = fakePage();
    install(page);
    expect(page.history.scrollRestoration).toBe('manual');
    expect(keyOf(page.history.state)).toBeDefined();
  });

  it('saves the page scroll under the entry key as it moves', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    page.scrollY = 640.4;
    page.emit('scroll');
    page.frame();
    expect(loadEntry(page.storage, 'a', SCROLL)).toBe(640);
  });

  it('puts the saved scroll back on Back to an entry that has a key', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.popTo({ boardEntry: 'b' });
    page.frame();
    expect(page.scrolls).toEqual([1200]);
    expect(page.scrollY).toBe(1200);
  });

  it('puts the saved scroll back on a reload of an entry that has a key', () => {
    const page = fakePage({ boardEntry: 'a' });
    saveEntry(page.storage, 'a', SCROLL, 900);
    install(page);
    page.frame();
    expect(page.scrollY).toBe(900);
  });

  it('waits until the page is tall enough before scrolling', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.height = 900; // room 100: the view has not drawn its rows yet
    page.popTo({ boardEntry: 'b' });
    page.frame();
    page.frame();
    expect(page.scrolls).toEqual([]);
    page.height = 2500; // room 1700
    page.frame();
    expect(page.scrolls).toEqual([1200]);
  });

  it('gives up after 4 s and scrolls as far as the page goes when it never draws tall enough', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.height = 1000; // room 200, never enough
    page.popTo({ boardEntry: 'b' });
    page.frame();
    vi.setSystemTime(RESTORE_WAIT_MS);
    page.frame();
    expect(page.scrolls).toEqual([]); // 4 s exactly is still waiting
    vi.setSystemTime(RESTORE_WAIT_MS + 1);
    page.frame();
    expect(page.scrolls).toEqual([200]);
    page.frame();
    expect(page.scrolls).toEqual([200]); // and the wait is over: no further scrolling
  });

  it('lets a scroll the user made first win over the restore', () => {
    for (const gesture of ['wheel', 'touchstart', 'keydown', 'mousedown']) {
      const page = fakePage({ boardEntry: 'a' });
      install(page);
      saveEntry(page.storage, 'b', SCROLL, 1200);
      page.height = 900;
      page.popTo({ boardEntry: 'b' });
      page.frame();
      page.emit(gesture);
      page.height = 5000; // the page grows tall enough afterwards
      page.frame();
      expect(page.scrolls, gesture).toEqual([]);
      expect(page.listening(gesture), gesture).toBe(0);
    }
  });

  it('stops listening for the user once the restore is over', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 100);
    page.popTo({ boardEntry: 'b' });
    expect(page.listening('wheel')).toBe(1);
    page.frame();
    expect(page.scrolls).toEqual([100]);
    expect(page.listening('wheel')).toBe(1); // still holding the position while the view draws
    vi.setSystemTime(RESTORE_WAIT_MS + 1);
    page.frame();
    expect(page.listening('wheel')).toBe(0);
    page.frame();
    expect(page.scrolls).toEqual([100]);
  });

  it('puts the position back when the view shown next draws short first and then grows', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.height = 4000; // the view left is still drawn, and tall enough
    page.popTo({ boardEntry: 'b' });
    page.frame();
    expect(page.scrolls).toEqual([1200]);
    page.height = 900; // the next view draws short while its data loads: the browser pulls the scroll up
    page.scrollY = 100;
    page.emit('scroll');
    page.frame();
    expect(page.scrolls).toEqual([1200]);
    expect(loadEntry(page.storage, 'b', SCROLL)).toBe(1200);
    page.height = 3000; // its rows land
    page.frame();
    expect(page.scrolls).toEqual([1200, 1200]);
    expect(page.scrollY).toBe(1200);
    vi.setSystemTime(RESTORE_WAIT_MS + 1);
    page.frame();
    expect(page.scrolls).toEqual([1200, 1200]);
    expect(loadEntry(page.storage, 'b', SCROLL)).toBe(1200);
  });

  it('leaves a page that kept its height where it is once the position is reached', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.popTo({ boardEntry: 'b' });
    page.frame();
    page.scrollY = 500; // moved by a means no gesture event reports (the scrollbar)
    page.frame();
    vi.setSystemTime(RESTORE_WAIT_MS + 1);
    page.frame();
    expect(page.scrolls).toEqual([1200]);
    expect(loadEntry(page.storage, 'b', SCROLL)).toBe(500);
  });

  it('starts a followed link at the top and gives the new entry a key', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    page.scrollY = 700;
    page.popTo(null); // a new entry has no state yet
    expect(page.scrolls).toEqual([0]);
    expect(page.scrollY).toBe(0);
    const key = keyOf(page.history.state);
    expect(key).toBeDefined();
    expect(key).not.toBe('a');
  });

  it('starts at the top for an entry with a key but nothing saved', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    page.popTo({ boardEntry: 'fresh' });
    expect(page.scrolls).toEqual([0]);
  });

  it('a followed link ends a restore still waiting', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.height = 900;
    page.popTo({ boardEntry: 'b' });
    page.popTo(null);
    page.height = 5000;
    page.frame();
    expect(page.scrolls).toEqual([0]);
  });

  it('keeps the scroll of the entry left when a link is followed before the next frame', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    page.scrollY = 640;
    page.emit('scroll'); // its frame has not come yet
    page.popTo(null);
    const next = keyOf(page.history.state)!;
    expect(loadEntry(page.storage, 'a', SCROLL)).toBe(640);
    page.frame();
    expect(loadEntry(page.storage, next, SCROLL)).toBeUndefined();
    page.popTo({ boardEntry: 'a' });
    page.frame();
    expect(page.scrollY).toBe(640);
  });

  it('keeps the scroll of the entry left even when no scroll event came for it', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    page.scrollY = 480;
    page.popTo(null);
    expect(loadEntry(page.storage, 'a', SCROLL)).toBe(480);
  });

  it('a link followed during a restore keeps the position the entry was waiting for, under its own key only', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.height = 900;
    page.popTo({ boardEntry: 'b' });
    page.scrollY = 37;
    page.popTo(null);
    const next = keyOf(page.history.state)!;
    expect(loadEntry(page.storage, 'b', SCROLL)).toBe(1200);
    expect(loadEntry(page.storage, next, SCROLL)).toBeUndefined();
  });

  it('does not save a scroll while a restore is still waiting', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.height = 900;
    page.popTo({ boardEntry: 'b' });
    page.scrollY = 0;
    page.emit('scroll');
    page.frame();
    expect(loadEntry(page.storage, 'b', SCROLL)).toBe(1200);
  });

  it('asks for one frame however many scroll events come before it', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    const before = page.requested;
    page.emit('scroll');
    page.emit('scroll');
    page.emit('scroll');
    expect(page.requested - before).toBe(1);
    page.frame();
    page.emit('scroll');
    expect(page.requested - before).toBe(2);
  });

  it('listens to scrolling passively, and to the user\x27s gestures in the capture phase, passively', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 100);
    page.popTo({ boardEntry: 'b' });
    expect(page.added.filter((a) => a.name === 'scroll').map((a) => a.options)).toEqual([{ passive: true }]);
    for (const gesture of ['wheel', 'touchstart', 'keydown', 'mousedown']) {
      expect(page.added.filter((a) => a.name === gesture).map((a) => a.options), gesture).toEqual([{ capture: true, passive: true }]);
    }
    page.emit('wheel');
    for (const gesture of ['wheel', 'touchstart', 'keydown', 'mousedown']) {
      expect(page.removed.filter((r) => r.name === gesture).map((r) => r.options), gesture).toEqual([true]);
    }
  });

  it('ends the restore it was holding when another entry is restored', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 100);
    saveEntry(page.storage, 'c', SCROLL, 200);
    page.popTo({ boardEntry: 'b' });
    page.popTo({ boardEntry: 'c' });
    expect(page.listening('wheel')).toBe(1);
    page.frame();
    expect(page.scrolls).toEqual([200]);
  });

  it('scrolls when the page has exactly the room the position needs', () => {
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.height = page.innerHeight + 1200;
    page.popTo({ boardEntry: 'b' });
    page.frame();
    expect(page.scrolls).toEqual([1200]);
  });

  it('counts the 4 s from when the restore began, not from the epoch', () => {
    vi.setSystemTime(100_000);
    const page = fakePage({ boardEntry: 'a' });
    install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.height = 900;
    page.popTo({ boardEntry: 'b' });
    page.frame();
    vi.setSystemTime(100_000 + RESTORE_WAIT_MS);
    page.frame();
    expect(page.listening('wheel')).toBe(1);
    vi.setSystemTime(100_000 + RESTORE_WAIT_MS + 1);
    page.frame();
    expect(page.listening('wheel')).toBe(0);
  });

  it('starts a followed link at the top even when a record is saved under the key it would get', () => {
    const key = newKey(() => 0.5, () => 0);
    vi.spyOn(Math, 'random').mockReturnValue(0.5);
    try {
      const page = fakePage({ boardEntry: 'a' });
      install(page);
      saveEntry(page.storage, key, SCROLL, 900);
      page.popTo(null);
      expect(keyOf(page.history.state)).toBe(key);
      expect(page.scrolls).toEqual([0]);
    } finally {
      vi.restoreAllMocks();
    }
  });

  it('restores a reload only to a position past the top', () => {
    const page = fakePage({ boardEntry: 'a' });
    saveEntry(page.storage, 'a', SCROLL, 0);
    install(page);
    page.frame();
    expect(page.scrolls).toEqual([]);
    expect(page.listening('wheel')).toBe(0);
  });

  it('ends a restore still waiting when its listeners are removed', () => {
    const page = fakePage({ boardEntry: 'a' });
    const off = install(page);
    saveEntry(page.storage, 'b', SCROLL, 1200);
    page.height = 900;
    page.popTo({ boardEntry: 'b' });
    off();
    expect(page.listening('wheel')).toBe(0);
    page.height = 5000;
    page.frame();
    expect(page.scrolls).toEqual([]);
  });

  it('removes its listeners when asked', () => {
    const page = fakePage({ boardEntry: 'a' });
    const off = install(page);
    off();
    expect(page.listening('scroll')).toBe(0);
    expect(page.listening('popstate')).toBe(0);
  });
});

describe('the current entry', () => {
  it('saves and loads under the key of the entry shown, giving a keyless entry one', () => {
    const page = fakePage();
    saveCurrent('rows', 33, page as unknown as Window);
    const key = keyOf(page.history.state);
    expect(key).toBeDefined();
    expect(loadEntry(page.storage, key as string, 'rows')).toBe(33);
    expect(loadCurrent('rows', page as unknown as Window)).toBe(33);
    expect(loadCurrent('other', page as unknown as Window)).toBeUndefined();
  });
  it('reads another entry as nothing saved', () => {
    const page = fakePage({ boardEntry: 'a' });
    saveEntry(page.storage, 'a', 'rows', 5);
    page.popTo({ boardEntry: 'b' });
    expect(loadCurrent('rows', page as unknown as Window)).toBeUndefined();
  });
});

describe('sessionStore', () => {
  it('is the window storage, or nothing where reading it throws', () => {
    const s = fakeStorage();
    expect(sessionStore({ sessionStorage: s })).toBe(s);
    const blocked = {
      get sessionStorage(): never {
        throw new Error('SecurityError');
      },
    };
    expect(sessionStore(blocked)).toBeUndefined();
  });
});

describe('pushHash', () => {
  function fakeWindow(state: unknown) {
    const storage = fakeStorage();
    const history = Object.assign(fakeHistory(state), {
      pushed: [] as { data: unknown; url?: string }[],
      pushState(data: unknown, _unused: string, url?: string) {
        history.pushed.push({ data, url });
        (history as { state: unknown }).state = data;
      },
    });
    const events: string[] = [];
    return { storage, history, events, win: { history, sessionStorage: storage, dispatchEvent: (e: Event) => events.push(e.type) > 0 } };
  }

  it('opens a new entry carrying the extra state and a fresh key, then announces it as popstate and hashchange', () => {
    const { history, events, win } = fakeWindow({ boardEntry: 'a' });
    pushHash('#/timeline?task=T-1', { preview: true }, win, () => 'b');
    expect(history.pushed).toEqual([{ data: { preview: true, boardEntry: 'b' }, url: '#/timeline?task=T-1' }]);
    expect(events).toEqual(['popstate', 'hashchange']);
  });

  it('starts the new entry with the current entry saved values, so its scroll is the same', () => {
    const { storage, win } = fakeWindow({ boardEntry: 'a' });
    saveEntry(storage, 'a', 'scroll', 420);
    saveEntry(storage, 'a', 'timelineRows', 80);
    pushHash('#/x', {}, win, () => 'b', () => 5);
    expect(loadEntry(storage, 'b', 'scroll')).toBe(420);
    expect(loadEntry(storage, 'b', 'timelineRows')).toBe(80);
    expect(loadEntry(storage, 'a', 'scroll')).toBe(420);
  });

  it('pushes without copying when the current entry has no key or nothing saved', () => {
    const { storage, history, win } = fakeWindow(null);
    pushHash('#/x', {}, win, () => 'b');
    expect(storage.data).toEqual({});
    expect(keyOf(history.state)).toBe('b');
  });

  it('records what it pushes with an empty unused title', () => {
    const { history, win } = fakeWindow({ boardEntry: 'a' });
    const titles: string[] = [];
    const push = history.pushState;
    history.pushState = (data, unused, url) => {
      titles.push(unused);
      push(data, unused, url);
    };
    pushHash('#/x', {}, win, () => 'b');
    expect(titles).toEqual(['']);
  });

  it('copies nothing from a keyless entry, whatever is saved under the word undefined', () => {
    const { storage, win } = fakeWindow(null);
    storage.setItem(`${ENTRY_KEY_FAMILY}undefined`, JSON.stringify({ t: 1, values: { scroll: 3 } }));
    pushHash('#/x', {}, win, () => 'b');
    expect(loadEntry(storage, 'b', 'scroll')).toBeUndefined();
  });

  it('forgets the oldest entry once the copy takes the storage past the limit', () => {
    const { storage, win } = fakeWindow({ boardEntry: 'a' });
    for (let i = 0; i < ENTRY_LIMIT - 1; i++) storage.setItem(`${ENTRY_KEY_FAMILY}k${i}`, JSON.stringify({ t: 1 + i, values: {} }));
    storage.setItem(`${ENTRY_KEY_FAMILY}a`, JSON.stringify({ t: 9000, values: { scroll: 420 } }));
    pushHash('#/x', {}, win, () => 'b', () => 9500);
    expect(Object.keys(storage.data)).toHaveLength(ENTRY_LIMIT);
    expect(storage.getItem(`${ENTRY_KEY_FAMILY}k0`)).toBeNull();
    expect(loadEntry(storage, 'b', 'scroll')).toBe(420);
  });

  it('still pushes when the storage throws', () => {
    const { history, win } = fakeWindow({ boardEntry: 'a' });
    const broken = { ...win, sessionStorage: { getItem: () => '{"t":1,"values":{"scroll":3}}', setItem: () => { throw new Error('quota'); }, removeItem: () => {} } };
    pushHash('#/x', {}, broken, () => 'b');
    expect(history.pushed).toHaveLength(1);
  });
});
