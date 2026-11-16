import { describe, expect, it } from 'vitest';
import { LOCAL_FAMILIES, SESSION_FAMILIES, clearOwnKeys, migrateBrowserStorage, migrateStorage, type MigrationStorage } from './storageMigration';

function memory(initial: Record<string, string> = {}) {
  const data = { ...initial };
  const storage: MigrationStorage & { data: Record<string, string> } = {
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

/** A storage whose every call throws, as blocked storage does. */
function throwing(): MigrationStorage {
  const boom = () => {
    throw new Error('storage is blocked');
  };
  return {
    getItem: boom,
    setItem: boom,
    removeItem: boom,
    key: boom,
    get length(): number {
      return boom();
    },
  };
}

const VIEWS = ['initiatives', 'initiative', 'keys', 'archive', 'notes', 'timeline', 'tasks', 'attention', 'actors'];

// Each family is one old key, its new name, and which storage holds it.
const FAMILIES: { name: string; session: boolean; old: string; now: string }[] = [
  ...VIEWS.map((view) => ({ name: `view v1 ${view}`, session: false, old: `board.view.v1.${view}`, now: `vh.view.v1.${view}` })),
  { name: 'view v2 tasks', session: false, old: 'board.view.v2.tasks', now: 'vh.view.v2.tasks' },
  { name: 'theme', session: false, old: 'board.theme', now: 'vh.theme' },
  { name: 'entry', session: true, old: 'board.entry.k1-abc', now: 'vh.entry.k1-abc' },
];

describe.each(FAMILIES)('the $name family', ({ session, old, now }) => {
  const families = session ? SESSION_FAMILIES : LOCAL_FAMILIES;

  it('moves an old-only key to the new name and removes the old one', () => {
    const s = memory({ [old]: 'old value', unrelated: 'stays' });
    migrateStorage(s, families);
    expect(s.data).toEqual({ [now]: 'old value', unrelated: 'stays' });
  });

  it('leaves a new-only key as it is', () => {
    const s = memory({ [now]: 'new value' });
    migrateStorage(s, families);
    expect(s.data).toEqual({ [now]: 'new value' });
  });

  it('lets the new key win when both exist, and still removes the old one', () => {
    const s = memory({ [old]: 'old value', [now]: 'new value' });
    migrateStorage(s, families);
    expect(s.data).toEqual({ [now]: 'new value' });
  });

  it('does not throw when the storage throws, and the app runs on its defaults', () => {
    expect(() => migrateStorage(throwing(), families)).not.toThrow();
  });
});

describe('migrateStorage', () => {
  it('does nothing without a storage', () => {
    expect(() => migrateStorage(undefined, LOCAL_FAMILIES)).not.toThrow();
  });

  it('keeps the old key when the copy cannot be written', () => {
    const s = memory({ 'board.theme': 'dark' });
    s.setItem = () => {
      throw new Error('quota');
    };
    migrateStorage(s, LOCAL_FAMILIES);
    expect(s.data).toEqual({ 'board.theme': 'dark' });
  });

  it('copies nothing for a listed key that has no value', () => {
    const s = memory({ 'board.theme': 'dark' });
    s.getItem = () => null;
    migrateStorage(s, LOCAL_FAMILIES);
    expect(s.data).toEqual({ 'board.theme': 'dark' });
  });

  it('moves a whole mixed family and no other key', () => {
    const s = memory({
      'board.view.v1.tasks': 'a',
      'board.view.v1.notes': 'b',
      'board.view.v2.tasks': 'c',
      'board.theme': 'light',
      'vh.view.v1.notes': 'new',
      'boardx.theme': 'unrelated',
      'board.entry.k': 'not a local key',
    });
    migrateStorage(s, LOCAL_FAMILIES);
    expect(s.data).toEqual({
      'vh.view.v1.tasks': 'a',
      'vh.view.v1.notes': 'new',
      'vh.view.v2.tasks': 'c',
      'vh.theme': 'light',
      'boardx.theme': 'unrelated',
      'board.entry.k': 'not a local key',
    });
  });
});

describe('migrateBrowserStorage', () => {
  it('migrates localStorage and sessionStorage, each by its own families', () => {
    const local = memory({ 'board.theme': 'dark', 'board.entry.k': 'in the wrong storage' });
    const session = memory({ 'board.entry.k': '{"t":1,"values":{}}', 'board.theme': 'in the wrong storage' });
    migrateBrowserStorage({ localStorage: local, sessionStorage: session });
    expect(local.data).toEqual({ 'vh.theme': 'dark', 'board.entry.k': 'in the wrong storage' });
    expect(session.data).toEqual({ 'vh.entry.k': '{"t":1,"values":{}}', 'board.theme': 'in the wrong storage' });
  });

  it('survives a window whose storage getters throw', () => {
    const win = {
      get localStorage(): MigrationStorage {
        throw new Error('blocked');
      },
      get sessionStorage(): MigrationStorage {
        throw new Error('blocked');
      },
    };
    expect(() => migrateBrowserStorage(win)).not.toThrow();
  });

  it('survives a window with no storage', () => {
    expect(() => migrateBrowserStorage({})).not.toThrow();
  });
});

describe('clearOwnKeys', () => {
  it('removes every vh.view.* key of every version, the theme and every other vh.* key, and nothing else', () => {
    const storage = memory({
      'vh.view.v1.tasks': '{"v":1,"query":"a=b"}',
      'vh.view.v1.initiative': '{"v":1,"query":"c=d"}',
      'vh.view.v9.tasks': 'x',
      'vh.theme': 'dark',
      'other': 'y',
      'vhx.theme': 'not ours',
    });
    clearOwnKeys(storage);
    expect(Object.keys(storage.data).sort()).toEqual(['other', 'vhx.theme']);
  });

  it('removes the board.* keys of the earlier name too: view state and theme (R3)', () => {
    const storage = memory({
      'board.view.v1.tasks': '{"v":1,"query":"a=b"}',
      'board.view.v2.tasks': 'x',
      'vh.view.v1.tasks': '{"v":1,"query":"a=b"}',
      'board.theme': 'dark',
      'other': 'y',
      'boardx.theme': 'not ours',
    });
    clearOwnKeys(storage);
    expect(Object.keys(storage.data).sort()).toEqual(['boardx.theme', 'other']);
  });

  it('removes the board.entry.* and vh.entry.* keys of a sessionStorage', () => {
    const storage = memory({ 'board.entry.k1': '{}', 'vh.entry.k2': '{}', 'vh.entry.k3': '{}', 'other.entry.k': 'y' });
    clearOwnKeys(storage);
    expect(storage.data).toEqual({ 'other.entry.k': 'y' });
  });

  it('survives a throwing storage and a missing one', () => {
    expect(() => clearOwnKeys(throwing())).not.toThrow();
    expect(() => clearOwnKeys(undefined)).not.toThrow();
  });
});
