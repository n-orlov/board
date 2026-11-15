import { describe, expect, it, vi } from 'vitest';
import type { Browser } from './session';
import type { ArrowPath, ArrowSurface, Box } from './timelineArrows';
import { apiQuery, DEFAULT_TIMELINE_FILTER, type Timeline, type TimelineModel } from './timeline';
import { viewKey, type ViewEnv, type ViewStorage } from './viewState';
import {
  canZoomIn,
  canZoomOut,
  CHART_SCREEN_SHARE,
  chartHeight,
  chartOptions,
  DEFAULT_CHART_HEIGHT,
  dropUnknown,
  MIN_CHART_HEIGHT,
  panBy,
  timeAt,
  timelineViewHash,
  WHEEL_ZOOM_RATE,
  wheelGesture,
  windowFromQuery,
  withoutWindow,
  zoomAround,
  forgetTimeline,
  clickedTask,
  DAY_MS,
  HOUR_MS,
  initialWindow,
  mountChart,
  openTimeline,
  PAN_DEBOUNCE_MS,
  sameRange,
  saveTimeline,
  spanLabel,
  spanOf,
  startFilter,
  TIMELINE_VIEW,
  TimelineApi,
  TimelineViewStore,
  ZOOM_FACTOR,
  ZOOM_MAX_MS,
  ZOOM_MIN_MS,
  type ChartClick,
  type ChartHooks,
  type ChartLib,
  type TimelineViewState,
  type Timers,
  type TimeWindow,
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
  it('lists the actor handles from GET /actors, same origin', async () => {
    const fetchFn = vi.fn(async (_input: string, _init?: RequestInit) => json({ items: [{ handle: 'ada' }, { handle: 'bob' }] }));
    const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname: '/board/', setHash: vi.fn() };
    expect(await new TimelineApi(browser, () => 't').actors()).toEqual(['ada', 'bob']);
    expect(fetchFn.mock.calls[0]![0]).toBe('/board/api/v1/actors');
    expect(fetchFn.mock.calls[0]![1]?.credentials).toBe('same-origin');
    expect(fetchFn.mock.calls[0]![1]?.headers).toEqual({ Accept: 'application/json' });
  });

  it('names what it was doing when GET /actors is refused without a problem body', async () => {
    const browser: Browser = { fetch: vi.fn(async () => new Response('', { status: 500 })), assign: vi.fn(), pathname: '/', setHash: vi.fn() };
    await expect(new TimelineApi(browser, () => 't').actors()).rejects.toThrow(/Listing the actors/);
  });

  it('turns a refusal of GET /actors into its problem text', async () => {
    const problem = new Response(JSON.stringify({ detail: 'unavailable' }), { status: 503, headers: { 'Content-Type': 'application/problem+json' } });
    const browser: Browser = { fetch: vi.fn(async () => problem), assign: vi.fn(), pathname: '/', setHash: vi.fn() };
    await expect(new TimelineApi(browser, () => 't').actors()).rejects.toThrow(/unavailable/);
  });

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

  it('opens on a window handed in (the fragment named one) and reads it', async () => {
    const api = { read: vi.fn(async () => answer()) };
    const w = { from: new Date('2026-01-01T00:00:00Z'), to: new Date('2026-01-08T00:00:00Z') };
    const store = new TimelineViewStore(api, () => undefined, DEFAULT_TIMELINE_FILTER, () => NOW, undefined, w);
    expect(store.state.window).toBe(w);
    await store.load();
    expect(api.read).toHaveBeenCalledWith(DEFAULT_TIMELINE_FILTER, w);
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
      maxHeight: DEFAULT_CHART_HEIGHT,
      zoomKey: 'shiftKey',
    });
    expect(chartOptions(w, 640).maxHeight).toBe(640);
    expect(ZOOM_MIN_MS).toBe(HOUR_MS);
    expect(ZOOM_MAX_MS).toBe(92 * DAY_MS);
  });
});

