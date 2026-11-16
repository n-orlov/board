// The browser-storage migration of the Village Hall rename (PRD R3). Every key family the app owned under `board.` now
// lives under `vh.`. `main.ts` calls `migrateBrowserStorage` first, before the theme or any view state is read:
//
//   localStorage    board.view.v1.<view>, board.view.v2.tasks -> vh.view.v1.<view>, vh.view.v2.tasks
//   localStorage    board.theme                               -> vh.theme
//   sessionStorage  board.entry.*                             -> vh.entry.*
//
// For each old key the value is copied when the new key is absent, and then the old key is removed: a new key that is
// already present wins and the old value is dropped. A value that cannot be copied (a full quota) leaves its old key
// where it was. A storage that is absent or throws (private mode, blocked storage) leaves the app on its defaults.
//
// Signing out clears both names: `clearOwnKeys` removes every `board.*` and `vh.*` key of a storage (view state,
// theme, entry state) and leaves every other key alone.

/** The slice of `Storage` the migration uses: tests pass a fake. */
export interface MigrationStorage {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
  removeItem: (key: string) => void;
  key: (index: number) => string | null;
  readonly length: number;
}

/** One family: every key that starts with `from` moves to the same key with `to` in its place. */
export interface KeyFamily {
  from: string;
  to: string;
}

/** The families `localStorage` holds. A prefix with a dot, so `board.theme` is moved on its own below. */
export const LOCAL_FAMILIES: readonly KeyFamily[] = [
  { from: 'board.view.', to: 'vh.view.' },
  { from: 'board.theme', to: 'vh.theme' },
];

/** The families `sessionStorage` holds. */
export const SESSION_FAMILIES: readonly KeyFamily[] = [{ from: 'board.entry.', to: 'vh.entry.' }];

/** The keys of `storage` that start with `prefix`. */
function keysWith(storage: MigrationStorage, prefix: string): string[] {
  const keys: string[] = [];
  for (let i = 0; i < storage.length; i++) {
    const key = storage.key(i);
    if (key !== null && key.startsWith(prefix)) keys.push(key);
  }
  return keys;
}

/** Moves one key: copies when the new one is absent, then removes the old one. */
function moveKey(storage: MigrationStorage, oldKey: string, newKey: string): void {
  if (storage.getItem(newKey) === null) {
    const value = storage.getItem(oldKey);
    if (value === null) return;
    storage.setItem(newKey, value);
  }
  storage.removeItem(oldKey);
}

/** Migrates the families of one storage; any throw ends that storage's migration and nothing else. */
export function migrateStorage(storage: MigrationStorage | undefined, families: readonly KeyFamily[]): void {
  try {
    if (!storage) return;
    for (const { from, to } of families) {
      for (const oldKey of keysWith(storage, from)) moveKey(storage, oldKey, to + oldKey.slice(from.length));
    }
  } catch {
    // A storage that throws: the app runs on its defaults and the old keys stay where they are.
  }
}

/** The storage a window offers, or undefined where reading it throws (blocked storage). */
function open(read: () => MigrationStorage | undefined): MigrationStorage | undefined {
  try {
    return read();
  } catch {
    return undefined;
  }
}

/** Runs the whole migration for a page: `localStorage` and `sessionStorage`, each on its own. */
export function migrateBrowserStorage(win: { readonly localStorage?: MigrationStorage; readonly sessionStorage?: MigrationStorage } = window): void {
  migrateStorage(open(() => win.localStorage), LOCAL_FAMILIES);
  migrateStorage(open(() => win.sessionStorage), SESSION_FAMILIES);
}

/** The prefixes of every key the app owns: the earlier name and the current one. */
export const OWN_KEY_PREFIXES: readonly string[] = ['board.', 'vh.'];

/** The slice of `Storage` that clearing needs. */
export type ClearableStorage = Pick<MigrationStorage, 'key' | 'removeItem' | 'length'>;

/** Sign out: removes every `board.*` and `vh.*` key of `storage` and nothing else; a storage that throws is left as it is. */
export function clearOwnKeys(storage: ClearableStorage | undefined): void {
  try {
    if (!storage) return;
    const keys: string[] = [];
    for (let i = 0; i < storage.length; i++) {
      const key = storage.key(i);
      if (key !== null && OWN_KEY_PREFIXES.some((prefix) => key.startsWith(prefix))) keys.push(key);
    }
    for (const key of keys) storage.removeItem(key);
  } catch {
    // Nothing more can be done with a storage that throws.
  }
}
