import { describe, expect, it, vi } from 'vitest';
import { keyOf, saveEntry, loadEntry, type EntryStorage } from './historyEntry';
import { COLLAPSED, ROW_SCROLL, TimelineEntry, type EntryChart } from './timelineEntry';

/** A page's history stack of entry states and a session storage, the way the browser moves between them. */
function page() {
  const data = new Map<string, string>();
  const storage: EntryStorage = {
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
    removeItem: (k) => void data.delete(k),
  };
  const stack: unknown[] = [{ boardEntry: 'one' }];
  let at = 0;
  const history = {
    get state() {
      return stack[at];
    },
    replaceState: (state: unknown) => void (stack[at] = state),
  };
  const win = { history, sessionStorage: storage };
  return {
    win,
    storage,
    /** A link followed: a new entry with no state of its own. */
    push: () => {
      stack.splice(at + 1);
      stack.push(null);
      at++;
    },
    go: (to: number) => void (at = to),
    key: () => keyOf(history.state),
  };
}

function fakeChart(live: { collapsed: string[]; rows: number }, entry: () => TimelineEntry) {
  const shown: { collapsed: readonly string[]; rows: number }[] = [];
  const chart: EntryChart = {
    // What a flush does in the real chart: the displayed state is reported through the entry's own hooks.
    flush: vi.fn(() => {
      entry().saveCollapsed(live.collapsed);
      entry().saveRows(live.rows);
    }),
    showEntry: vi.fn((state) => {
      shown.push(state);
      live.collapsed = [...state.collapsed];
      live.rows = state.rows;
    }),
  };
  return { chart, shown };
}

describe('TimelineEntry: the state belongs to the entry the chart is displayed on', () => {
  function setup(onTimeline = () => true) {
    const p = page();
    const live = { collapsed: [] as string[], rows: 0 };
    const made: TimelineEntry[] = [];
    const f = fakeChart(live, () => made[0]!);
    const entry = new TimelineEntry(p.win, () => f.chart, onTimeline);
    made.push(entry);
    return { p, live, entry, ...f };
  }
  const saved = (p: ReturnType<typeof page>, key: string, name: string) => loadEntry(p.storage, key, name);

  it('saves to its own entry after the page has moved on to another (a deferred report after Back)', () => {
    const { p, entry } = setup();
    p.push(); // a link followed: history.state is a new, other entry
    entry.saveRows(300);
    entry.saveCollapsed(['i:a']);
    expect(saved(p, 'one', ROW_SCROLL)).toBe(300);
    expect(saved(p, 'one', COLLAPSED)).toEqual(['i:a']);
    expect(p.key()).toBeUndefined();
  });

  it('on a move to another entry, the entry left gets the displayed state first, then the new one is shown', () => {
    const { p, live, entry, shown } = setup();
    live.collapsed = ['i:b'];
    live.rows = 250;
    p.push();
    entry.navigated();
    expect(saved(p, 'one', COLLAPSED)).toEqual(['i:b']);
    expect(saved(p, 'one', ROW_SCROLL)).toBe(250);
    expect(shown).toEqual([{ collapsed: [], rows: 0 }]); // a link's entry: every initiative open, rows at the top
    const second = entry.key();
    expect(second).not.toBe('one');
    live.collapsed = ['i:c'];
    live.rows = 40;

    p.go(0); // Back
    entry.navigated();
    expect(saved(p, second, COLLAPSED)).toEqual(['i:c']);
    expect(saved(p, second, ROW_SCROLL)).toBe(40);
    expect(shown.at(-1)).toEqual({ collapsed: ['i:b'], rows: 250 });
    expect(entry.key()).toBe('one');

    p.go(1); // Forward
    entry.navigated();
    expect(shown.at(-1)).toEqual({ collapsed: ['i:c'], rows: 40 });
  });

  it('a move to another view reports to the entry it was shown on and shows nothing', () => {
    const { p, live, entry, shown, chart } = setup(() => false);
    live.collapsed = ['i:a'];
    live.rows = 90;
    p.push();
    entry.navigated();
    expect(chart.flush).toHaveBeenCalledTimes(1);
    expect(saved(p, 'one', COLLAPSED)).toEqual(['i:a']);
    expect(shown).toEqual([]);
    expect(entry.key()).toBe('one');
  });

  it('a move that stays on the same entry (a script push of the same state) changes nothing', () => {
    const { p, entry, chart, shown } = setup();
    entry.navigated();
    expect(chart.flush).toHaveBeenCalledTimes(1);
    expect(shown).toEqual([]);
    expect(p.key()).toBe('one');
  });

  it('reads what is saved for its entry, ignoring anything that is not a saved collapse or scroll', () => {
    const { p, entry } = setup();
    expect(entry.collapsed()).toEqual([]);
    expect(entry.rows()).toBeUndefined();
    saveEntry(p.storage, 'one', COLLAPSED, ['i:b', 'i:a', 7]);
    saveEntry(p.storage, 'one', ROW_SCROLL, -3);
    expect(entry.collapsed()).toEqual(['i:a', 'i:b']);
    expect(entry.rows()).toBeUndefined();
    saveEntry(p.storage, 'one', ROW_SCROLL, 12);
    expect(entry.rows()).toBe(12);
  });

  it('leaving (the page unloads, or an entry is about to be pushed from this one) flushes the chart', () => {
    const { p, live, entry, chart } = setup();
    live.rows = 77;
    entry.leaving();
    expect(chart.flush).toHaveBeenCalledTimes(1);
    expect(saved(p, 'one', ROW_SCROLL)).toBe(77);
  });

  it('gives an entry with no key one, in place, and owns it', () => {
    const p = page();
    p.push();
    const entry = new TimelineEntry(p.win, () => undefined, () => true);
    expect(entry.key()).toBe(p.key());
    expect(entry.key()).not.toBe('');
    entry.leaving(); // no chart mounted yet: nothing to flush
    entry.navigated();
  });
});