describe('chartHeight', () => {
  it('is the screen below the chart less what the page keeps under it, else a share of the screen, never below the minimum', () => {
    expect(chartHeight(900, 200, 80)).toBe(620);
    expect(chartHeight(900.6, 200, 0)).toBe(700);
    expect(chartHeight(844, 600, 80)).toBe(Math.floor(764 * CHART_SCREEN_SHARE)); // a phone: a share of the screen
    expect(chartHeight(380, 300, 0)).toBe(MIN_CHART_HEIGHT);
  });
});

describe('wheelGesture', () => {
  const wheel = (over: Partial<Parameters<typeof wheelGesture>[0]>) => ({ deltaX: 0, deltaY: 0, deltaMode: 0, shiftKey: false, ctrlKey: false, ...over });

  it('leaves a vertical wheel to vis-timeline, which scrolls the rows', () => {
    expect(wheelGesture(wheel({ deltaY: 100 }))).toEqual({ kind: 'rows' });
    expect(wheelGesture(wheel({ deltaY: -100, deltaX: 30 }))).toEqual({ kind: 'rows' });
  });
  it('pans the time axis on a mostly horizontal wheel', () => {
    expect(wheelGesture(wheel({ deltaX: 50, deltaY: 10 }))).toEqual({ kind: 'pan', px: 50 });
    expect(wheelGesture(wheel({ deltaX: -2, deltaMode: 1 }))).toEqual({ kind: 'pan', px: -80 });
  });
  it('zooms on Shift or a trackpad pinch (Ctrl), by the larger delta', () => {
    expect(wheelGesture(wheel({ deltaY: 100, shiftKey: true }))).toEqual({ kind: 'zoom', px: 100 });
    expect(wheelGesture(wheel({ deltaX: -100, shiftKey: true }))).toEqual({ kind: 'zoom', px: -100 }); // Shift turns the wheel sideways
    expect(wheelGesture(wheel({ deltaY: 3, ctrlKey: true }))).toEqual({ kind: 'zoom', px: 3 });
    expect(wheelGesture(wheel({ deltaY: 1, deltaMode: 2, shiftKey: true }))).toEqual({ kind: 'zoom', px: 800 });
  });
});

describe('zoomAround, panBy and timeAt', () => {
  const range = { start: 0, end: 10 * DAY_MS };

  it('zooms around the anchor, which keeps its place', () => {
    expect(zoomAround(range, 2 * DAY_MS, 0.5)).toEqual({ start: DAY_MS, end: 6 * DAY_MS });
    expect(zoomAround(range, 0, 2)).toEqual({ start: 0, end: 20 * DAY_MS });
  });
  it('holds the length between an hour and a quarter', () => {
    const tiny = zoomAround(range, 5 * DAY_MS, 1e-9);
    expect(tiny.end - tiny.start).toBe(ZOOM_MIN_MS);
    const huge = zoomAround(range, 5 * DAY_MS, 1e9);
    expect(huge.end - huge.start).toBe(ZOOM_MAX_MS);
  });
  it('zooms one wheel notch by about a fifth', () => {
    expect(Math.exp(100 * WHEEL_ZOOM_RATE)).toBeCloseTo(1.22, 2);
  });
  it('pans by the pixels as a share of the width', () => {
    expect(panBy(range, 100, 1000)).toEqual({ start: DAY_MS, end: 11 * DAY_MS });
    expect(panBy(range, -50, 1000)).toEqual({ start: -DAY_MS / 2, end: 9.5 * DAY_MS });
    expect(panBy(range, 100, 0)).toEqual(range);
  });
  it('finds the time under a point', () => {
    expect(timeAt(range, 250, 1000)).toBe(2.5 * DAY_MS);
    expect(timeAt(range, 250, 0)).toBe(5 * DAY_MS);
  });
});

