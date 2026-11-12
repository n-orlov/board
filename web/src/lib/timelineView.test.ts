import { describe, expect, it, vi } from 'vitest';
import type { Browser } from './session';
import { DEFAULT_TIMELINE_FILTER, type Timeline, type TimelineModel } from './timeline';
import {
  chartOptions,
  DAY_MS,
  HOUR_MS,
  initialWindow,
  mountChart,
  TimelineApi,
  TimelineViewStore,
  ZOOM_MAX_MS,
  ZOOM_MIN_MS,
  type ChartLib,
  type TimelineViewState,
} from './timelineView';

const NOW = new Date('2026-03-10T12:00:00Z');
const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

function answer(over: Partial<Timeline> = {}): Timeline {
  return {
    from: '2026-02-24T12:00:00Z',
    to: '2026-03-17T12:00:00Z',
    initiatives: [{ slug: 'alpha', title: 'Alpha', tags: [], closed: false }],
    tasks: [
      {
        ref: 'T1',
        initiative: 'alpha',
        title: 'First',
        status: 'open',
        kind: 'work',
        created_at: '2026-02-28T09:00:00Z',
        attention: [],
        deps: [],
      },
    ],
    leases: [],
    markers: [],
    ...over,
  };
}

describe('initialWindow', () => {
  it('opens two weeks back and one week ahead of now, the server default', () => {
    const w = initialWindow(NOW);
    expect(w.from.toISOString()).toBe('2026-02-24T12:00:00.000Z');
    expect(w.to.toISOString()).toBe('2026-03-17T12:00:00.000Z');
    expect(DAY_MS).toBe(24 * HOUR_MS);
    expect(HOUR_MS).toBe(3_600_000);
  });
});

describe('TimelineApi', () => {
  it('reads GET /timeline under the filter and window, same origin, under the mount prefix', async () => {
    const fetchFn = vi.fn(async (_input: string, _init?: RequestInit) => json(answer()));
    const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname: '/board/', setHash: vi.fn() };
    const got = await new TimelineApi(browser, () => 't').read({ ...DEFAULT_TIMELINE_FILTER, actor: 'ada', closedWithin: 30 }, initialWindow(NOW));
    expect(got.tasks.map((t) => t.ref)).toEqual(['T1']);
    expect(fetchFn).toHaveBeenCalledTimes(1);
    const [url, init] = fetchFn.mock.calls[0]!;
    expect(url).toBe('/board/api/v1/timeline?from=2026-02-24T12%3A00%3A00.000Z&to=2026-03-17T12%3A00%3A00.000Z&actor=ada&closed_within=30d');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.headers).toEqual({ Accept: 'application/json' });
  });

  it('turns a refusal into its problem text', async () => {
    const problem = new Response(JSON.stringify({ detail: 'from must be before to' }), { status: 400, headers: { 'Content-Type': 'application/problem+json' } });
    const browser: Browser = { fetch: vi.fn(async () => problem), assign: vi.fn(), pathname: '/', setHash: vi.fn() };
    await expect(new TimelineApi(browser, () => 't').read(DEFAULT_TIMELINE_FILTER, initialWindow(NOW))).rejects.toThrow(/from must be before to/);
  });
});

function rig(read: (filter: unknown) => Promise<Timeline>) {
  const api = { read: vi.fn(read) };
  const seen: TimelineViewState[] = [];
  const store = new TimelineViewStore(api, (s) => seen.push({ ...s }), DEFAULT_TIMELINE_FILTER, () => NOW);
  return { api, store, seen };
}

