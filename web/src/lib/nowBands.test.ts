import { describe, expect, it } from 'vitest';
import type { AttentionCounts, SectionKey } from './attention';
import type { Attention } from './attentionView';
import {
  BANDS,
  BANDS_STORAGE_KEY,
  bandCount,
  bandOf,
  bandRows,
  defaultOpen,
  isOpen,
  layoutOf,
  loadBandPrefs,
  NO_BAND_PREFS,
  parseBandPrefs,
  saveBandPrefs,
  withChoice,
  type BandKey,
  type BandStorage,
} from './nowBands';

const NOW = new Date('2026-01-01T12:00:00Z');
const bob = { handle: 'bob', way_back: 'ralphctl status bob' };
const counts: AttentionCounts = { open_asks: 2, done_not_closed: 1, expired_leases: 1, overdue_watches: 3, conflicts: 2, total: 9 };
const attention: Attention = {
  counts,
  open_asks: [{ ref: 'A1', question: 'Which?', asker: bob, mode: 'poll', asker_waiting: false, blocking: false, created_at: '2026-01-01T11:00:00Z', age_seconds: 3600 }],
  done_not_closed: [{ task: 'aa-2', title: 'Ship', done_by: bob, done_at: '2026-01-01T10:00:00Z' }],
  expired_leases: [{ lease: 'L4', holder: bob, task: 'aa-3', title: 'Slow', intent: 'ride', renewed_at: '2026-01-01T08:00:00Z', expired_at: '2026-01-01T09:00:00Z' }],
  overdue_watches: [],
  conflicts: [{ resource: 'db/main', holders: [{ lease: 'L1', actor: bob, task: 'aa-1', intent: 'm', expires_at: '2026-01-01T13:00:00Z' }], tasks: ['aa-1'] }],
};
const band = (key: BandKey) => BANDS.find((b) => b.key === key)!;

describe('band assignment and order', () => {
  it('has the four bands in order, Done, not closed last', () => {
    expect(BANDS.map((b) => b.title)).toEqual(['Open asks', 'Expired leases and conflicts', 'Overdue watches', 'Done, not closed']);
    expect(BANDS.map((b) => b.key)).toEqual(['asks', 'expired', 'watches', 'done']);
  });

  it('puts every attention section in exactly one band', () => {
    const all: SectionKey[] = ['open_asks', 'expired_leases', 'conflicts', 'overdue_watches', 'done_not_closed'];
    expect(BANDS.flatMap((b) => [...b.sections]).sort()).toEqual([...all].sort());
    expect(all.map(bandOf)).toEqual(['asks', 'expired', 'expired', 'watches', 'done']);
  });

  it('counts a band as the counts of its sections added', () => {
    expect(BANDS.map((b) => bandCount(counts, b))).toEqual([2, 3, 3, 1]);
  });

  it('lists expired leases before conflicts in the shared band, each row tagged with its section', () => {
    const rows = bandRows(band('expired'), attention, NOW);
    expect(rows.map((r) => [r.section, r.row.id])).toEqual([
      ['expired_leases', 'L4'],
      ['conflicts', 'db/main'],
    ]);
    expect(bandRows(band('asks'), attention, NOW).map((r) => r.section)).toEqual(['open_asks']);
    expect(bandRows(band('done'), attention, NOW).map((r) => r.row.id)).toEqual(['aa-2']);
    expect(bandRows(band('watches'), attention, NOW)).toEqual([]);
    expect(bandRows(band('asks'), { counts }, NOW)).toEqual([]);
  });
});

describe('default open states', () => {
  const keys: BandKey[] = ['asks', 'expired', 'watches', 'done'];

  it('opens every band but Done, not closed on a wide screen, empty or not', () => {
    expect(keys.map((k) => defaultOpen(k, 'wide', 5))).toEqual([true, true, true, false]);
    expect(keys.map((k) => defaultOpen(k, 'wide', 0))).toEqual([true, true, true, false]);
  });

  it('opens only Open asks and Expired leases and conflicts, and only when not empty, on a narrow one', () => {
    expect(keys.map((k) => defaultOpen(k, 'narrow', 5))).toEqual([true, true, false, false]);
    expect(keys.map((k) => defaultOpen(k, 'narrow', 0))).toEqual([false, false, false, false]);
  });

  it('maps the media query to a layout', () => {
    expect(layoutOf(true)).toBe('wide');
    expect(layoutOf(false)).toBe('narrow');
  });
});

describe('remembered open state', () => {
  it('uses the default until a band is toggled, then the choice, apart for wide and narrow', () => {
    expect(isOpen(NO_BAND_PREFS, 'asks', 'wide', 0)).toBe(true);
    const closedWide = withChoice(NO_BAND_PREFS, 'asks', 'wide', false);
    expect(isOpen(closedWide, 'asks', 'wide', 4)).toBe(false);
    expect(isOpen(closedWide, 'asks', 'narrow', 4)).toBe(true);
    const openDone = withChoice(closedWide, 'done', 'narrow', true);
    expect(isOpen(openDone, 'done', 'narrow', 0)).toBe(true);
    expect(isOpen(openDone, 'done', 'wide', 0)).toBe(false);
    expect(isOpen(openDone, 'asks', 'wide', 4)).toBe(false);
  });

  it('reads back what it saved under a versioned vh. key', () => {
    const data = new Map<string, string>();
    const storage: BandStorage = { getItem: (k) => data.get(k) ?? null, setItem: (k, v) => void data.set(k, v) };
    expect(BANDS_STORAGE_KEY).toBe('vh.now.bands.v1');
    expect(loadBandPrefs(storage)).toEqual(NO_BAND_PREFS);
    const prefs = withChoice(withChoice(NO_BAND_PREFS, 'watches', 'wide', false), 'done', 'narrow', true);
    saveBandPrefs(storage, prefs);
    expect([...data.keys()]).toEqual(['vh.now.bands.v1']);
    expect(loadBandPrefs(storage)).toEqual(prefs);
  });

  it('falls back to the defaults on a bad stored value, and drops unknown bands and non-boolean choices', () => {
    for (const bad of [null, undefined, '', 'nope', '[]', '7', 'null', '{"wide":5,"narrow":[]}']) {
      expect(parseBandPrefs(bad)).toEqual(NO_BAND_PREFS);
    }
    expect(parseBandPrefs('{"wide":{"asks":false,"ghost":true,"done":"yes"},"narrow":{"done":true}}')).toEqual({ wide: { asks: false }, narrow: { done: true } });
  });

  it('survives storage that throws', () => {
    const blocked: BandStorage = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    expect(loadBandPrefs(blocked)).toEqual(NO_BAND_PREFS);
    expect(() => saveBandPrefs(blocked, NO_BAND_PREFS)).not.toThrow();
    expect(loadBandPrefs(undefined)).toEqual(NO_BAND_PREFS);
    expect(() => saveBandPrefs(undefined, NO_BAND_PREFS)).not.toThrow();
  });
});