describe('the window in the fragment', () => {
  const from = '2026-03-01T00:00:00Z';
  const to = '2026-03-15T00:00:00Z';

  it('reads a window with both ends, in order, within the zoom range', () => {
    expect(windowFromQuery(`tag=x&from=${from}&to=${to}`)).toEqual({ from: new Date(from), to: new Date(to) });
    expect(windowFromQuery(`from=${from}`)).toBeUndefined();
    expect(windowFromQuery(`from=${to}&to=${from}`)).toBeUndefined();
    expect(windowFromQuery(`from=nope&to=${to}`)).toBeUndefined();
    expect(windowFromQuery('from=2020-01-01T00:00:00Z&to=2026-01-01T00:00:00Z')).toBeUndefined();
  });
  it('strips the window from the filter query', () => {
    expect(withoutWindow(`tag=x&from=${from}&to=${to}`)).toBe('tag=x');
    expect(withoutWindow(`from=${from}&to=${to}`)).toBe('');
  });
  it('writes the filter, then the window to the second', () => {
    expect(timelineViewHash(DEFAULT_TIMELINE_FILTER)).toBe('#/timeline');
    const w = { from: new Date('2026-03-01T00:00:00.123Z'), to: new Date(to) };
    expect(timelineViewHash({ ...DEFAULT_TIMELINE_FILTER, tags: ['x'] }, w)).toBe(`#/timeline?tag=x&from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`);
    expect(windowFromQuery(timelineViewHash(DEFAULT_TIMELINE_FILTER, w).split('?')[1]!)).toEqual({ from: new Date(from), to: new Date(to) });
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
    getIds() {
      return this.rows.map((r) => (r as { id: string }).id);
    }
    remove(ids: (string | number)[]) {
      this.rows = this.rows.filter((r) => !ids.includes((r as { id: string }).id));
    }
    updates: string[][] = [];
    update(rows: T[]) {
      this.updates.push(rows.map((r) => (r as { id: string }).id));
      for (const row of rows) {
        const at = this.rows.findIndex((r) => (r as { id: string }).id === (row as { id: string }).id);
        if (at < 0) this.rows.push(row);
        else this.rows[at] = { ...this.rows[at], ...row };
      }
    }
  }
  type Listener = (props?: ChartClick) => void;
  function fakeLib() {
    class FakeTimeline {
      destroy = vi.fn();
      zoomIn = vi.fn();
      zoomOut = vi.fn();
      redraw = vi.fn();
      setWindow = vi.fn();
      setOptions = vi.fn();
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
    const ranges: TimeWindow[] = [];
    const h: ChartHooks = { open: (ref) => opened.push(ref), arrows, onSpan: (ms) => spans.push(ms), onRange: (r) => ranges.push(r) };
    return { h, opened, spans, drawn, boxes, ranges };
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

  /** A container like vis-timeline's: a centre panel 1000 px wide at x 100, a label panel that scrolls the rows. */
  function fakeContainer() {
    const listeners = new Map<string, (e: unknown) => void>();
    const left = { scrollTop: 0, scrollHeight: 2000, clientHeight: 500 };
    const centre = { getBoundingClientRect: () => ({ left: 100, width: 1000 }) };
    const container = {
      addEventListener: (type: string, fn: (e: unknown) => void) => listeners.set(type, fn),
      removeEventListener: (type: string) => listeners.delete(type),
      querySelector: (sel: string) => (sel === '.vis-panel.vis-center' ? centre : sel === '.vis-panel.vis-left' ? left : null),
    };
    const wheel = (over: object) => {
      const e = { deltaX: 0, deltaY: 0, deltaMode: 0, shiftKey: false, ctrlKey: false, clientX: 100, preventDefault: vi.fn(), stopPropagation: vi.fn(), ...over };
      listeners.get('wheel')?.(e);
      return e;
    };
    return { container: container as unknown as HTMLElement, listeners, left, wheel };
  }

  it('pans on a sideways wheel and zooms around the pointer on Shift+wheel, leaving a vertical wheel to vis-timeline', () => {
    const { lib, made } = fakeLib();
    const c = fakeContainer();
    mountChart(lib, c.container, model(['A1']), initialWindow(NOW), hooks().h);
    const t = made[0]!; // window 0 .. 2 days

    const down = c.wheel({ deltaY: 100 });
    expect(down.preventDefault).not.toHaveBeenCalled();
    expect(t.setWindow).not.toHaveBeenCalled();

    const side = c.wheel({ deltaX: 100 });
    expect(side.preventDefault).toHaveBeenCalled();
    const [panStart, panEnd, options] = t.setWindow.mock.lastCall as [number, number, object];
    expect(panStart).toBeCloseTo(0.2 * DAY_MS);
    expect(panEnd).toBeCloseTo(2.2 * DAY_MS);
    expect(options).toEqual({ animation: false });

    const zoom = c.wheel({ deltaY: -100, shiftKey: true, clientX: 100 }); // the pointer at the left edge: it stays put
    expect(zoom.stopPropagation).toHaveBeenCalled();
    const [start, end] = t.setWindow.mock.lastCall as [number, number];
    expect(start).toBe(0);
    expect(end).toBeCloseTo(2 * DAY_MS * Math.exp(-100 * WHEEL_ZOOM_RATE));
  });

  it('reports the rows scrolling and puts a saved scroll back once the rows are tall enough', () => {
    vi.useFakeTimers();
    try {
      const { lib, made } = fakeLib();
      const c = fakeContainer();
      const rowsSeen: number[] = [];
      const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), { ...hooks().h, onRowScroll: (px) => rowsSeen.push(px) });
      c.left.scrollHeight = 0;
      chart.scrollRows(900);
      made[0]!.emit('changed');
      expect(c.left.scrollTop).toBe(0); // not drawn yet
      c.left.scrollHeight = 2000;
      made[0]!.emit('changed');
      expect(c.left.scrollTop).toBe(900);

      c.left.scrollTop = 300;
      c.listeners.get('scroll')?.({});
      c.listeners.get('scroll')?.({});
      vi.advanceTimersByTime(150);
      expect(rowsSeen).toEqual([300]);
      chart.destroy();
      expect(c.listeners.size).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });

  it('moves to a window and sets its height through vis-timeline', () => {
    const { lib, made } = fakeLib();
    const chart = mountChart(lib, {} as HTMLElement, model(['A1']), initialWindow(NOW), hooks().h, 700);
    expect((made[0]!.options as { maxHeight: number }).maxHeight).toBe(700);
    chart.setHeight(500);
    expect(made[0]!.setOptions).toHaveBeenCalledWith({ maxHeight: 500 });
    const w = { from: new Date(0), to: new Date(DAY_MS) };
    chart.moveTo(w);
    expect(made[0]!.setWindow).toHaveBeenCalledWith(w.from, w.to, { animation: false });
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

  it('keeps what vis-timeline holds about a row that stays: a collapse is not undone by a refresh', () => {
    const { lib, made } = fakeLib();
    const withNested = (showNested: boolean, content = 'A'): TimelineModel => ({
      groups: [{ id: 'i:a', ref: 'a', order: 0, content, nestedGroups: ['t:A1'], showNested } as unknown as TimelineModel['groups'][number]],
      items: [],
      dependencies: [],
    });
    const chart = mountChart(lib, {} as HTMLElement, withNested(true), initialWindow(NOW), hooks().h);
    (made[0]!.groups.rows[0] as { showNested?: boolean }).showNested = false; // vis-timeline's own toggle
    chart.show(withNested(true, 'renamed')); // changed: merged without the collapse flag
    expect(made[0]!.groups.rows[0]).toMatchObject({ content: 'renamed', showNested: false });
    chart.show({ groups: [...withNested(true, 'renamed').groups, { id: 'i:b', ref: 'b', order: 1, showNested: true } as unknown as TimelineModel['groups'][number]], items: [], dependencies: [] });
    expect(made[0]!.groups.rows.map((g) => (g as { id: string; showNested?: boolean }).showNested)).toEqual([false, true]);
  });

  it('touches only the rows that changed, so an answer that changed nothing disturbs nothing', () => {
    const { lib, made } = fakeLib();
    const chart = mountChart(lib, {} as HTMLElement, model(['A1', 'A2']), initialWindow(NOW), hooks().h);
    chart.show(model(['A1', 'A2']));
    expect(made[0]!.items.updates.at(-1)).toEqual([]);
    expect(made[0]!.groups.updates.at(-1)).toEqual([]);
    const changed = model(['A1', 'A2']);
    changed.items[1] = { ...changed.items[1]!, title: 'new title' };
    chart.show(changed);
    expect(made[0]!.items.updates.at(-1)).toEqual(['act:A2']);
    expect(made[0]!.groups.updates.at(-1)).toEqual([]);
    chart.show(model(['A1', 'A2', 'A3']));
    expect(made[0]!.items.updates.at(-1)).toEqual(['act:A2', 'act:A3']); // A2 changed back, A3 is new
  });

  it('merges new data into the rows it holds and drops the ones that are gone', () => {
    const { lib, made } = fakeLib();
    const chart = mountChart(lib, {} as HTMLElement, model(['A1', 'A2']), initialWindow(NOW), hooks().h);
    chart.show(model(['A2', 'A3']));
    expect(made[0]!.groups.rows.map((g) => (g as { id: string }).id)).toEqual(['t:A2', 't:A3']);
    expect(made[0]!.items.rows.map((i) => (i as { id: string }).id)).toEqual(['act:A2', 'act:A3']);
  });

  it('tells the view the visible range once it stops moving, not while it moves', () => {
    const { lib, made } = fakeLib();
    const h = hooks();
    mountChart(lib, {} as HTMLElement, model(['A1']), initialWindow(NOW), h.h);
    made[0]!.window = { start: new Date(DAY_MS), end: new Date(3 * DAY_MS) };
    made[0]!.emit('rangechange');
    made[0]!.emit('changed');
    expect(h.ranges).toEqual([]);
    made[0]!.emit('rangechanged');
    expect(h.ranges).toEqual([{ from: new Date(DAY_MS), to: new Date(3 * DAY_MS) }]);
  });
});

describe('sameRange', () => {
  const w = initialWindow(NOW);
  it('holds a range within 1% of its length at both ends', () => {
    const slack = 21 * DAY_MS * 0.01;
    expect(sameRange(w, { from: new Date(w.from.getTime() + slack), to: new Date(w.to.getTime() - slack) })).toBe(true);
  });
  it('is false once either end moves further', () => {
    const over = 21 * DAY_MS * 0.01 + 1;
    expect(sameRange(w, { from: new Date(w.from.getTime() + over), to: w.to })).toBe(false);
    expect(sameRange(w, { from: w.from, to: new Date(w.to.getTime() + over) })).toBe(false);
    expect(sameRange(w, { from: new Date(w.from.getTime() - over), to: w.to })).toBe(false);
    expect(sameRange(w, { from: w.from, to: new Date(w.to.getTime() - over) })).toBe(false);
  });
});

describe('TimelineViewStore panning', () => {
  function timers() {
    const pending = new Map<number, () => void>();
    let next = 1;
    const cleared: number[] = [];
    const t: Timers = {
      set: (fn, ms) => {
        expect(ms).toBe(PAN_DEBOUNCE_MS);
        pending.set(next, fn);
        return next++;
      },
      clear: (h) => {
        cleared.push(h as number);
        pending.delete(h as number);
      },
    };
    return { t, pending, cleared, fire: () => [...pending.values()].forEach((fn) => fn()) };
  }
  const moved = (days: number): TimeWindow => {
    const w = initialWindow(NOW);
    return { from: new Date(w.from.getTime() + days * DAY_MS), to: new Date(w.to.getTime() + days * DAY_MS) };
  };

  it('reads the new range once the pan has been still for the debounce, not before', async () => {
    const tm = timers();
    const api = { read: vi.fn(async () => answer()) };
    const store = new TimelineViewStore(api, () => undefined, DEFAULT_TIMELINE_FILTER, () => NOW, tm.t);
    store.panTo(moved(5));
    expect(api.read).not.toHaveBeenCalled();
    expect(store.state.window).toEqual(initialWindow(NOW));
    tm.fire();
    await vi.waitFor(() => expect(api.read).toHaveBeenCalledTimes(1));
    expect(api.read).toHaveBeenCalledWith(DEFAULT_TIMELINE_FILTER, moved(5));
    expect(store.state.window).toEqual(moved(5));
  });

  it('reads only the last range of a burst', async () => {
    const tm = timers();
    const api = { read: vi.fn(async () => answer()) };
    const store = new TimelineViewStore(api, () => undefined, DEFAULT_TIMELINE_FILTER, () => NOW, tm.t);
    store.panTo(moved(2));
    store.panTo(moved(4));
    store.panTo(moved(6));
    expect(tm.cleared).toEqual([1, 2]);
    expect(tm.pending.size).toBe(1);
    tm.fire();
    await vi.waitFor(() => expect(api.read).toHaveBeenCalledTimes(1));
    expect(api.read).toHaveBeenCalledWith(DEFAULT_TIMELINE_FILTER, moved(6));
  });

  it('debounces on the browser timers when none are handed in, and cancels with them', async () => {
    vi.useFakeTimers();
    try {
      const api = { read: vi.fn(async () => answer()) };
      const store = new TimelineViewStore(api, () => undefined, DEFAULT_TIMELINE_FILTER, () => NOW);
      store.panTo(moved(5));
      store.cancelPan();
      vi.advanceTimersByTime(PAN_DEBOUNCE_MS * 2);
      expect(api.read).not.toHaveBeenCalled();
      store.panTo(moved(5));
      vi.advanceTimersByTime(PAN_DEBOUNCE_MS - 1);
      expect(api.read).not.toHaveBeenCalled();
      vi.advanceTimersByTime(1);
      expect(api.read).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('reads nothing for the range it already holds, and a pan back drops the pending read', () => {
    const tm = timers();
    const api = { read: vi.fn(async () => answer()) };
    const store = new TimelineViewStore(api, () => undefined, DEFAULT_TIMELINE_FILTER, () => NOW, tm.t);
    store.panTo(initialWindow(NOW));
    expect(tm.pending.size).toBe(0);
    store.panTo(moved(5));
    store.panTo(initialWindow(NOW));
    expect(tm.pending.size).toBe(0);
    expect(api.read).not.toHaveBeenCalled();
  });

  it('drops the pending read when the view goes away', () => {
    const tm = timers();
    const store = new TimelineViewStore({ read: vi.fn(async () => answer()) }, () => undefined, DEFAULT_TIMELINE_FILTER, () => NOW, tm.t);
    store.cancelPan();
    expect(tm.cleared).toEqual([]);
    store.panTo(moved(5));
    store.cancelPan();
    expect(tm.cleared).toEqual([1]);
    expect(tm.pending.size).toBe(0);
  });

  it('keeps the panned window when the filter changes or a live event refreshes', async () => {
    const tm = timers();
    const api = { read: vi.fn(async () => answer()) };
    const store = new TimelineViewStore(api, () => undefined, DEFAULT_TIMELINE_FILTER, () => NOW, tm.t);
    store.panTo(moved(5));
    tm.fire();
    await vi.waitFor(() => expect(api.read).toHaveBeenCalledTimes(1));
    await store.setFilter({ ...DEFAULT_TIMELINE_FILTER, actor: 'ada' });
    await store.load();
    expect(api.read).toHaveBeenLastCalledWith({ ...DEFAULT_TIMELINE_FILTER, actor: 'ada' }, moved(5));
  });
});

function memory(initial: Record<string, string> = {}): ViewStorage & { data: Map<string, string> } {
  const data = new Map(Object.entries(initial));
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

function viewEnv(storage: ViewStorage | undefined): ViewEnv & { hashes: string[] } {
  const hashes: string[] = [];
  return { storage, replaceHash: (h) => void hashes.push(h), hashes };
}

describe('saved Timeline filters', () => {
  const KEY = 'board.view.v1.timeline';
  const SAVED = 'initiative=ghost&tag=gone&tag=work&actor=bob&closed=30';

  it('is saved under board.view.v1.timeline', () => {
    expect(viewKey(TIMELINE_VIEW)).toBe(KEY);
  });

  it('saves the filter as the fragment query and removes the key for the empty filter', () => {
    const storage = memory();
    saveTimeline(storage, { initiatives: ['aa'], tags: ['work'], actor: 'bob', closedWithin: 7 });
    expect(JSON.parse(storage.data.get(KEY)!)).toEqual({ v: 1, query: 'initiative=aa&tag=work&actor=bob&closed=7' });
    saveTimeline(storage, DEFAULT_TIMELINE_FILTER);
    expect(storage.data.has(KEY)).toBe(false);
  });

  it('restores a bare route in place, before any fetch, and reports it as restored', () => {
    const e = viewEnv(memory({ [KEY]: JSON.stringify({ v: 1, query: 'tag=work&actor=bob&closed=30' }) }));
    expect(openTimeline(e, '#/timeline')).toEqual({ filter: { initiatives: [], tags: ['work'], actor: 'bob', closedWithin: 30 }, restored: true });
    expect(e.hashes).toEqual(['#/timeline?tag=work&actor=bob&closed=30']);
  });

  it('lets a fragment with its own query win and saves it, without restoring', () => {
    const storage = memory({ [KEY]: JSON.stringify({ v: 1, query: 'tag=old' }) });
    const e = viewEnv(storage);
    expect(openTimeline(e, '#/timeline?tag=new')).toEqual({ filter: { ...DEFAULT_TIMELINE_FILTER, tags: ['new'] }, restored: false });
    expect(e.hashes).toEqual([]);
    expect(JSON.parse(storage.data.get(KEY)!).query).toBe('tag=new');
  });

  it('starts at the defaults with nothing saved, a malformed value, another version or no storage', () => {
    for (const raw of [undefined, 'nope', '{"v":2,"query":"tag=x"}', '{"v":1,"query":5}']) {
      const e = viewEnv(memory(raw === undefined ? {} : { [KEY]: raw }));
      expect(openTimeline(e, '#/timeline')).toEqual({ filter: DEFAULT_TIMELINE_FILTER, restored: false });
      expect(e.hashes).toEqual([]);
    }
    expect(openTimeline(viewEnv(undefined), '#/timeline').restored).toBe(false);
  });

  it('opens a fragment that names a window on it, with its own filter and nothing restored or saved', () => {
    const storage = memory({ [KEY]: JSON.stringify({ v: 1, query: 'tag=saved' }) });
    const e = viewEnv(storage);
    const from = '2026-03-01T00:00:00Z';
    const to = '2026-03-08T00:00:00Z';
    expect(openTimeline(e, `#/timeline?from=${from}&to=${to}`)).toEqual({ filter: DEFAULT_TIMELINE_FILTER, restored: false, window: { from: new Date(from), to: new Date(to) } });
    expect(e.hashes).toEqual([]);
    expect(JSON.parse(storage.data.get(KEY)!).query).toBe('tag=saved');
    expect(openTimeline(e, `#/timeline?tag=x&from=${from}&to=${to}`)).toEqual({ filter: { ...DEFAULT_TIMELINE_FILTER, tags: ['x'] }, restored: false, window: { from: new Date(from), to: new Date(to) } });
    expect(JSON.parse(storage.data.get(KEY)!).query).toBe('tag=x');
  });

  it('forgets the saved filter', () => {
    const storage = memory({ [KEY]: JSON.stringify({ v: 1, query: 'tag=x' }) });
    forgetTimeline(storage);
    expect(storage.data.has(KEY)).toBe(false);
    expect(openTimeline(viewEnv(storage), '#/timeline').restored).toBe(false);
  });

  const known = [
    { slug: 'aa', tags: ['work'] },
    { slug: 'bb', tags: ['home', 'work'] },
  ];

  it('restores through the module, drops the deleted initiative and the removed tag, and keeps the valid actor and closed_within', () => {
    const opened = openTimeline(viewEnv(memory({ [KEY]: JSON.stringify({ v: 1, query: SAVED }) })), '#/timeline');
    expect(opened.filter).toEqual({ initiatives: ['ghost'], tags: ['gone', 'work'], actor: 'bob', closedWithin: 30 });
    const kept = startFilter(opened, known);
    expect(kept).toEqual({ initiatives: [], tags: ['work'], actor: 'bob', closedWithin: 30 });
    const read = apiQuery(kept, new Date('2026-01-01T00:00:00Z'), new Date('2026-01-08T00:00:00Z'));
    expect(read.getAll('initiative')).toEqual([]);
    expect(read.getAll('tag')).toEqual(['work']);
    expect(read.get('actor')).toBe('bob');
    expect(read.get('closed_within')).toBe('30d');
  });

  it('drops every restored initiative and tag when the lookup answered with no initiatives at all', () => {
    const opened = openTimeline(viewEnv(memory({ [KEY]: JSON.stringify({ v: 1, query: SAVED }) })), '#/timeline');
    expect(startFilter(opened, [])).toEqual({ initiatives: [], tags: [], actor: 'bob', closedWithin: 30 });
  });

  it('keeps a restored filter as it is when the lookup failed', () => {
    const opened = openTimeline(viewEnv(memory({ [KEY]: JSON.stringify({ v: 1, query: SAVED }) })), '#/timeline');
    expect(startFilter(opened, undefined)).toBe(opened.filter);
  });

  it('keeps the values a link names, even unknown ones', () => {
    const opened = openTimeline(viewEnv(memory()), '#/timeline?initiative=ghost&tag=gone');
    expect(opened.restored).toBe(false);
    expect(startFilter(opened, known)).toBe(opened.filter);
    expect(startFilter(opened, [])).toBe(opened.filter);
  });

  it('keeps a filter whose values are all known, as the same object', () => {
    const filter = { initiatives: ['bb'], tags: ['home', 'work'], actor: 'bob', closedWithin: 7 as const };
    expect(dropUnknown(filter, known)).toBe(filter);
    expect(dropUnknown(DEFAULT_TIMELINE_FILTER, known)).toBe(DEFAULT_TIMELINE_FILTER);
  });

  it('drops an unknown initiative and keeps the known one and the tags', () => {
    expect(dropUnknown({ ...DEFAULT_TIMELINE_FILTER, initiatives: ['ghost', 'aa'], tags: ['work'] }, known)).toEqual({ ...DEFAULT_TIMELINE_FILTER, initiatives: ['aa'], tags: ['work'] });
  });

  it('drops an unknown tag and keeps the known one and the initiatives', () => {
    expect(dropUnknown({ ...DEFAULT_TIMELINE_FILTER, initiatives: ['aa'], tags: ['gone', 'home'] }, known)).toEqual({ ...DEFAULT_TIMELINE_FILTER, initiatives: ['aa'], tags: ['home'] });
  });
});
