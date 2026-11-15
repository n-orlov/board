import { describe, expect, it } from 'vitest';
import {
  ENTRY_KEY_FAMILY,
  ENTRY_LIMIT,
  ensureKey,
  keyOf,
  loadEntry,
  newKey,
  replaceHash,
  saveEntry,
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