describe('TimelineViewStore', () => {
  it('starts unloaded on the initial window and draws the mapped answer once read', async () => {
    const { store, api, seen } = rig(async () => answer());
    expect(store.state).toMatchObject({ loaded: false, loading: false, error: '', filter: DEFAULT_TIMELINE_FILTER });
    expect(store.state.model).toEqual({ groups: [], items: [], dependencies: [] });
    expect(store.state.window).toEqual(initialWindow(NOW));
    await store.load();
    expect(api.read).toHaveBeenCalledWith(DEFAULT_TIMELINE_FILTER, initialWindow(NOW));
    expect(seen[0]!.loading).toBe(true);
    expect(store.state).toMatchObject({ loaded: true, loading: false, error: '' });
    expect(store.state.model.groups.map((g) => g.id)).toEqual(['i:alpha', 't:T1']);
    expect(store.state.model.items.map((i) => i.id)).toContain('act:T1');
  });

  it('opens on the window around the real clock when no clock is handed in', () => {
    const before = Date.now();
    const store = new TimelineViewStore({ read: vi.fn(async () => answer()) }, () => undefined, DEFAULT_TIMELINE_FILTER);
    const from = store.state.window.from.getTime() + 14 * DAY_MS;
    expect(from).toBeGreaterThanOrEqual(before);
    expect(from).toBeLessThanOrEqual(Date.now());
    expect(store.state.window.to.getTime() - store.state.window.from.getTime()).toBe(21 * DAY_MS);
  });

  it('shows a refusal and keeps what it drew', async () => {
    let fail = false;
    const { store } = rig(async () => {
      if (fail) throw new Error('The server is not reachable.');
      return answer();
    });
    await store.load();
    fail = true;
    await store.load();
    expect(store.state).toMatchObject({ loaded: true, loading: false, error: 'The server is not reachable.' });
    expect(store.state.model.groups).toHaveLength(2);
    fail = false;
    await store.load();
    expect(store.state.error).toBe('');
  });

  it('reads again under a new filter', async () => {
    const { store, api } = rig(async () => answer());
    const filter = { ...DEFAULT_TIMELINE_FILTER, tags: ['work'] };
    await store.setFilter(filter);
    expect(store.state.filter).toEqual(filter);
    expect(api.read).toHaveBeenLastCalledWith(filter, initialWindow(NOW));
  });

  it('drops a read that lands after a newer one and stays loading while one is out', async () => {
    const resolvers: ((t: Timeline) => void)[] = [];
    const { store } = rig(() => new Promise<Timeline>((resolve) => resolvers.push(resolve)));
    const first = store.load();
    const second = store.load();
    resolvers[1]!(answer({ initiatives: [], tasks: [] }));
    await second;
    expect(store.state.loading).toBe(true);
    expect(store.state.model.groups).toEqual([]);
    resolvers[0]!(answer());
    await first;
    expect(store.state.loading).toBe(false);
    expect(store.state.model.groups).toEqual([]);
  });

  it('drops a failure that lands after a newer read', async () => {
    const pending: { resolve: (t: Timeline) => void; reject: (e: Error) => void }[] = [];
    const { store } = rig(() => new Promise<Timeline>((resolve, reject) => pending.push({ resolve, reject })));
    const first = store.load();
    const second = store.load();
    pending[1]!.resolve(answer());
    await second;
    pending[0]!.reject(new Error('late'));
    await first;
    expect(store.state.error).toBe('');
    expect(store.state.loaded).toBe(true);
  });
});

describe('chartOptions', () => {
  it('opens on the window, zooms from an hour to a quarter, stacks lease bars and orders rows by the mapping', () => {
    const w = initialWindow(NOW);
    expect(chartOptions(w)).toEqual({
      start: w.from,
      end: w.to,
      zoomMin: HOUR_MS,
      zoomMax: 92 * DAY_MS,
      orientation: 'top',
      stack: false,
      stackSubgroups: true,
      groupOrder: 'order',
      selectable: false,
      verticalScroll: true,
      maxHeight: '70vh',
    });
    expect(ZOOM_MIN_MS).toBe(HOUR_MS);
    expect(ZOOM_MAX_MS).toBe(92 * DAY_MS);
  });
});

describe('mountChart', () => {
  class FakeData<T> {
    rows: T[];
    constructor(rows: T[]) {
      this.rows = [...rows];
    }
    clear() {
      this.rows = [];
    }
    add(rows: T[]) {
      this.rows.push(...rows);
    }
  }
  function fakeLib() {
    const made: { container: unknown; items: FakeData<unknown>; groups: FakeData<unknown>; options: unknown; destroy: ReturnType<typeof vi.fn> }[] = [];
    class FakeTimeline {
      destroy = vi.fn();
      constructor(container: unknown, items: FakeData<unknown>, groups: FakeData<unknown>, options: unknown) {
        made.push({ container, items, groups, options, destroy: this.destroy });
      }
    }
    return { lib: { DataSet: FakeData, Timeline: FakeTimeline } as unknown as ChartLib, made };
  }
  const model = (refs: string[]): TimelineModel => ({
    groups: refs.map((ref, order) => ({ id: `t:${ref}`, ref, order }) as TimelineModel['groups'][number]),
    items: refs.map((ref) => ({ id: `act:${ref}`, group: `t:${ref}` }) as TimelineModel['items'][number]),
    dependencies: [],
  });

  it('hands the groups, items and options to vis-timeline in the container', () => {
    const { lib, made } = fakeLib();
    const container = {} as HTMLElement;
    const w = initialWindow(NOW);
    mountChart(lib, container, model(['A1', 'A2']), w);
    expect(made).toHaveLength(1);
    expect(made[0]!.container).toBe(container);
    expect(made[0]!.options).toEqual(chartOptions(w));
    expect(made[0]!.groups.rows.map((g) => (g as { id: string }).id)).toEqual(['t:A1', 't:A2']);
    expect(made[0]!.items.rows.map((i) => (i as { id: string }).id)).toEqual(['act:A1', 'act:A2']);
  });

  it('replaces what it draws on show and takes vis-timeline off the page on destroy', () => {
    const { lib, made } = fakeLib();
    const chart = mountChart(lib, {} as HTMLElement, model(['A1', 'A2']), initialWindow(NOW));
    chart.show(model(['B1']));
    expect(made[0]!.groups.rows.map((g) => (g as { id: string }).id)).toEqual(['t:B1']);
    expect(made[0]!.items.rows.map((i) => (i as { id: string }).id)).toEqual(['act:B1']);
    expect(made[0]!.destroy).not.toHaveBeenCalled();
    chart.destroy();
    expect(made[0]!.destroy).toHaveBeenCalledTimes(1);
  });
});
