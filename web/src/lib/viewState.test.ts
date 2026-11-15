import { describe, expect, it, vi } from 'vitest';
import {
  clearAllViews,
  clearViewQuery,
  currentViewEnv,
  FilterTurns,
  loadViewQuery,
  openView,
  pageStorage,
  parseStored,
  saveViewQuery,
  viewKey,
  type ViewEnv,
  type ViewStorage,
} from './viewState';

function fakeStorage(initial: Record<string, string> = {}) {
  const data: Record<string, string> = { ...initial };
  const storage: ViewStorage & { data: Record<string, string> } = {
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

/** A storage whose every call throws, like private mode or a blocked origin. */
function throwingStorage(): ViewStorage {
  const boom = (): never => {
    throw new Error('SecurityError');
  };
  return { getItem: boom, setItem: boom, removeItem: boom, key: boom, get length(): number { return boom(); } };
}

function env(storage: ViewStorage | undefined) {
  const replaceHash = vi.fn<(hash: string) => void>();
  const e: ViewEnv = { storage, replaceHash };
  return { e, replaceHash };
}

const KEY = 'board.view.v1.tasks';
const stored = (query: string) => JSON.stringify({ v: 1, query });

describe('viewKey', () => {
  it('is board.view.v1.<view>', () => {
    expect(viewKey('tasks')).toBe(KEY);
    expect(viewKey('initiative')).toBe('board.view.v1.initiative');
  });
});

describe('openView on a revisited entry', () => {
  const revisitedEnv = (storage: ViewStorage, revisited: boolean) => {
    const replaceHash = vi.fn<(hash: string) => void>();
    const e: ViewEnv = { storage, replaceHash, revisited: () => revisited };
    return { e, replaceHash };
  };

  it('shows the defaults for a bare fragment and imports nothing a later visit saved', () => {
    const storage = fakeStorage({ [KEY]: stored('status=done&sort=title&dir=desc') });
    const { e, replaceHash } = revisitedEnv(storage, true);
    expect(openView(e, 'tasks', '#/tasks', '')).toBe('');
    expect(replaceHash).not.toHaveBeenCalled();
    expect(storage.data[KEY]).toBe(stored('status=done&sort=title&dir=desc'));
  });

  it('still uses and saves the query its own fragment carries', () => {
    const storage = fakeStorage({ [KEY]: stored('status=done') });
    const { e, replaceHash } = revisitedEnv(storage, true);
    expect(openView(e, 'tasks', '#/tasks', 'kind=watch')).toBe('kind=watch');
    expect(replaceHash).not.toHaveBeenCalled();
    expect(storage.data[KEY]).toBe(stored('kind=watch'));
  });

  it('restores the saved query for a bare link that is a new navigation, for any view', () => {
    for (const [view, path] of [['tasks', '#/tasks'], ['actors', '#/actors']] as const) {
      const storage = fakeStorage({ [`board.view.v1.${view}`]: stored('sort=x&dir=desc') });
      const { e, replaceHash } = revisitedEnv(storage, false);
      expect(openView(e, view, path, '')).toBe('sort=x&dir=desc');
      expect(replaceHash).toHaveBeenCalledWith(`${path}?sort=x&dir=desc`);
    }
  });

  it('the browser adapter asks the history entry on screen', () => {
    const e = currentViewEnv({ history: { state: { boardEntry: 'never-revisited' }, replaceState: vi.fn() } });
    expect(e.revisited?.()).toBe(false);
  });
});

describe('openView', () => {
  it('uses the query the fragment carries and saves it, whatever was saved before', () => {
    const storage = fakeStorage({ [KEY]: stored('status=done') });
    const { e, replaceHash } = env(storage);
    expect(openView(e, 'tasks', '#/tasks', 'kind=watch&sort=title')).toBe('kind=watch&sort=title');
    expect(replaceHash).not.toHaveBeenCalled();
    expect(storage.data[KEY]).toBe(stored('kind=watch&sort=title'));
  });

  it('restores the saved query for a bare route by rewriting the fragment in place', () => {
    const { e, replaceHash } = env(fakeStorage({ [KEY]: stored('status=done&sort=title&dir=desc') }));
    expect(openView(e, 'tasks', '#/tasks', '')).toBe('status=done&sort=title&dir=desc');
    expect(replaceHash).toHaveBeenCalledTimes(1);
    expect(replaceHash).toHaveBeenCalledWith('#/tasks?status=done&sort=title&dir=desc');
  });

  it('opens a bare route at its defaults when nothing is saved', () => {
    const { e, replaceHash } = env(fakeStorage());
    expect(openView(e, 'tasks', '#/tasks', '')).toBe('');
    expect(replaceHash).not.toHaveBeenCalled();
  });

  it.each([
    ['not JSON', 'status=done'],
    ['JSON that is not an object', '"status=done"'],
    ['null', 'null'],
    ['an object without a query', JSON.stringify({ v: 1 })],
    ['a query that is not text', JSON.stringify({ v: 1, query: 7 })],
    ['an empty query', JSON.stringify({ v: 1, query: '' })],
  ])('falls back to the defaults on a malformed value (%s)', (_name, raw) => {
    const { e, replaceHash } = env(fakeStorage({ [KEY]: raw }));
    expect(openView(e, 'tasks', '#/tasks', '')).toBe('');
    expect(replaceHash).not.toHaveBeenCalled();
  });

  it.each([2, 0, '1', undefined])('falls back to the defaults on a foreign version (%j)', (v) => {
    const { e, replaceHash } = env(fakeStorage({ [KEY]: JSON.stringify({ v, query: 'status=done' }) }));
    expect(openView(e, 'tasks', '#/tasks', '')).toBe('');
    expect(replaceHash).not.toHaveBeenCalled();
  });

  it('never reads another version key', () => {
    const { e } = env(fakeStorage({ 'board.view.v2.tasks': stored('status=done') }));
    expect(openView(e, 'tasks', '#/tasks', '')).toBe('');
  });

  it('keeps working on a storage that throws, with a query and without one', () => {
    const { e, replaceHash } = env(throwingStorage());
    expect(openView(e, 'tasks', '#/tasks', '')).toBe('');
    expect(openView(e, 'tasks', '#/tasks', 'status=done')).toBe('status=done');
    expect(replaceHash).not.toHaveBeenCalled();
  });

  it('keeps working with no storage at all', () => {
    const { e } = env(undefined);
    expect(openView(e, 'tasks', '#/tasks', '')).toBe('');
    expect(openView(e, 'tasks', '#/tasks', '?a=b')).toBe('a=b');
  });

  it('accepts a query with its leading question mark', () => {
    const storage = fakeStorage();
    expect(openView(env(storage).e, 'tasks', '#/tasks', '?status=done')).toBe('status=done');
    expect(storage.data[KEY]).toBe(stored('status=done'));
  });

  it('shares one key for every initiative', () => {
    const storage = fakeStorage();
    openView(env(storage).e, 'initiative', '#/i/e2', 'status=done');
    const { e, replaceHash } = env(storage);
    expect(openView(e, 'initiative', '#/i/e5', '')).toBe('status=done');
    expect(replaceHash).toHaveBeenCalledWith('#/i/e5?status=done');
  });
});

describe('saveViewQuery and clearViewQuery', () => {
  it('saving the default (empty) query removes the key', () => {
    const storage = fakeStorage({ [KEY]: stored('status=done') });
    saveViewQuery(storage, 'tasks', '');
    expect(storage.data).toEqual({});
  });

  it('clear removes the key and only that key', () => {
    const storage = fakeStorage({ [KEY]: stored('status=done'), 'board.view.v1.notes': stored('q=x') });
    clearViewQuery(storage, 'tasks');
    expect(Object.keys(storage.data)).toEqual(['board.view.v1.notes']);
    expect(loadViewQuery(storage, 'tasks')).toBeNull();
  });

  it('survives a throwing storage and a missing one', () => {
    expect(() => saveViewQuery(throwingStorage(), 'tasks', 'a=b')).not.toThrow();
    expect(() => clearViewQuery(throwingStorage(), 'tasks')).not.toThrow();
    expect(() => saveViewQuery(undefined, 'tasks', 'a=b')).not.toThrow();
    expect(loadViewQuery(throwingStorage(), 'tasks')).toBeNull();
    expect(loadViewQuery(undefined, 'tasks')).toBeNull();
  });
});

describe('clearAllViews', () => {
  it('removes every board.view.* key of every version and nothing else', () => {
    const storage = fakeStorage({
      [KEY]: stored('a=b'),
      'board.view.v1.initiative': stored('c=d'),
      'board.view.v9.tasks': 'x',
      'board.theme': 'dark',
      'other': 'y',
    });
    clearAllViews(storage);
    expect(Object.keys(storage.data).sort()).toEqual(['board.theme', 'other']);
  });

  it('survives a throwing storage and a missing one', () => {
    expect(() => clearAllViews(throwingStorage())).not.toThrow();
    expect(() => clearAllViews(undefined)).not.toThrow();
  });
});

describe('parseStored', () => {
  it('reads the query of a current value and nothing from an absent one', () => {
    expect(parseStored(stored('a=b'))).toBe('a=b');
    expect(parseStored(null)).toBeNull();
  });
});

describe('the browser adapter', () => {
  it('replaces the fragment without a history entry and reads the window storage', () => {
    const replaceState = vi.fn();
    const storage = fakeStorage();
    const e = currentViewEnv({ history: { replaceState }, localStorage: storage });
    expect(e.storage).toBe(storage);
    e.replaceHash('#/tasks?a=b');
    expect(replaceState).toHaveBeenCalledWith(null, '', '#/tasks?a=b');
  });

  it('keeps the history entry state when it rewrites the fragment', () => {
    const replaceState = vi.fn();
    const e = currentViewEnv({ history: { state: { boardEntry: 'k' }, replaceState } });
    e.replaceHash('#/tasks?a=b');
    expect(replaceState).toHaveBeenCalledWith({ boardEntry: 'k' }, '', '#/tasks?a=b');
  });

  it('has no storage where reading it throws', () => {
    const win = {
      get localStorage(): never {
        throw new Error('blocked');
      },
    };
    expect(pageStorage(win)).toBeUndefined();
  });
});

describe('FilterTurns', () => {
  it('keeps a change current until a later one begins', () => {
    const turns = new FilterTurns();
    const restoring = turns.begin();
    expect(restoring()).toBe(true);
    const link = turns.begin();
    expect(restoring()).toBe(false);
    expect(link()).toBe(true);
    turns.begin();
    expect(link()).toBe(false);
  });

  it('a restoration that answers after a link and after a filter applied is stale either way', () => {
    const turns = new FilterTurns();
    const first = turns.begin();
    turns.begin(); // a link
    const second = turns.begin(); // a filter applied on top
    expect([first(), second()]).toEqual([false, true]);
  });

  it('must read before the first read even for the filter shown, and afterwards only for another filter', () => {
    const turns = new FilterTurns();
    expect(turns.hasRead()).toBe(false);
    expect(turns.mustRead(true)).toBe(true);
    expect(turns.mustRead(false)).toBe(true);
    turns.reading();
    expect(turns.hasRead()).toBe(true);
    expect(turns.mustRead(true)).toBe(false);
    expect(turns.mustRead(false)).toBe(true);
  });

  it('a view that ends turns every pending check false, and a change begun after the end is never current', () => {
    const turns = new FilterTurns();
    const restoring = turns.begin();
    const hashChange = turns.begin();
    turns.end();
    expect([restoring(), hashChange()]).toEqual([false, false]);
    expect(turns.begin()()).toBe(false);
  });

  it('ending a view that already read leaves no check current either', () => {
    const turns = new FilterTurns();
    const applied = turns.begin();
    turns.reading();
    expect(applied()).toBe(true);
    turns.end();
    expect(applied()).toBe(false);
  });
});
