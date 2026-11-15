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
  const calls: { data: unknown; url?: string }[] = [];
  const history: EntryHistory & { calls: typeof calls } = {
    state,
    calls,
    replaceState(data, _unused, url) {
      calls.push({ data, url });
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
    expect(h.calls).toEqual([{ data: { boardEntry: 'k' }, url: '#/tasks?q=x' }]);
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
  });
  it('drop the oldest entries past the limit', () => {
    const s = fakeStorage();
    for (let i = 0; i <= ENTRY_LIMIT; i++) saveEntry(s, `k${i}`, 'scroll', i, () => i);
    expect(Object.keys(s.data)).toHaveLength(ENTRY_LIMIT);
    expect(loadEntry(s, 'k0', 'scroll')).toBeUndefined();
    expect(loadEntry(s, `k${ENTRY_LIMIT}`, 'scroll')).toBe(ENTRY_LIMIT);
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
  const page = {
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
    addEventListener(name: string, fn: Listener) {
      (listeners[name] ??= []).push(fn);
    },
    removeEventListener(name: string, fn: Listener) {
      listeners[name] = (listeners[name] ?? []).filter((f) => f !== fn);
    },
    requestAnimationFrame(cb: () => void) {
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
    expect(page.listening('wheel')).toBe(0);
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
