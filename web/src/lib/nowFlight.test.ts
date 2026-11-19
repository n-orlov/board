import { describe, expect, it } from 'vitest';
import {
  DEFAULT_WINDOW,
  WINDOW_STORAGE_KEY,
  WINDOWS,
  flightRows,
  loadWindow,
  parseWindow,
  recentRows,
  saveWindow,
  type InFlight,
  type Recent,
} from './nowFlight';

const NOW = new Date('2026-01-01T12:00:00Z');

describe('the window preference', () => {
  it('uses a versioned vh. key, never the prototype one', () => {
    expect(WINDOW_STORAGE_KEY).toMatch(/^vh\.[a-z.]+\.v\d+$/);
    expect(WINDOW_STORAGE_KEY.startsWith('vh.proto.')).toBe(false);
  });

  it('offers 8h, 24h and 3d, defaulting to 24h', () => {
    expect(WINDOWS.map((w) => w.value)).toEqual(['8h', '24h', '3d']);
    expect(DEFAULT_WINDOW).toBe('24h');
    expect(loadWindow({ getItem: () => null, setItem: () => undefined })).toBe('24h');
    expect(loadWindow(undefined)).toBe('24h');
  });

  it('persists a choice and reads it back', () => {
    const data = new Map<string, string>();
    const storage = { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => void data.set(k, v) };
    saveWindow(storage, '3d');
    expect(data.get(WINDOW_STORAGE_KEY)).toBe('3d');
    expect(loadWindow(storage)).toBe('3d');
  });

  it('ignores a stored value that is not one of the three', () => {
    for (const bad of ['', '1h', '24H', '7d', '"24h"', 'null', '{}']) expect(parseWindow(bad)).toBe('24h');
    expect(parseWindow(null)).toBe('24h');
    expect(parseWindow(undefined)).toBe('24h');
    for (const good of ['8h', '24h', '3d']) expect(parseWindow(good)).toBe(good);
  });

  it('survives blocked storage', () => {
    const blocked = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    expect(loadWindow(blocked)).toBe('24h');
    expect(() => saveWindow(blocked, '8h')).not.toThrow();
  });
});

const task = (ref: string, title = `Title ${ref}`) => ({ task: ref, title, initiative: 'aa', status: 'open' as const, kind: 'work' as const });

describe('flightRows', () => {
  const items: InFlight[] = [
    {
      task: task('aa-2'),
      leases: [
        { lease: 'L7', holder: { handle: 'claude:x', way_back: 'w' }, intent: 'edit', started_at: '2026-01-01T11:00:00Z', renewed_at: '2026-01-01T11:30:00Z', expires_at: '2026-01-01T12:41:00Z' },
        { lease: 'L9', holder: { handle: 'sue' }, intent: 'review', started_at: '2026-01-01T11:10:00Z', renewed_at: '2026-01-01T11:10:00Z', expires_at: '2026-01-01T12:00:30Z' },
      ],
    },
    { task: task('aa/1'), leases: [] },
  ];

  it('makes one row per task with its leases nested, holder, intent and time left', () => {
    const rows = flightRows(items, NOW);
    expect(rows.map((r) => r.task)).toEqual(['aa-2', 'aa/1']);
    expect(rows[0]).toMatchObject({ title: 'Title aa-2', initiative: 'aa', href: '#/t/aa-2' });
    expect(rows[0]!.leases).toEqual([
      { lease: 'L7', holder: 'claude:x', intent: 'edit', ttl: 'expires in 41m' },
      { lease: 'L9', holder: 'sue', intent: 'review', ttl: 'expires in under a minute' },
    ]);
  });

  it('escapes the ref in the link and phrases a lease past its time', () => {
    expect(flightRows(items, NOW)[1]!.href).toBe('#/t/aa%2F1');
    const late = flightRows([{ task: task('aa-3'), leases: [{ ...items[0]!.leases[0]!, expires_at: '2026-01-01T11:58:00Z' }] }], NOW);
    expect(late[0]!.leases[0]!.ttl).toBe('expired 2m ago');
  });

  it('is empty for no tasks', () => {
    expect(flightRows([], NOW)).toEqual([]);
  });
});

describe('recentRows', () => {
  const items: Recent[] = [
    { task: task('aa-4'), last_event: { type: 'lease_ended', summary: 'L3 ended: done', actor: { handle: 'bob' }, at: '2026-01-01T11:48:00Z' } },
  ];

  it('shows the last event with its type, actor and age, in the server order', () => {
    const rows = recentRows(items, NOW);
    expect(rows).toEqual([
      { task: 'aa-4', title: 'Title aa-4', initiative: 'aa', href: '#/t/aa-4', last: 'lease_ended by bob 12 minutes ago', summary: 'L3 ended: done', at: '2026-01-01T11:48:00Z' },
    ]);
  });
});
