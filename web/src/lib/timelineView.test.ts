import { describe, expect, it, vi } from 'vitest';
import type { Browser } from './session';
import type { ArrowPath, ArrowSurface, Box } from './timelineArrows';
import { DEFAULT_TIMELINE_FILTER, type Timeline, type TimelineModel } from './timeline';
import {
  canZoomIn,
  canZoomOut,
  chartOptions,
  clickedTask,
  DAY_MS,
  HOUR_MS,
  initialWindow,
  mountChart,
  spanLabel,
  spanOf,
  TimelineApi,
  TimelineViewStore,
  ZOOM_FACTOR,
  ZOOM_MAX_MS,
  ZOOM_MIN_MS,
  type ChartClick,
  type ChartHooks,
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
    });
    expect(ZOOM_MIN_MS).toBe(HOUR_MS);
    expect(ZOOM_MAX_MS).toBe(92 * DAY_MS);
  });
});

describe('zoom', () => {
  it('measures the range and limits the buttons at an hour and a quarter', () => {
    expect(spanOf({ start: new Date(1000), end: new Date(4000) })).toBe(3000);
    expect(canZoomIn(2 * HOUR_MS)).toBe(true);
    expect(canZoomIn(HOUR_MS)).toBe(false);
    expect(canZoomIn(HOUR_MS * 1.01)).toBe(false); // vis-timeline clamps with a little rounding
    expect(canZoomIn(HOUR_MS * 1.02)).toBe(false);
    expect(canZoomIn(HOUR_MS * 1.03)).toBe(true);
    expect(canZoomOut(30 * DAY_MS)).toBe(true);
    expect(canZoomOut(ZOOM_MAX_MS)).toBe(false);
    expect(canZoomOut(ZOOM_MAX_MS * 0.99)).toBe(false);
    expect(canZoomOut(ZOOM_MAX_MS / 1.02)).toBe(false);
    expect(canZoomOut(ZOOM_MAX_MS / 1.03)).toBe(true);
  });

  it('says the visible range as words in hours, days, weeks and months', () => {
    expect(spanLabel(HOUR_MS)).toBe('1 hour');
    expect(spanLabel(0)).toBe('1 hour');
    expect(spanLabel(6 * HOUR_MS)).toBe('6 hours');
    expect(spanLabel(DAY_MS)).toBe('1 day');
    expect(spanLabel(3.4 * DAY_MS)).toBe('3 days');
    expect(spanLabel(14 * DAY_MS)).toBe('2 weeks');
    expect(spanLabel(21 * DAY_MS)).toBe('3 weeks');
    expect(spanLabel(60 * DAY_MS)).toBe('2 months');
    expect(spanLabel(92 * DAY_MS)).toBe('3 months');
  });
});

