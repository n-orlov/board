import { describe, expect, it } from 'vitest';
import { KEYS_VIEW, KEY_COLUMNS, KEY_SORT_COLUMNS, KEY_SORT_LABELS, isKeysHash, keysHash, queryOfHash, sortOfQuery } from './keysView';
import type { Key } from './keys';
import { sortRows } from './tableSort';
import { openView, saveViewQuery, viewKey, type ViewStorage } from './viewState';

function key(name: string, over: Partial<Key> = {}): Key {
  return { id: name, name, display_prefix: `bk_${name}`, status: 'active', created_at: '2026-03-01T00:00:00Z', created_by: 'alice', ...over };
}

function memory(): ViewStorage & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
    removeItem: (k) => void data.delete(k),
    key: (i) => [...data.keys()][i] ?? null,
    get length() {
      return data.size;
    },
  };
}

const KEYS: Key[] = [
  key('mid', { display_prefix: 'bk_b', status: 'expired', created_at: '2026-03-02T00:00:00+02:00', last_used_at: '2026-03-05T00:00:00Z' }),
  key('Zulu', { display_prefix: 'bk_c', status: 'revoked', created_at: '2026-03-01T23:00:00Z', expires_at: '2026-04-01T00:00:00Z' }),
  key('alpha', { display_prefix: 'bk_a', status: 'active', created_at: '2026-03-03T00:00:00Z', last_used_at: '2026-03-04T00:00:00Z', expires_at: '2026-05-01T00:00:00Z' }),
];
const names = (column: string, dir: 'asc' | 'desc') => sortRows(KEYS, { column, dir }, KEY_COLUMNS).map((k) => k.name);

describe('the sortable columns', () => {
  it('names the six columns of the table, in order, with a phone label each', () => {
    expect(KEY_SORT_COLUMNS).toEqual(['name', 'prefix', 'status', 'created', 'used', 'expires']);
    expect(KEY_SORT_LABELS.map((l) => l.column)).toEqual(KEY_SORT_COLUMNS);
    expect(KEY_SORT_LABELS.map((l) => l.label)).toEqual(['Name', 'Prefix', 'Status', 'Created', 'Last used', 'Expires']);
  });

  it('compares each column by its own type: text, status and time', () => {
    expect(Object.fromEntries(Object.entries(KEY_COLUMNS).map(([name, column]) => [name, column.type]))).toEqual({
      name: 'text',
      prefix: 'text',
      status: 'status',
      created: 'time',
      used: 'time',
      expires: 'time',
    });
  });

  it('sorts text without regard to case, status in lifecycle order and time by the instant', () => {
    expect(names('name', 'asc')).toEqual(['alpha', 'mid', 'Zulu']);
    expect(names('prefix', 'desc')).toEqual(['Zulu', 'mid', 'alpha']);
    expect(names('status', 'asc')).toEqual(['alpha', 'mid', 'Zulu']);
    expect(names('status', 'desc')).toEqual(['Zulu', 'mid', 'alpha']);
    // 00:00+02:00 on the 2nd is 22:00Z on the 1st, before 23:00Z on the 1st, though it sorts after it as text.
    expect(names('created', 'asc')).toEqual(['mid', 'Zulu', 'alpha']);
    expect(names('created', 'desc')).toEqual(['alpha', 'Zulu', 'mid']);
  });

  it('puts a key that was never used or never expires last in both directions', () => {
    expect(names('used', 'asc')).toEqual(['alpha', 'mid', 'Zulu']);
    expect(names('used', 'desc')).toEqual(['mid', 'alpha', 'Zulu']);
    expect(names('expires', 'asc')).toEqual(['Zulu', 'alpha', 'mid']);
    expect(names('expires', 'desc')).toEqual(['alpha', 'Zulu', 'mid']);
  });

  it('keeps the server order when nothing is sorted', () => {
    expect(sortRows(KEYS, null, KEY_COLUMNS).map((k) => k.name)).toEqual(['mid', 'Zulu', 'alpha']);
  });
});

describe('the sort and the fragment query', () => {
  it('reads a column and a direction from the query, anywhere in it', () => {
    expect(sortOfQuery('sort=created&dir=desc')).toEqual({ column: 'created', dir: 'desc' });
    expect(sortOfQuery('dir=asc&sort=expires')).toEqual({ column: 'expires', dir: 'asc' });
  });

  it('opens the default order for an unknown column, a bad direction or no query', () => {
    expect(sortOfQuery('')).toBeNull();
    expect(sortOfQuery('sort=actions&dir=asc')).toBeNull();
    expect(sortOfQuery('sort=name&dir=sideways')).toBeNull();
  });

  it('writes the fragment: bare for the default, else sort then dir', () => {
    expect(keysHash(null)).toBe('#/settings/keys');
    expect(keysHash({ column: 'used', dir: 'asc' })).toBe('#/settings/keys?sort=used&dir=asc');
  });

  it('round-trips every column in both directions', () => {
    for (const column of KEY_SORT_COLUMNS) {
      for (const dir of ['asc', 'desc'] as const) {
        expect(sortOfQuery(queryOfHash(keysHash({ column, dir })))).toEqual({ column, dir });
      }
    }
  });

  it('reads the query after the first ?, and none from a bare fragment', () => {
    expect(queryOfHash('#/settings/keys?sort=name&dir=asc')).toBe('sort=name&dir=asc');
    expect(queryOfHash('#/settings/keys')).toBe('');
    expect(queryOfHash('?sort=name')).toBe('sort=name');
  });

  it('knows its own fragment, bare or with a query, and no other', () => {
    expect(isKeysHash('#/settings/keys')).toBe(true);
    expect(isKeysHash('#/settings/keys?sort=name&dir=asc')).toBe(true);
    expect(isKeysHash('#/settings/keysx')).toBe(false);
    expect(isKeysHash('#/actors')).toBe(false);
  });
});

describe('saved state', () => {
  it('opens a bare route with the saved query, in place', () => {
    const storage = memory();
    saveViewQuery(storage, KEYS_VIEW, 'sort=created&dir=desc');
    expect(viewKey(KEYS_VIEW)).toBe('vh.view.v1.keys');
    const replaced: string[] = [];
    const query = openView({ storage, replaceHash: (h) => replaced.push(h) }, KEYS_VIEW, '#/settings/keys', '');
    expect(replaced).toEqual(['#/settings/keys?sort=created&dir=desc']);
    expect(sortOfQuery(query)).toEqual({ column: 'created', dir: 'desc' });
  });

  it('lets a fragment with a query win and saves it', () => {
    const storage = memory();
    saveViewQuery(storage, KEYS_VIEW, 'sort=name&dir=asc');
    const query = openView({ storage, replaceHash: () => undefined }, KEYS_VIEW, '#/settings/keys', 'sort=status&dir=desc');
    expect(sortOfQuery(query)).toEqual({ column: 'status', dir: 'desc' });
    expect(JSON.parse(storage.data.get('vh.view.v1.keys')!)).toEqual({ v: 1, query: 'sort=status&dir=desc' });
  });
});