describe('clickedTask', () => {
  const items = new Map([
    ['act:T1', { id: 'act:T1', task: 'T1' }],
    ['lease:L1', { id: 'lease:L1', task: 'T2', lease: 'L1' }],
  ]);
  const itemOf = (id: string) => items.get(id) as TimelineModel['items'][number] | undefined;

  it('opens the task of a bar of any kind', () => {
    expect(clickedTask({ what: 'item', item: 'act:T1' }, itemOf)).toBe('T1');
    expect(clickedTask({ what: 'item', item: 'lease:L1' }, itemOf)).toBe('T2');
    expect(clickedTask({ what: 'item', item: 'gone' }, itemOf)).toBeUndefined();
    expect(clickedTask({ what: 'item', item: null }, itemOf)).toBeUndefined();
    expect(clickedTask({ what: 'item' }, itemOf)).toBeUndefined();
  });

  it('opens the task of a task row label and leaves an initiative row to collapse', () => {
    expect(clickedTask({ what: 'group-label', group: 't:T9' }, itemOf)).toBe('T9');
    expect(clickedTask({ what: 'group-label', group: 'i:alpha' }, itemOf)).toBeUndefined();
    expect(clickedTask({ what: 'group-label', group: null }, itemOf)).toBeUndefined();
    expect(clickedTask({ what: 'group-label' }, itemOf)).toBeUndefined();
  });

  it('opens nothing for the background, the axis or a click on nothing', () => {
    for (const what of ['background', 'axis', 'custom-time', null]) expect(clickedTask({ what, item: 'act:T1', group: 't:T1' }, itemOf)).toBeUndefined();
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
  type Listener = (props?: ChartClick) => void;
  function fakeLib() {
    class FakeTimeline {
      destroy = vi.fn();
      zoomIn = vi.fn();
      zoomOut = vi.fn();
      redraw = vi.fn();
      window = { start: new Date(0), end: new Date(2 * DAY_MS) };
      listeners = new Map<string, Listener>();
      constructor(
        readonly container: unknown,
        readonly items: FakeData<unknown>,
        readonly groups: FakeData<unknown>,
        readonly options: unknown,
      ) {
        made.push(this);
      }
      on(event: string, listener: Listener) {
        this.listeners.set(event, listener);
      }
      getWindow() {
        return this.window;
      }
      emit(event: string, props?: ChartClick) {
        this.listeners.get(event)?.(props);
      }
    }
    const made: FakeTimeline[] = [];
    return { lib: { DataSet: FakeData, Timeline: FakeTimeline } as unknown as ChartLib, made };
  }
  const model = (refs: string[], dependencies: TimelineModel['dependencies'] = []): TimelineModel => ({
    groups: refs.map((ref, order) => ({ id: `t:${ref}`, ref, order }) as TimelineModel['groups'][number]),
    items: refs.map((ref) => ({ id: `act:${ref}`, group: `t:${ref}`, task: ref }) as TimelineModel['items'][number]),
    dependencies,
  });
  function hooks() {
    const opened: string[] = [];
    const spans: number[] = [];
    const drawn: ArrowPath[][] = [];
    const boxes = new Map<string, Box>();
    const arrows: ArrowSurface = { boxOf: (id) => boxes.get(id), render: (paths) => drawn.push([...paths]) };
    const h: ChartHooks = { open: (ref) => opened.push(ref), arrows, onSpan: (ms) => spans.push(ms) };
    return { h, opened, spans, drawn, boxes };
  }

  it('hands the groups, items and options to vis-timeline in the container', () => {
    const { lib, made } = fakeLib();
    const container = {} as HTMLElement;
    const w = initialWindow(NOW);
    mountChart(lib, container, model(['A1', 'A2']), w, hooks().h);
    expect(made).toHaveLength(1);
    expect(made[0]!.container).toBe(container);
    expect(made[0]!.options).toEqual(chartOptions(w));
    expect(made[0]!.groups.rows.map((g) => (g as { id: string }).id)).toEqual(['t:A1', 't:A2']);
    expect(made[0]!.items.rows.map((i) => (i as { id: string }).id)).toEqual(['act:A1', 'act:A2']);
  });

  it('replaces what it draws on show and takes vis-timeline off the page on destroy', () => {
    const { lib, made } = fakeLib();
    const chart = mountChart(lib, {} as HTMLElement, model(['A1', 'A2']), initialWindow(NOW), hooks().h);
    chart.show(model(['B1']));
    expect(made[0]!.groups.rows.map((g) => (g as { id: string }).id)).toEqual(['t:B1']);
    expect(made[0]!.items.rows.map((i) => (i as { id: string }).id)).toEqual(['act:B1']);
    expect(made[0]!.destroy).not.toHaveBeenCalled();
    chart.destroy();
    expect(made[0]!.destroy).toHaveBeenCalledTimes(1);
  });

  it('zooms in and out by the factor, without animation', () => {
    const { lib, made } = fakeLib();
    const chart = mountChart(lib, {} as HTMLElement, model(['A1']), initialWindow(NOW), hooks().h);
    chart.zoom('in');
    expect(made[0]!.zoomIn).toHaveBeenCalledWith(ZOOM_FACTOR, { animation: false });
    expect(made[0]!.zoomOut).not.toHaveBeenCalled();
    chart.zoom('out');
    expect(made[0]!.zoomOut).toHaveBeenCalledWith(ZOOM_FACTOR, { animation: false });
    expect(made[0]!.zoomIn).toHaveBeenCalledTimes(1);
  });

  it('opens the task of a click on a bar or a task row, from the model last shown', () => {
    const { lib, made } = fakeLib();
    const h = hooks();
    const chart = mountChart(lib, {} as HTMLElement, model(['A1']), initialWindow(NOW), h.h);
    made[0]!.emit('click', { what: 'item', item: 'act:A1' });
    made[0]!.emit('click', { what: 'group-label', group: 't:A7' });
    made[0]!.emit('click', { what: 'group-label', group: 'i:alpha' });
    made[0]!.emit('click', { what: 'background' });
    expect(h.opened).toEqual(['A1', 'A7']);
    chart.show(model(['B1']));
    made[0]!.emit('click', { what: 'item', item: 'act:A1' }); // no longer drawn
    made[0]!.emit('click', { what: 'item', item: 'act:B1' });
    expect(h.opened).toEqual(['A1', 'A7', 'B1']);
  });

  it('reports the span and redraws the arrows after every redraw and every range change, and redraws vis-timeline once the range settles', () => {
    const { lib, made } = fakeLib();
    const h = hooks();
    const pair = { id: 'dep:A1>A2', from: 'A1', fromItem: 'act:A1', to: 'A2', toItem: 'act:A2' };
    const chart = mountChart(lib, {} as HTMLElement, model(['A1', 'A2'], [pair]), initialWindow(NOW), h.h);
    h.boxes.set('act:A1', { left: 0, top: 0, right: 10, bottom: 10 });
    h.boxes.set('act:A2', { left: 30, top: 20, right: 40, bottom: 30 });
    made[0]!.emit('changed');
    expect(h.spans).toEqual([2 * DAY_MS]);
    expect(h.drawn).toHaveLength(1);
    expect(h.drawn[0]!.map((p) => p.id)).toEqual(['dep:A1>A2']);

    made[0]!.window = { start: new Date(0), end: new Date(DAY_MS) };
    made[0]!.emit('rangechange');
    expect(h.spans).toEqual([2 * DAY_MS, DAY_MS]);
    expect(made[0]!.redraw).not.toHaveBeenCalled();
    made[0]!.emit('rangechanged');
    expect(made[0]!.redraw).toHaveBeenCalledTimes(1);
    expect(h.spans).toHaveLength(3);
    expect(h.drawn).toHaveLength(3);

    chart.show(model(['A1', 'A2'])); // the new model has no dependency: the next redraw draws no arrow
    made[0]!.emit('changed');
    expect(h.drawn[h.drawn.length - 1]).toEqual([]);
  });
});
