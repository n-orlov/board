import { describe, expect, it, vi } from 'vitest';
import type { Browser } from './session';
import type { ArrowPath, ArrowSurface, Box } from './timelineArrows';
import { apiQuery, DEFAULT_TIMELINE_FILTER, type Timeline, type TimelineModel } from './timeline';
import { RESTORE_WAIT_MS } from './historyEntry';
import { viewKey, type ViewEnv, type ViewStorage } from './viewState';
import {
  canZoomIn,
  canZoomOut,
  CHART_SCREEN_SHARE,
  chartHeight,
  chartOptions,
  DEFAULT_CHART_HEIGHT,
  dropUnknown,
  forgetTimeline,
  clickedTask,
  collapseAfter,
  collapsedOf,
  encodeCollapsed,
  withCollapsed,
  DAY_MS,
  HOUR_MS,
  gestureKind,
  gestureTracker,
  nearestBar,
  pressOpens,
  TAP_REACH_PX,
  type Press,
  initialWindow,
  MIN_CHART_HEIGHT,
  mountChart,
  panBy,
  openTimeline,
  PAN_DEBOUNCE_MS,
  sameRange,
  saveTimeline,
  spanLabel,
  spanOf,
  startFilter,
  TAP_SLOP_PX,
  timeAt,
  TIMELINE_VIEW,
  TimelineApi,
  TimelineViewStore,
  WHEEL_ZOOM_RATE,
  wheelGesture,
  zoomAround,
  ZOOM_FACTOR,
  ZOOM_MAX_MS,
  ZOOM_MIN_MS,
  type ChartClick,
  type ChartHooks,
  type ChartLib,
  type TimelineViewState,
  type Timers,
  type TimeWindow,
  timelineViewHash,
  windowFromQuery,
  withoutWindow,
  taskFromQuery,
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
    expect(url).toBe('/board/api/v1/timeline?from=2026-02-24T12%3A00%3A00.000Z&to=2026-03-17T12%3A00%3A00.000Z&rows=all&actor=ada&closed_within=30d');
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

  it('opens on a window handed in (the fragment named one) and reads it', async () => {
    const w = { from: new Date('2026-01-01T00:00:00Z'), to: new Date('2026-01-08T00:00:00Z') };
    const api = { read: vi.fn(async () => answer()) };
    const store = new TimelineViewStore(api, () => undefined, DEFAULT_TIMELINE_FILTER, () => NOW, undefined, w);
    expect(store.state.window).toBe(w);
    await store.load();
    expect(api.read).toHaveBeenCalledWith(DEFAULT_TIMELINE_FILTER, w);
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
  it('opens on the window, zooms from an hour to a quarter, never stacks and orders rows by the mapping', () => {
    const w = initialWindow(NOW);
    expect(chartOptions(w)).toEqual({
      start: w.from,
      end: w.to,
      zoomMin: HOUR_MS,
      zoomMax: 92 * DAY_MS,
      orientation: 'top',
      stack: false,
      margin: { item: { horizontal: 10, vertical: 0 } },
      groupOrder: 'order',
      selectable: false,
      verticalScroll: true,
      maxHeight: DEFAULT_CHART_HEIGHT,
      zoomKey: 'shiftKey',
      dataAttributes: ['task', 'id'],
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

  it('leaves a vertical wheel to vis-timeline, which scrolls the rows, and never zooms on a plain wheel', () => {
    expect(wheelGesture(wheel({ deltaY: 100 }))).toEqual({ kind: 'rows' });
    expect(wheelGesture(wheel({ deltaY: -100, deltaX: 30 }))).toEqual({ kind: 'rows' });
    for (const [deltaX, deltaY, deltaMode] of [[0, 400, 0], [300, 0, 0], [0, -3, 1], [5, 120, 0], [-9, 1, 2]]) {
      expect(wheelGesture(wheel({ deltaX, deltaY, deltaMode })).kind).not.toBe('zoom');
    }
  });
  it('pans the time axis on a mostly horizontal wheel', () => {
    expect(wheelGesture(wheel({ deltaX: 50, deltaY: 10 }))).toEqual({ kind: 'pan', px: 50 });
    expect(wheelGesture(wheel({ deltaX: -2, deltaMode: 1 }))).toEqual({ kind: 'pan', px: -80 });
  });
  it('zooms on Shift, by the larger delta (Shift turns the wheel sideways)', () => {
    expect(wheelGesture(wheel({ deltaY: 100, shiftKey: true }))).toEqual({ kind: 'zoom', px: 100 });
    expect(wheelGesture(wheel({ deltaX: -100, shiftKey: true }))).toEqual({ kind: 'zoom', px: -100 });
    expect(wheelGesture(wheel({ deltaY: 1, deltaMode: 2, shiftKey: true }))).toEqual({ kind: 'zoom', px: 800 });
  });
  it('zooms on Ctrl, which is how a trackpad pinch arrives', () => {
    expect(wheelGesture(wheel({ deltaY: 3, ctrlKey: true }))).toEqual({ kind: 'zoom', px: 3 });
    expect(wheelGesture(wheel({ deltaY: -40, deltaX: 2, ctrlKey: true }))).toEqual({ kind: 'zoom', px: -40 });
  });
});

describe('zoomAround, panBy and timeAt', () => {
  const range = { start: 0, end: 10 * DAY_MS };

  it('zooms around the anchor, which keeps its place', () => {
    expect(zoomAround(range, 2 * DAY_MS, 0.5)).toEqual({ start: DAY_MS, end: 6 * DAY_MS });
    expect(zoomAround(range, 0, 2)).toEqual({ start: 0, end: 20 * DAY_MS });
    const out = zoomAround(range, 7 * DAY_MS, 1.7);
    expect((7 * DAY_MS - out.start) / (out.end - out.start)).toBeCloseTo(0.7, 10);
  });
  it('zooms an empty range around its middle', () => {
    expect(zoomAround({ start: 5 * DAY_MS, end: 5 * DAY_MS }, 5 * DAY_MS, 1)).toEqual({ start: 5 * DAY_MS - ZOOM_MIN_MS / 2, end: 5 * DAY_MS + ZOOM_MIN_MS / 2 });
  });
  it('holds the length between an hour and 92 days', () => {
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

describe('gestureKind', () => {
  it('is a tap for one pointer, or none (a synthetic click), that moved at most the slop', () => {
    expect(gestureKind({ pointers: 1, moved: 0 })).toBe('tap');
    expect(gestureKind({ pointers: 0, moved: 0 })).toBe('tap');
    expect(gestureKind({ pointers: 1, moved: TAP_SLOP_PX })).toBe('tap');
  });
  it('is a one-finger drag once the pointer moved past the slop, on either axis', () => {
    expect(gestureKind({ pointers: 1, moved: TAP_SLOP_PX + 1 })).toBe('drag');
  });
  it('is a pinch once a second pointer took part', () => {
    expect(gestureKind({ pointers: 2, moved: 0 })).toBe('pinch');
    expect(gestureKind({ pointers: 3, moved: 80 })).toBe('pinch');
  });
});

describe('gestureTracker', () => {
  function track() {
    const listeners = new Map<string, (e: unknown) => void>();
    const container = { addEventListener: (type: string, fn: (e: unknown) => void) => listeners.set(type, fn), removeEventListener: (type: string) => listeners.delete(type) };
    const tracker = gestureTracker(container as unknown as HTMLElement);
    // The first finger of a gesture is the primary pointer, as in a browser.
    const pointer = (type: string, pointerId: number, clientX = 0, clientY = 0, isPrimary = pointerId === 1) =>
      listeners.get(type)?.({ pointerId, clientX, clientY, isPrimary });
    return { tracker, listeners, pointer };
  }

  it('sees a tap that stays within the slop as a tap', () => {
    const { tracker, pointer } = track();
    expect(tracker.kind()).toBe('tap'); // nothing seen yet: a synthetic click
    pointer('pointerdown', 1, 50, 50);
    pointer('pointermove', 1, 54, 47);
    pointer('pointerup', 1, 54, 47);
    expect(tracker.kind()).toBe('tap');
  });

  it('sees a one-finger drag as a drag on the horizontal axis, on the vertical axis, and when it ends where it began', () => {
    const { tracker, pointer } = track();
    pointer('pointerdown', 1, 0, 0);
    pointer('pointermove', 1, 60, 3);
    pointer('pointerup', 1, 60, 3);
    expect(tracker.kind()).toBe('drag');

    pointer('pointerdown', 1, 0, 0);
    pointer('pointermove', 1, 2, 90);
    pointer('pointerup', 1, 2, 90);
    expect(tracker.kind()).toBe('drag');

    pointer('pointerdown', 1, 0, 0);
    pointer('pointermove', 1, 40, 0);
    pointer('pointermove', 1, 0, 0);
    pointer('pointerup', 1, 0, 0);
    expect(tracker.kind()).toBe('drag');
  });

  it('sees two fingers as a pinch, whether they move or not, and the next gesture starts afresh', () => {
    const { tracker, pointer } = track();
    pointer('pointerdown', 1, 100, 100);
    pointer('pointerdown', 2, 200, 100);
    pointer('pointermove', 1, 60, 100);
    pointer('pointermove', 2, 240, 100);
    pointer('pointerup', 1);
    pointer('pointerup', 2);
    expect(tracker.kind()).toBe('pinch');

    pointer('pointerdown', 1, 0, 0);
    pointer('pointerup', 1, 0, 0);
    expect(tracker.kind()).toBe('tap');
  });

  it('ignores a pointer that is not down, and moves of one that has left', () => {
    const { tracker, pointer } = track();
    pointer('pointermove', 5, 900, 900);
    pointer('pointerdown', 1, 0, 0);
    pointer('pointerup', 1, 0, 0);
    pointer('pointermove', 1, 500, 0);
    expect(tracker.kind()).toBe('tap');
  });

  it('starts afresh at a primary pointerdown when the end of an earlier pointer never arrived', () => {
    const { tracker, pointer } = track();
    pointer('pointerdown', 1, 0, 0); // a swipe whose pointerup and pointercancel were lost (seen on Chrome for Android)
    pointer('pointermove', 1, 200, 0);
    expect(tracker.kind()).toBe('drag');
    pointer('pointerdown', 7, 10, 10, true); // the next touch is primary: no other finger can be down
    pointer('pointerup', 7, 10, 10);
    expect(tracker.kind()).toBe('tap');
  });

  it('ends a pointer on lostpointercapture, and counts a later non-primary pointer on its own', () => {
    const { tracker, pointer } = track();
    pointer('pointerdown', 3, 0, 0, false);
    pointer('pointermove', 3, 300, 0);
    pointer('lostpointercapture', 3); // with no pointerup after it
    pointer('pointerdown', 4, 0, 0, false); // not primary, but nothing else is down: a gesture of its own
    pointer('pointerup', 4);
    expect(tracker.kind()).toBe('tap');
  });

  it('ends a pointer on pointercancel and listens no more after stop', () => {
    const { tracker, listeners, pointer } = track();
    pointer('pointerdown', 1, 0, 0);
    pointer('pointerdown', 2, 50, 0);
    pointer('pointercancel', 1);
    pointer('pointercancel', 2);
    pointer('pointerdown', 3, 0, 0, false);
    expect(tracker.kind()).toBe('tap'); // 3 started a fresh gesture: the cancelled pair no longer counts
    tracker.stop();
    expect(listeners.size).toBe(0);
  });

  it('does nothing on a container that cannot listen', () => {
    const tracker = gestureTracker({});
    expect(tracker.kind()).toBe('tap');
    expect(() => tracker.stop()).not.toThrow();
  });
});

describe('the previewed task in the fragment', () => {
  it('reads a ref, and nothing for a missing, blank or overlong one', () => {
    expect(taskFromQuery('tag=x&task=board-3')).toBe('board-3');
    expect(taskFromQuery('task=%20T-1%20')).toBe('T-1');
    expect(taskFromQuery('tag=x')).toBeUndefined();
    expect(taskFromQuery('task=')).toBeUndefined();
    expect(taskFromQuery(`task=${'x'.repeat(129)}`)).toBeUndefined();
  });
  it('is stripped from the filter query and written after the window', () => {
    expect(withoutWindow('tag=x&task=T-1')).toBe('tag=x');
    const w = { from: new Date('2026-03-01T00:00:00Z'), to: new Date('2026-03-15T00:00:00Z') };
    expect(timelineViewHash(DEFAULT_TIMELINE_FILTER, undefined, 'T-1')).toBe('#/timeline?task=T-1');
    expect(timelineViewHash(DEFAULT_TIMELINE_FILTER, w, 'T-1')).toMatch(/^#\/timeline\?from=.*&to=.*&task=T-1$/);
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

describe('nearestBar', () => {
  const bar = (task: string, group: string | undefined, left: number, right: number) => ({ task, group, left, right, top: 10, bottom: 46 });

  it('finds the nearest bar of the same row within the reach, the first drawn on a tie', () => {
    const bars = [bar('A', 't:1', 100, 102), bar('B', 't:1', 130, 140), bar('C', 't:2', 105, 200), bar('D', 't:1', 90, 98)];
    expect(nearestBar(bars, 110, 30, 't:1')?.task).toBe('A'); // 8 px from A, 20 from B
    expect(nearestBar(bars, 101, 30, 't:1')?.task).toBe('A'); // inside A
    expect(nearestBar(bars, 116, 50, 't:1')?.task).toBe('A'); // 14 px right, 4 px below: within 16
    expect(nearestBar(bars, 99, 30, 't:1')?.task).toBe('A'); // 1 px from A and from D: the first drawn wins
    expect(nearestBar(bars, 160, 30, 't:1')).toBeUndefined(); // 20 px from B
    expect(nearestBar(bars, 150, 30, 't:2')?.task).toBe('C');
    expect(nearestBar(bars, 110, 30, 't:3')).toBeUndefined();
    expect(nearestBar(bars, 110, 30, 't:1', 5)).toBeUndefined();
    expect(TAP_REACH_PX).toBe(16);
  });
});

describe('pressOpens', () => {
  const el = {} as Element;
  const at = (key: string, e: Element | null = el, top = 10): Press => ({ key, el: e, top });

  it('opens the task of a click that ends the press it began on, and one with no press seen', () => {
    expect(pressOpens(at('t:A1'), at('t:A1'))).toBe('A1');
    expect(pressOpens(at('t:A1'), at('t:A1', el, 11))).toBe('A1'); // 1 px of layout shift is none
    expect(pressOpens(undefined, at('t:A1'))).toBe('A1'); // a synthetic click
  });

  it('opens nothing when the press began elsewhere, on another element, or the element moved', () => {
    expect(pressOpens(at('t:A1'), at('t:A2'))).toBeUndefined();
    expect(pressOpens(at('g:i:alpha'), at('t:A1'))).toBeUndefined();
    expect(pressOpens(at(''), at('t:A1'))).toBeUndefined();
    expect(pressOpens(at('t:A1'), at('t:A1', {} as Element))).toBeUndefined();
    expect(pressOpens(at('t:A1'), at('t:A1', el, 12))).toBeUndefined();
  });

  it('opens nothing on an initiative row or on nothing', () => {
    expect(pressOpens(at('g:i:alpha'), at('g:i:alpha'))).toBeUndefined();
    expect(pressOpens(undefined, at(''))).toBeUndefined();
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
    get() {
      return this.rows;
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
  /** What vis-timeline finds under an element a fake hit test returns (a release point carries no `props` of its own). */
  const hitProps = new Map<unknown, ChartClick>();
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
      itemSet?: { groups: Record<string, unknown>; toggleGroupShowNested(group: unknown): void };
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
      /** A test's fake event carries what vis-timeline would find under it. */
      getEventProperties(event: { props?: ChartClick; target?: unknown }) {
        return event.props ?? hitProps.get(event.target) ?? { what: null };
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

  /** A chart container; with `hitTest`, it sits in a document whose hit test answers `under` (set by `lift`). */
  function fakeContainer(bars: unknown[] = [], hitTest = false, labels: unknown[] = []) {
    const listeners = new Map<string, (e: unknown) => void>();
    // Several listeners may hold one event type (the chart's and the rows' own): `listeners` answers with all of them.
    const held = new Map<string, ((e: unknown) => void)[]>();
    const centre = { getBoundingClientRect: () => ({ left: 100, width: 1000 }) };
    const left = { scrollTop: 0, scrollHeight: 0, clientHeight: 400 };
    let under: unknown = null;
    const hits: [number, number][] = [];
    const ownerDocument = {
      elementFromPoint: (x: number, y: number) => {
        hits.push([x, y]);
        return under;
      },
    };
    const container = {
      ...(hitTest ? { ownerDocument } : {}),
      addEventListener: (type: string, fn: (e: unknown) => void) => {
        const all = [...(held.get(type) ?? []), fn];
        held.set(type, all);
        listeners.set(type, (e) => all.forEach((one) => one(e)));
      },
      removeEventListener: (type: string, fn?: (e: unknown) => void) => {
        const rest = (held.get(type) ?? []).filter((one) => one !== fn);
        held.set(type, rest);
        if (rest.length === 0) listeners.delete(type);
        else listeners.set(type, (e) => rest.forEach((one) => one(e)));
      },
      querySelector: (sel: string) => (sel === '.vis-panel.vis-center' ? centre : sel === '.vis-panel.vis-left' ? left : null),
      querySelectorAll: (sel: string) => (sel.startsWith('.vis-item[data-task]') ? bars : sel === '.vis-label' ? labels : []),
    };
    const wheel = (over: object) => {
      const e = { deltaX: 0, deltaY: 0, deltaMode: 0, shiftKey: false, ctrlKey: false, clientX: 100, preventDefault: vi.fn(), stopPropagation: vi.fn(), ...over };
      listeners.get('wheel')?.(e);
      return e;
    };
    const pointer = (type: string, pointerId: number, clientX: number, clientY = 0, over: object = {}) =>
      listeners.get(type)?.({ pointerId, clientX, clientY, isPrimary: pointerId === 1, ...over });
    /** A DOM click on the chart that vis-timeline reads as `props`, on `target`. */
    const click = (props: ChartClick, over: object = {}) => listeners.get('click')?.({ props, target: null, clientX: 0, clientY: 0, ...over });
    /** Pointer `pointerId` comes up at (x, y) over `target`, which vis-timeline reads as `props`. */
    const lift = (pointerId: number, x: number, y: number, target: unknown, props: ChartClick) => {
      under = target;
      if (target !== null) hitProps.set(target, props);
      pointer('pointerup', pointerId, x, y, { target: 'the element it went down on' });
    };
    /** A key goes down on `target`; `preventDefault` says whether the chart took it. */
    const key = (target: unknown, over: object = {}) => {
      const e = { key: 'Enter', target, ctrlKey: false, altKey: false, metaKey: false, shiftKey: false, repeat: false, preventDefault: vi.fn(), ...over };
      listeners.get('keydown')?.(e);
      return e;
    };
    return { container: container as unknown as HTMLElement, listeners, wheel, pointer, click, lift, hits, left, key };
  }

  it('reports the rows scrolling and puts a saved scroll back once the rows are tall enough', () => {
    vi.useFakeTimers();
    try {
      const { lib, made } = fakeLib();
      const c = fakeContainer();
      const rowsSeen: number[] = [];
      const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), { ...hooks().h, onRowScroll: (px) => rowsSeen.push(px) });
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

  it('reports nothing while a restore is pending or without a hook, and stops a report still waiting on destroy', () => {
    vi.useFakeTimers();
    try {
      const { lib } = fakeLib();
      const c = fakeContainer();
      const rowsSeen: number[] = [];
      const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), { ...hooks().h, onRowScroll: (px) => rowsSeen.push(px) });
      chart.scrollRows(500);
      c.listeners.get('scroll')?.({});
      vi.advanceTimersByTime(150);
      expect(rowsSeen).toEqual([]); // the rows are still waiting for the restore: their scrollTop is not the user's

      c.listeners.get('scroll')?.({});
      chart.destroy(); // a report still waiting is dropped
      vi.advanceTimersByTime(150);
      expect(rowsSeen).toEqual([]);

      const bare = fakeContainer();
      mountChart(lib, bare.container, model(['A1']), initialWindow(NOW), hooks().h);
      bare.listeners.get('scroll')?.({});
      vi.advanceTimersByTime(150); // no onRowScroll hook: nothing to call

      const empty = { addEventListener: () => undefined, removeEventListener: () => undefined, querySelector: () => null } as unknown as HTMLElement;
      const none = mountChart(lib, empty, model(['A1']), initialWindow(NOW), hooks().h);
      none.scrollRows(100);
      none.destroy();
    } finally {
      vi.useRealTimers();
    }
  });

  it('holds a saved rows scroll as far as the rows go, and keeps waiting while they are shorter than it', () => {
    const { lib, made } = fakeLib();
    const c = fakeContainer();
    const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), hooks().h);
    chart.scrollRows(900);
    c.left.scrollHeight = 700; // 300 px of room
    made[0]!.emit('changed');
    expect(c.left.scrollTop).toBe(300);
    c.left.scrollHeight = 1500; // now tall enough
    made[0]!.emit('changed');
    expect(c.left.scrollTop).toBe(900);
    c.left.scrollTop = 10;
    made[0]!.emit('changed'); // restored: a later redraw leaves the user's own scroll alone
    expect(c.left.scrollTop).toBe(10);
    chart.scrollRows(0); // nothing to restore
    made[0]!.emit('changed');
    expect(c.left.scrollTop).toBe(10);
  });

  /** A mounted chart whose rows are asked to restore `px`; the rows draw `rowsHeight` tall. */
  function restoring(px: number, rowsHeight = 0) {
    const { lib, made } = fakeLib();
    const c = fakeContainer();
    const rowsSeen: number[] = [];
    const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), { ...hooks().h, onRowScroll: (n) => rowsSeen.push(n) });
    c.left.scrollHeight = rowsHeight;
    chart.scrollRows(px);
    const redraw = (height: number) => {
      c.left.scrollHeight = height;
      made[0]!.emit('changed');
    };
    /** The user leaves the rows at `top`: a scroll event that the report follows. */
    const userScrolls = (top: number) => {
      c.left.scrollTop = top;
      c.listeners.get('scroll')?.({});
      vi.advanceTimersByTime(150);
    };
    return { c, chart, rowsSeen, redraw, userScrolls };
  }

  describe.each([
    ['a vertical wheel', 'wheel', { deltaY: 40 }],
    ['a touch', 'touchstart', {}],
    ['a key', 'keydown', { key: 'ArrowDown' }],
    ['a press on the scrollbar', 'mousedown', {}],
  ] as const)('the rows restore, then %s', (_name, type, event) => {
    it('cancels it before the rows are tall enough: a later redraw does not put the stale offset back', () => {
      vi.useFakeTimers();
      try {
        const r = restoring(900);
        r.redraw(500); // 100 px of room: held as far as it goes
        expect(r.c.left.scrollTop).toBe(100);
        r.c.listeners.get(type)?.(event);
        r.c.left.scrollTop = 40; // where the user leaves them
        r.redraw(3000);
        expect(r.c.left.scrollTop).toBe(40);
        r.userScrolls(60);
        expect(r.rowsSeen).toEqual([60]); // reported again, as the user leaves it
        vi.advanceTimersByTime(10_000); // the deadline of the cancelled restore does nothing
        expect(r.c.left.scrollTop).toBe(60);
      } finally {
        vi.useRealTimers();
      }
    });
  });

  describe('a rows restore that gives up', () => {
    it('settles as far as the rows allow after four seconds and leaves every later redraw alone', () => {
      vi.useFakeTimers();
      try {
        const r = restoring(900, 700); // 300 px of room
        vi.advanceTimersByTime(RESTORE_WAIT_MS - 1);
        r.redraw(700);
        expect(r.c.left.scrollTop).toBe(300); // still holding
        r.c.left.scrollTop = 0;
        vi.advanceTimersByTime(1); // the deadline: as far as the rows go
        expect(r.c.left.scrollTop).toBe(300);
        r.userScrolls(120);
        r.redraw(5000); // tall enough now, but too late
        expect(r.c.left.scrollTop).toBe(120);
        expect(r.rowsSeen).toEqual([120]);
      } finally {
        vi.useRealTimers();
      }
    });

    it('gives up with no redraw at all and reports what the rows end at', () => {
      vi.useFakeTimers();
      try {
        const r = restoring(900, 700);
        r.c.left.scrollTop = 0;
        r.c.listeners.get('scroll')?.({});
        vi.advanceTimersByTime(150);
        expect(r.rowsSeen).toEqual([]); // pending: the position is not the user's yet
        vi.advanceTimersByTime(RESTORE_WAIT_MS);
        expect(r.c.left.scrollTop).toBe(300);
        r.userScrolls(300); // the scroll the settling caused
        expect(r.rowsSeen).toEqual([300]);
      } finally {
        vi.useRealTimers();
      }
    });

    it('moves nothing when the rows never got any room, and a redraw after it stays where it is', () => {
      vi.useFakeTimers();
      try {
        const r = restoring(900, 0);
        vi.advanceTimersByTime(RESTORE_WAIT_MS);
        expect(r.c.left.scrollTop).toBe(0);
        r.redraw(2000);
        expect(r.c.left.scrollTop).toBe(0);
      } finally {
        vi.useRealTimers();
      }
    });
  });

  it('keeps restoring through a sideways wheel, which pans the time axis rather than scrolling the rows', () => {
    vi.useFakeTimers();
    try {
      const r = restoring(900);
      r.c.listeners.get('wheel')?.({ deltaX: 80, deltaY: 0, preventDefault: vi.fn(), stopPropagation: vi.fn(), clientX: 100 });
      r.redraw(2000);
      expect(r.c.left.scrollTop).toBe(900);
    } finally {
      vi.useRealTimers();
    }
  });

  it('starts a new four-second wait for a second restore and drops the first one', () => {
    vi.useFakeTimers();
    try {
      const r = restoring(900);
      vi.advanceTimersByTime(3000);
      r.chart.scrollRows(500);
      vi.advanceTimersByTime(3000); // the first deadline has passed; the second has not
      r.redraw(2000);
      expect(r.c.left.scrollTop).toBe(500);
      r.userScrolls(10);
      r.chart.scrollRows(0); // nothing to restore also drops a pending one
      r.redraw(4000);
      expect(r.c.left.scrollTop).toBe(10);
    } finally {
      vi.useRealTimers();
    }
  });

  it('stops the wait when the chart goes away', () => {
    vi.useFakeTimers();
    try {
      const r = restoring(900, 700);
      r.chart.destroy();
      expect(r.c.listeners.size).toBe(0);
      r.c.left.scrollTop = 7;
      vi.advanceTimersByTime(RESTORE_WAIT_MS * 2);
      expect(r.c.left.scrollTop).toBe(7);
    } finally {
      vi.useRealTimers();
    }
  });

  it('pans on a sideways wheel, leaving a vertical wheel to vis-timeline', () => {
    const { lib, made } = fakeLib();
    const c = fakeContainer();
    mountChart(lib, c.container, model(['A1']), initialWindow(NOW), hooks().h);
    const t = made[0]!; // window 0 .. 2 days

    const down = c.wheel({ deltaY: 100 });
    expect(down.preventDefault).not.toHaveBeenCalled();
    expect(t.setWindow).not.toHaveBeenCalled();

    const side = c.wheel({ deltaX: 100 });
    expect(side.preventDefault).toHaveBeenCalled();
    expect(side.stopPropagation).toHaveBeenCalled();
    const [panStart, panEnd, options] = t.setWindow.mock.lastCall as [number, number, object];
    expect(panStart).toBeCloseTo(0.2 * DAY_MS);
    expect(panEnd).toBeCloseTo(2.2 * DAY_MS);
    expect(options).toEqual({ animation: false });
  });

  it('zooms around the pointer on Shift+wheel and on Ctrl+wheel, in one direction and out the other, within the bounds', () => {
    const { lib, made } = fakeLib();
    const c = fakeContainer();
    mountChart(lib, c.container, model(['A1']), initialWindow(NOW), hooks().h);
    const t = made[0]!; // window 0 .. 2 days, drawn 1000 px wide from x = 100
    const span = 2 * DAY_MS;

    for (const key of ['shiftKey', 'ctrlKey']) {
      t.setWindow.mockClear();
      const left = c.wheel({ deltaY: -100, [key]: true, clientX: 100 }); // the pointer at the left edge: it stays put
      expect(left.stopPropagation).toHaveBeenCalled();
      const [start, end] = t.setWindow.mock.lastCall as [number, number];
      expect(start).toBe(0);
      expect(end).toBeCloseTo(span * Math.exp(-100 * WHEEL_ZOOM_RATE)); // in

      c.wheel({ deltaY: 100, [key]: true, clientX: 600 }); // out, the pointer in the middle
      const [outStart, outEnd] = t.setWindow.mock.lastCall as [number, number];
      expect(outEnd - outStart).toBeCloseTo(span * Math.exp(100 * WHEEL_ZOOM_RATE));
      expect((DAY_MS - outStart) / (outEnd - outStart)).toBeCloseTo(0.5, 10); // the instant under the pointer stays under it
    }

    t.window = { start: new Date(0), end: new Date(ZOOM_MAX_MS) };
    c.wheel({ deltaY: 1000, shiftKey: true });
    const [maxStart, maxEnd] = t.setWindow.mock.lastCall as [number, number];
    expect(maxEnd - maxStart).toBe(ZOOM_MAX_MS);
    t.window = { start: new Date(0), end: new Date(ZOOM_MIN_MS) };
    c.wheel({ deltaY: -1000, ctrlKey: true });
    const [minStart, minEnd] = t.setWindow.mock.lastCall as [number, number];
    expect(minEnd - minStart).toBe(ZOOM_MIN_MS);
  });

  it('leaves the wheel to vis-timeline while the chart has no width', () => {
    const { lib, made } = fakeLib();
    const listeners = new Map<string, (e: unknown) => void>();
    const container = {
      addEventListener: (type: string, fn: (e: unknown) => void) => listeners.set(type, fn),
      removeEventListener: (type: string) => listeners.delete(type),
      querySelector: () => ({ getBoundingClientRect: () => ({ left: 0, width: 0 }) }),
    };
    mountChart(lib, container as unknown as HTMLElement, model(['A1']), initialWindow(NOW), hooks().h);
    const e = { deltaX: 0, deltaY: 100, deltaMode: 0, shiftKey: true, ctrlKey: false, clientX: 0, preventDefault: vi.fn(), stopPropagation: vi.fn() };
    listeners.get('wheel')?.(e);
    expect(e.preventDefault).not.toHaveBeenCalled();
    expect(made[0]!.setWindow).not.toHaveBeenCalled();
  });

  it('opens a task for a tap but not for a drag or a pinch that ended over its bar, and stops listening on destroy', () => {
    const { lib } = fakeLib();
    const h = hooks();
    const c = fakeContainer();
    const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), h.h);
    const props = { what: 'item', item: 'act:A1' };
    const click = () => c.click(props);
    const pointer = (type: string, id: number, x: number) => c.pointer(type, id, x, 0, { props });

    pointer('pointerdown', 1, 0);
    pointer('pointerup', 1, 0);
    click();
    expect(h.opened).toEqual(['A1']);

    pointer('pointerdown', 1, 0);
    pointer('pointermove', 1, 80);
    pointer('pointerup', 1, 80);
    click(); // a drag
    pointer('pointerdown', 1, 0);
    pointer('pointerdown', 2, 100);
    pointer('pointerup', 1, 0);
    pointer('pointerup', 2, 100);
    click(); // a pinch
    expect(h.opened).toEqual(['A1']);

    pointer('pointerdown', 1, 0); // a stuck gesture does not stop later taps
    pointer('pointerdown', 1, 0);
    pointer('pointerup', 1, 0);
    click();
    expect(h.opened).toEqual(['A1', 'A1']);

    chart.destroy();
    expect(c.listeners.size).toBe(0);
  });

  it('sets the chart height and reports the shown window after every redraw', () => {
    const { lib, made } = fakeLib();
    const shown: TimeWindow[] = [];
    const chart = mountChart(lib, {} as HTMLElement, model(['A1']), initialWindow(NOW), { ...hooks().h, onWindow: (r) => shown.push(r) }, 640);
    expect((made[0]!.options as { maxHeight: number }).maxHeight).toBe(640);
    chart.setHeight(500);
    expect(made[0]!.setOptions).toHaveBeenCalledWith({ maxHeight: 500 });
    made[0]!.emit('rangechange');
    expect(shown).toEqual([{ from: new Date(0), to: new Date(2 * DAY_MS) }]);
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

  it('opens the task of a DOM click on a bar or a task row, from the model last shown, and ignores vis-timeline\'s own click', () => {
    const { lib, made } = fakeLib();
    const h = hooks();
    const c = fakeContainer();
    const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), h.h);
    c.click({ what: 'item', item: 'act:A1' });
    c.click({ what: 'group-label', group: 't:A7' });
    c.click({ what: 'group-label', group: 'i:alpha' });
    c.click({ what: 'background' });
    expect(h.opened).toEqual(['A1', 'A7']);
    made[0]!.emit('click', { what: 'item', item: 'act:A1' }); // vis-timeline's click (its tap recogniser) is not heard
    expect(h.opened).toEqual(['A1', 'A7']);
    chart.show(model(['B1']));
    c.click({ what: 'item', item: 'act:A1' }); // no longer drawn
    c.click({ what: 'item', item: 'act:B1' });
    expect(h.opened).toEqual(['A1', 'A7', 'B1']);
  });

  it('focuses the bar a click landed on before it opens the task, and lets each drawn bar take the focus from script, once', () => {
    const { lib, made } = fakeLib();
    const order: string[] = [];
    const h = hooks();
    const hk = { ...h.h, open: (ref: string) => order.push(`open ${ref}`) };
    const bar = {
      focus: vi.fn(() => order.push('focus')),
      tabIndex: 0,
      dataset: { task: 'A1' },
      textContent: ' Task A ',
      attrs: {} as Record<string, string>,
      setAttribute(k: string, v: string) {
        this.attrs[k] = v;
      },
      getBoundingClientRect: () => ({ left: 0, top: 0, right: 10, bottom: 10 }),
      closest() {
        return this;
      },
    };
    const c = fakeContainer([bar]);
    mountChart(lib, c.container, model(['A1']), initialWindow(NOW), hk);
    c.click({ what: 'item', item: 'act:A1' }, { target: { closest: () => bar } });
    expect(order).toEqual(['focus', 'open A1']);
    expect(bar.focus).toHaveBeenCalledWith({ preventScroll: true });
    c.click({ what: 'item', item: 'act:A1' }, { target: { closest: () => null } });
    expect(order).toEqual(['focus', 'open A1', 'open A1']);
    made[0]!.emit('changed');
    expect(bar.tabIndex).toBe(-1);
    expect(bar.attrs).toEqual({}); // no role: a bar is not a standalone control
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

  describe('what the chart displays, reported before the deferred reports (R7, R8)', () => {
    const group = (slug: string, showNested = true) => ({ id: `i:${slug}`, ref: slug, order: 0, nestedGroups: [`t:${slug}1`], showNested }) as unknown as TimelineModel['groups'][number];
    const two = (): TimelineModel => ({ groups: [group('a'), group('b')], items: [], dependencies: [] });

    it('flush reports the scroll of the rows now (inside its debounce, or before its scroll event fired), and destroy does the same', () => {
      vi.useFakeTimers();
      try {
        const { lib } = fakeLib();
        const c = fakeContainer();
        const seen: number[] = [];
        const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), { ...hooks().h, onRowScroll: (px) => seen.push(px) });
        c.left.scrollTop = 120;
        c.listeners.get('scroll')?.({});
        chart.flush();
        expect(seen).toEqual([120]);
        vi.advanceTimersByTime(500);
        expect(seen).toEqual([120]); // the debounce was cancelled: no second report, later, for another entry
        c.left.scrollTop = 200; // moved, but its scroll event has not fired yet: the rows themselves are what is read
        chart.flush();
        expect(seen).toEqual([120, 200]);

        c.left.scrollTop = 340;
        c.listeners.get('scroll')?.({});
        chart.destroy(); // torn down before the debounce fires
        expect(seen).toEqual([120, 200, 340]);
        vi.advanceTimersByTime(500);
        expect(seen).toEqual([120, 200, 340]);
      } finally {
        vi.useRealTimers();
      }
    });

    it('flush reports a collapse and an expand that vis-timeline has toggled in the data but not yet redrawn', () => {
      const { lib, made } = fakeLib();
      const reported: string[][] = [];
      const chart = mountChart(lib, {} as HTMLElement, two(), initialWindow(NOW), { ...hooks().h, onCollapse: (ids) => reported.push(ids) });
      (made[0]!.groups.rows[1] as { showNested?: boolean }).showNested = false; // the toggle; no `changed` yet
      chart.flush();
      expect(reported).toEqual([['i:b']]);
      (made[0]!.groups.rows[1] as { showNested?: boolean }).showNested = true;
      (made[0]!.groups.rows[0] as { showNested?: boolean }).showNested = false;
      chart.destroy();
      expect(reported).toEqual([['i:b'], ['i:a']]);
      made[0]!.emit('changed'); // the redraw that comes after teardown reports nothing new
      expect(reported).toEqual([['i:b'], ['i:a']]);
    });

    it('reports nothing for rows the page has already dropped (they read 0, which is not where the user left them)', () => {
      const { lib } = fakeLib();
      const c = fakeContainer();
      const seen: number[] = [];
      const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), { ...hooks().h, onRowScroll: (px) => seen.push(px) });
      c.left.scrollTop = 0;
      (c.left as { isConnected?: boolean }).isConnected = false;
      chart.flush();
      chart.destroy();
      expect(seen).toEqual([]);
    });

    it('does not report a restore still under way as the user\'s scroll', () => {
      vi.useFakeTimers();
      try {
        const { lib } = fakeLib();
        const c = fakeContainer();
        const seen: number[] = [];
        const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), { ...hooks().h, onRowScroll: (px) => seen.push(px) });
        chart.scrollRows(800);
        c.listeners.get('scroll')?.({});
        chart.flush();
        expect(seen).toEqual([]);
      } finally {
        vi.useRealTimers();
      }
    });

    it('shows another entry: toggles only the initiatives that differ and puts its rows scroll back', () => {
      const { lib, made } = fakeLib();
      const c = fakeContainer();
      const chart = mountChart(lib, c.container, two(), initialWindow(NOW), { ...hooks().h, collapsed: ['i:a'] });
      const vis = { a: {}, b: {} };
      const toggle = vi.fn((g: unknown, force?: boolean) => {
        const id = g === vis.a ? 'i:a' : 'i:b';
        (made[0]!.groups.rows.find((r) => (r as { id: string }).id === id) as { showNested?: boolean }).showNested = force;
      });
      made[0]!.itemSet = { groups: { 'i:a': vis.a, 'i:b': vis.b }, toggleGroupShowNested: toggle };
      c.left.scrollHeight = 2000;
      c.left.scrollTop = 50;
      chart.showEntry({ collapsed: ['i:b'], rows: 700 });
      expect(toggle.mock.calls).toEqual([
        [vis.a, true],
        [vis.b, false],
      ]);
      expect(c.left.scrollTop).toBe(700);
      toggle.mockClear();
      chart.showEntry({ collapsed: ['i:b'], rows: 700 });
      expect(toggle).not.toHaveBeenCalled(); // already displayed
      chart.showEntry({ collapsed: [], rows: 0 }); // an entry with nothing saved: every initiative open, rows at the top
      expect(toggle.mock.calls).toEqual([[vis.b, true]]);
      expect(c.left.scrollTop).toBe(0);
    });

    it('shows an entry for a chart with no rows panel yet, and never toggles a row that is not an initiative', () => {
      const { lib, made } = fakeLib();
      const empty = { addEventListener: () => undefined, removeEventListener: () => undefined, querySelector: () => null } as unknown as HTMLElement;
      const chart = mountChart(lib, empty, model(['A1']), initialWindow(NOW), hooks().h);
      const toggle = vi.fn();
      made[0]!.itemSet = { groups: { 't:A1': {} }, toggleGroupShowNested: toggle };
      chart.showEntry({ collapsed: ['t:A1'], rows: 0 });
      chart.showEntry({ collapsed: [], rows: 100 });
      expect(toggle).not.toHaveBeenCalled();
      chart.destroy();
    });

    it('shows an entry whose rows are not tall enough yet once they are, and gives up after the bounded wait', () => {
      vi.useFakeTimers();
      try {
        const { lib, made } = fakeLib();
        const c = fakeContainer();
        const chart = mountChart(lib, c.container, two(), initialWindow(NOW), hooks().h);
        chart.showEntry({ collapsed: [], rows: 900 });
        expect(c.left.scrollTop).toBe(0);
        c.left.scrollHeight = 1700; // room 1300
        made[0]!.emit('changed');
        expect(c.left.scrollTop).toBe(900);
        c.left.scrollTop = 0;
        chart.showEntry({ collapsed: [], rows: 5000 });
        c.left.scrollHeight = 900; // room 500: never tall enough
        made[0]!.emit('changed');
        vi.advanceTimersByTime(RESTORE_WAIT_MS + 1);
        expect(c.left.scrollTop).toBe(500);
      } finally {
        vi.useRealTimers();
      }
    });
  });

  describe('collapsed initiatives', () => {
    const group = (slug: string, showNested = true) => ({ id: `i:${slug}`, ref: slug, order: 0, nestedGroups: [`t:${slug}1`], showNested }) as unknown as TimelineModel['groups'][number];
    const two = (): TimelineModel => ({ groups: [group('a'), group('b')], items: [], dependencies: [] });
    const shown = (g: { rows: unknown[] }) => g.rows.map((r) => (r as { showNested?: boolean }).showNested);

    it('opens the initiatives it was told are collapsed collapsed, and the others expanded', () => {
      const { lib, made } = fakeLib();
      mountChart(lib, {} as HTMLElement, two(), initialWindow(NOW), { ...hooks().h, collapsed: ['i:b'] });
      expect(shown(made[0]!.groups)).toEqual([true, false]);
    });

    it('opens every initiative expanded when it is told nothing', () => {
      const { lib, made } = fakeLib();
      mountChart(lib, {} as HTMLElement, two(), initialWindow(NOW), hooks().h);
      expect(shown(made[0]!.groups)).toEqual([true, true]);
    });

    it('reports a collapse and an expand once each, when vis-timeline has redrawn', () => {
      const { lib, made } = fakeLib();
      const reported: string[][] = [];
      mountChart(lib, {} as HTMLElement, two(), initialWindow(NOW), { ...hooks().h, onCollapse: (ids) => reported.push(ids) });
      made[0]!.emit('changed');
      expect(reported).toEqual([]); // nothing moved
      (made[0]!.groups.rows[1] as { showNested?: boolean }).showNested = false; // vis-timeline's own toggle
      made[0]!.emit('changed');
      made[0]!.emit('changed');
      (made[0]!.groups.rows[0] as { showNested?: boolean }).showNested = false;
      made[0]!.emit('changed');
      (made[0]!.groups.rows[1] as { showNested?: boolean }).showNested = true;
      made[0]!.emit('changed');
      expect(reported).toEqual([['i:b'], ['i:a', 'i:b'], ['i:a']]);
    });

    it('does not report a restored collapse as a change, and reports expanding it', () => {
      const { lib, made } = fakeLib();
      const reported: string[][] = [];
      mountChart(lib, {} as HTMLElement, two(), initialWindow(NOW), { ...hooks().h, collapsed: ['i:a'], onCollapse: (ids) => reported.push(ids) });
      made[0]!.emit('changed');
      expect(reported).toEqual([]);
      (made[0]!.groups.rows[0] as { showNested?: boolean }).showNested = true;
      made[0]!.emit('changed');
      expect(reported).toEqual([[]]);
    });

    it('draws an initiative that comes back after a filter dropped it collapsed again', () => {
      const { lib, made } = fakeLib();
      const chart = mountChart(lib, {} as HTMLElement, two(), initialWindow(NOW), { ...hooks().h, collapsed: ['i:b'] });
      chart.show({ groups: [group('a')], items: [], dependencies: [] });
      made[0]!.emit('changed'); // i:b is no longer held: it stays collapsed
      chart.show(two());
      expect(shown(made[0]!.groups)).toEqual([true, false]);
    });
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

  it('moves to a window through vis-timeline, without animation', () => {
    const { lib, made } = fakeLib();
    const chart = mountChart(lib, {} as HTMLElement, model(['A1']), initialWindow(NOW), hooks().h);
    const w = { from: new Date(DAY_MS), to: new Date(3 * DAY_MS) };
    chart.moveTo(w);
    expect(made[0]!.setWindow).toHaveBeenCalledWith(w.from, w.to, { animation: false });
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

  /**
   * The tap rule (R6): a tap's target is taken at pointerdown, and the click opens a task only when it lands on that same
   * element. The fake chart reads a fake event's `props` as what vis-timeline finds under it and its `target` as the element
   * hit, so each case drives pointer events and a click the way a browser delivers them.
   */
  describe('row labels from the keyboard (R9)', () => {
    /** A row label as vis-timeline draws it: with the classes it keeps and the attributes the view adds. */
    function labelEl(classes: string[], top = 0) {
      const attrs = new Map<string, string>();
      const set = new Set(classes);
      const el = {
        tabIndex: -1,
        classList: { contains: (c: string) => set.has(c) },
        hasAttribute: (name: string) => (name === 'tabindex' ? el.tabIndex >= 0 : attrs.has(name)),
        setAttribute: (name: string, value: string) => attrs.set(name, value),
        attrs,
        set,
        closest: (selector: string) => (selector === '.vis-label' ? el : null),
        getBoundingClientRect: () => ({ left: 0, top, width: 80, height: 44 }),
      };
      return el;
    }

    it('puts each label in the Tab order as a button, and gives an initiative its state, read again on every redraw', () => {
      const { lib, made } = fakeLib();
      const group = labelEl(['vis-label', 'vis-nesting-group', 'expanded']);
      const task = labelEl(['vis-label', 'vis-nested-group']);
      const c = fakeContainer([], false, [group, task]);
      mountChart(lib, c.container, model(['A1']), initialWindow(NOW), hooks().h);
      made[0]!.emit('changed');
      expect([group.tabIndex, task.tabIndex]).toEqual([0, 0]);
      expect([group.attrs.get('role'), task.attrs.get('role')]).toEqual(['button', 'button']);
      expect(group.attrs.get('aria-expanded')).toBe('true');
      expect(task.attrs.has('aria-expanded')).toBe(false); // a task row has nothing to expand
      group.set.delete('expanded');
      group.set.add('collapsed');
      made[0]!.emit('changed');
      expect(group.attrs.get('aria-expanded')).toBe('false');
    });

    it('opens the task of a label on Enter or Space, and does nothing for any other key, modifier, repeat or element', () => {
      const { lib } = fakeLib();
      const h = hooks();
      const task = labelEl(['vis-label']);
      const c = fakeContainer([], false, [task]);
      mountChart(lib, c.container, model(['A1']), initialWindow(NOW), h.h);
      hitProps.set(task, { what: 'group-label', group: 't:A1' });
      const enter = c.key(task);
      expect(enter.preventDefault).toHaveBeenCalledTimes(1);
      const space = c.key(task, { key: ' ' });
      expect(space.preventDefault).toHaveBeenCalledTimes(1);
      expect(h.opened).toEqual(['A1', 'A1']);
      for (const over of [{ key: 'a' }, { key: 'Tab' }, { ctrlKey: true }, { altKey: true }, { metaKey: true }, { shiftKey: true }, { repeat: true }]) {
        expect(c.key(task, over).preventDefault).not.toHaveBeenCalled();
      }
      const bar = { closest: () => null };
      expect(c.key(bar).preventDefault).not.toHaveBeenCalled(); // not a label
      const inner = { closest: () => task };
      expect(c.key(inner).preventDefault).not.toHaveBeenCalled(); // a label's child is not the focused label
      expect(h.opened).toEqual(['A1', 'A1']);
    });

    it('collapses or expands an initiative on Enter or Space through vis-timeline, opens nothing, and shows the state at once', () => {
      const { lib, made } = fakeLib();
      const h = hooks();
      const group = labelEl(['vis-label', 'vis-nesting-group', 'expanded']);
      const c = fakeContainer([], false, [group]);
      mountChart(lib, c.container, model(['A1']), initialWindow(NOW), h.h);
      const vis = { id: 'the vis group' };
      const toggle = vi.fn(() => {
        group.set.delete('expanded');
        group.set.add('collapsed');
      });
      made[0]!.itemSet = { groups: { 'i:alpha': vis }, toggleGroupShowNested: toggle };
      hitProps.set(group, { what: 'group-label', group: 'i:alpha' });
      c.key(group, { key: ' ' });
      expect(toggle).toHaveBeenCalledWith(vis);
      expect(group.attrs.get('aria-expanded')).toBe('false');
      expect(h.opened).toEqual([]);
    });

    it('gives the focus back to the label it collapsed when the redraw dropped it, once, and takes it from nowhere else', () => {
      const { lib, made } = fakeLib();
      const group = Object.assign(labelEl(['vis-label', 'vis-nesting-group', 'expanded']), { isConnected: true, focus: vi.fn() });
      const c = fakeContainer([], false, [group]);
      const body = {};
      const doc = { activeElement: body as unknown, body };
      Object.assign(c.container, { ownerDocument: doc });
      mountChart(lib, c.container, model(['A1']), initialWindow(NOW), hooks().h);
      made[0]!.itemSet = { groups: { 'i:alpha': {} }, toggleGroupShowNested: vi.fn() };
      hitProps.set(group, { what: 'group-label', group: 'i:alpha' });
      made[0]!.emit('changed');
      expect(group.focus).not.toHaveBeenCalled(); // nothing was toggled
      c.key(group);
      made[0]!.emit('changed'); // the redraw left the focus on the body
      expect(group.focus).toHaveBeenCalledTimes(1);
      expect(group.focus).toHaveBeenCalledWith({ preventScroll: true });
      made[0]!.emit('changed');
      expect(group.focus).toHaveBeenCalledTimes(1); // only the redraw its own toggle caused
      c.key(group);
      doc.activeElement = {}; // the focus went to another element
      made[0]!.emit('changed');
      doc.activeElement = group;
      c.key(group);
      made[0]!.emit('changed'); // the label still has it
      group.isConnected = false;
      doc.activeElement = body;
      c.key(group);
      made[0]!.emit('changed'); // and a label no longer drawn is left alone
      expect(group.focus).toHaveBeenCalledTimes(1);
    });

    it('does nothing for a label it finds no row for, and survives a chart with no row store', () => {
      const { lib } = fakeLib();
      const h = hooks();
      const lost = labelEl(['vis-label']);
      const group = labelEl(['vis-label', 'vis-nesting-group', 'expanded']);
      const c = fakeContainer([], false, [lost, group]);
      mountChart(lib, c.container, model(['A1']), initialWindow(NOW), h.h);
      expect(c.key(lost).preventDefault).toHaveBeenCalledTimes(1); // the key is taken, nothing is found under it
      hitProps.set(group, { what: 'group-label', group: 'i:alpha' });
      expect(c.key(group).preventDefault).toHaveBeenCalledTimes(1); // vis-timeline offers no itemSet
      expect(h.opened).toEqual([]);
      expect(group.attrs.get('aria-expanded')).toBe('true');
    });

    it('hears no key once the chart is destroyed', () => {
      const { lib } = fakeLib();
      const c = fakeContainer([], false, []);
      const chart = mountChart(lib, c.container, model(['A1']), initialWindow(NOW), hooks().h);
      expect(c.listeners.has('keydown')).toBe(true);
      chart.destroy();
      expect(c.listeners.has('keydown')).toBe(false);
    });
  });

  describe('the tap rule', () => {
    interface Fake {
      closest(selector: string): Fake | null;
      getBoundingClientRect(): { left: number; top: number; right: number; bottom: number };
      dataset?: { id: string; task: string };
      focus?: () => void;
    }
    const barEl = (task: string, left: number, top: number, width = 60): Fake => ({
      dataset: { id: `act:${task}`, task },
      focus: vi.fn(),
      closest(selector) {
        return selector === '.vis-item[data-task]' ? this : null;
      },
      getBoundingClientRect: () => ({ left, top, right: left + width, bottom: top + 36 }),
    });
    const labelEl = (top: number): Fake => ({
      closest(selector) {
        return selector === '.vis-label' ? this : null;
      },
      getBoundingClientRect: () => ({ left: 0, top, right: 80, bottom: top + 44 }),
    });
    const rowEl: Fake = { closest: () => null, getBoundingClientRect: () => ({ left: 0, top: 0, right: 0, bottom: 0 }) };

    function chart() {
      const { lib } = fakeLib();
      const h = hooks();
      const bars = [barEl('A1', 200, 100), barEl('A2', 300, 160), barEl('A3', 200, 220, 2)];
      const c = fakeContainer(bars, true);
      const data: TimelineModel = {
        groups: ['A1', 'A2', 'A3'].map((ref, order) => ({ id: `t:${ref}`, ref, order }) as TimelineModel['groups'][number]),
        items: ['A1', 'A2', 'A3'].map((ref) => ({ id: `act:${ref}`, group: `t:${ref}`, task: ref }) as TimelineModel['items'][number]),
        dependencies: [],
      };
      mountChart(lib, c.container, data, initialWindow(NOW), h.h);
      /** A pointer goes down on `target` where vis-timeline finds `props`. */
      const down = (target: Fake | null, props: ChartClick, x = 0, y = 0) => c.pointer('pointerdown', 1, x, y, { target, props });
      /** The pointer comes up over `target`, where vis-timeline finds `props`. */
      const up = (target: Fake | null, props: ChartClick, x = 0, y = 0) => c.lift(1, x, y, target, props);
      /** The click that ends the press, on `target` where vis-timeline finds `props`. */
      const clickOn = (target: Fake | null, props: ChartClick, x = 0, y = 0) => c.click(props, { target, clientX: x, clientY: y });
      const tap = (target: Fake | null, props: ChartClick, x = 0, y = 0) => {
        down(target, props, x, y);
        up(target, props, x, y);
        clickOn(target, props, x, y);
      };
      return { h, c, bars, down, up, clickOn, tap };
    }
    const onBar = (task: string): ChartClick => ({ what: 'item', item: `act:${task}`, group: `t:${task}` });
    const onLabel = (group: string): ChartClick => ({ what: 'group-label', group });
    const onRow = (group: string): ChartClick => ({ what: 'background', group });

    it('case 1: a press and a release on a task\'s bar, label or row opens that task', () => {
      const { h, bars, tap } = chart();
      tap(bars[0]!, onBar('A1'));
      tap(labelEl(100), onLabel('t:A2'));
      tap(rowEl, onRow('t:A2'), 330, 175); // the empty part of A2's row, over its bar's box
      expect(h.opened).toEqual(['A1', 'A2', 'A2']);
      expect(bars[0]!.focus).toHaveBeenCalledWith({ preventScroll: true });
    });

    it('case 2: a press on one task\'s bar released over another task\'s row opens nothing', () => {
      const { h, c, bars, down, up, clickOn } = chart();
      // A mouse: the click is where the pointer came up.
      down(bars[0]!, onBar('A1'));
      up(rowEl, onRow('t:A2'), 330, 175);
      clickOn(rowEl, onRow('t:A2'), 330, 175);
      down(bars[0]!, onBar('A1'));
      up(labelEl(100), onLabel('t:A2'));
      clickOn(labelEl(100), onLabel('t:A2'));
      down(bars[0]!, onBar('A1'));
      up(bars[1]!, onBar('A2'));
      clickOn(bars[1]!, onBar('A2'));
      expect(h.opened).toEqual([]);

      // A finger: the browser aims the click at where it went down (A1's bar); it came up over A2's row, 8 px lower.
      down(bars[0]!, onBar('A1'), 230, 132);
      up(rowEl, onRow('t:A2'), 230, 140);
      clickOn(bars[0]!, onBar('A1'), 230, 132);
      down(bars[0]!, onBar('A1'), 230, 132);
      up(bars[1]!, onBar('A2'), 230, 140);
      clickOn(bars[0]!, onBar('A1'), 230, 132);
      expect(h.opened).toEqual([]);
      expect(c.hits.at(-1)).toEqual([230, 140]); // the release point is what is hit-tested

      // Lifted on the bar it went down on, the same finger's tap opens it.
      down(bars[0]!, onBar('A1'), 230, 132);
      up(bars[0]!, onBar('A1'), 232, 134);
      clickOn(bars[0]!, onBar('A1'), 230, 132);
      expect(h.opened).toEqual(['A1']);
    });

    it('case 3: a drag past the slop that ends on a bar pans and opens nothing', () => {
      const { h, c, bars, down, clickOn } = chart();
      down(bars[1]!, onBar('A2'), 0, 0);
      c.pointer('pointermove', 1, TAP_SLOP_PX + 1, 0, {});
      c.lift(1, TAP_SLOP_PX + 1, 0, bars[0]!, onBar('A1'));
      clickOn(bars[0]!, onBar('A1'));
      down(bars[0]!, onBar('A1'), 0, 0);
      c.pointer('pointermove', 1, 0, 90, {});
      c.lift(1, 0, 90, bars[0]!, onBar('A1'));
      clickOn(bars[0]!, onBar('A1')); // back on the bar it started on: still a pan
      expect(h.opened).toEqual([]);
    });

    it('case 4: a tap on an initiative label only toggles it, even when the collapse moves another row under the pointer', () => {
      const { h, down, up, clickOn } = chart();
      const initiative = labelEl(50);
      down(initiative, onLabel('i:alpha'));
      up(initiative, onLabel('i:alpha'));
      clickOn(initiative, onLabel('i:alpha')); // vis-timeline toggles the group; the click is still on the initiative
      expect(h.opened).toEqual([]);

      const task = labelEl(50); // the collapse drew a task row's label where the initiative's was
      down(initiative, onLabel('i:alpha'));
      up(initiative, onLabel('i:alpha')); // pointerup comes before vis-timeline's own tap collapses it
      clickOn(task, onLabel('t:A1'));
      expect(h.opened).toEqual([]);

      const same = { ...task, getBoundingClientRect: () => ({ left: 0, top: 200, right: 80, bottom: 244 }) };
      down(same, onLabel('t:A1')); // the same label, but it has moved by the click
      up(same, onLabel('t:A1'));
      clickOn({ ...same, getBoundingClientRect: () => ({ left: 0, top: 100, right: 80, bottom: 144 }) }, onLabel('t:A1'));
      expect(h.opened).toEqual([]);
    });

    it('case 5: a tap on the empty part of a task row opens the nearest bar within reach in that row', () => {
      const { h, tap } = chart();
      tap(rowEl, onRow('t:A3'), 212, 236); // 10 px right of A3's 2 px bar
      expect(h.opened).toEqual(['A3']);
      tap(rowEl, onRow('t:A3'), 202 + TAP_REACH_PX + 2, 236); // out of reach
      tap(rowEl, onRow('t:A1'), 330, 150); // A2's bar is near, but in the next row
      tap(rowEl, onRow('t:A9'), 201, 236); // a row with no bar
      expect(h.opened).toEqual(['A3']);
    });

    it('case 6: after a pointer lost without pointerup or pointercancel the next tap opens', () => {
      const { h, c, bars, down, up, clickOn } = chart();
      down(bars[0]!, onBar('A1'), 0, 0); // a swipe whose end never arrived (seen on Chrome for Android)
      c.pointer('pointermove', 1, 200, 0, {});
      down(bars[1]!, onBar('A2'), 5, 5); // the next touch is primary again
      up(bars[1]!, onBar('A2'), 5, 5);
      clickOn(bars[1]!, onBar('A2'), 5, 5);
      expect(h.opened).toEqual(['A2']);

      c.pointer('pointerdown', 3, 0, 0, { isPrimary: false, target: bars[0]!, props: onBar('A1') });
      c.pointer('lostpointercapture', 3, 0, 0, {});
      c.pointer('pointerdown', 4, 0, 0, { isPrimary: false, target: bars[0]!, props: onBar('A1') });
      c.lift(4, 0, 0, bars[0]!, onBar('A1'));
      clickOn(bars[0]!, onBar('A1'));
      expect(h.opened).toEqual(['A2', 'A1']);

      // A pointerup of a pointer never seen going down is not where the tap came up.
      down(bars[1]!, onBar('A2'), 5, 5);
      up(bars[1]!, onBar('A2'), 5, 5);
      c.lift(9, 330, 175, rowEl, onRow('t:A1'));
      clickOn(bars[1]!, onBar('A2'), 5, 5);
      expect(h.opened).toEqual(['A2', 'A1', 'A2']);
    });
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

describe('the window in the fragment', () => {
  const from = '2026-03-01T00:00:00Z';
  const to = '2026-03-08T06:30:15Z';
  const w = { from: new Date(from), to: new Date(to) };

  it('reads a window with both ends, in order, within the zoom range', () => {
    expect(windowFromQuery(`tag=x&from=${from}&to=${to}`)).toEqual(w);
    expect(windowFromQuery(`from=${from}`)).toBeUndefined();
    expect(windowFromQuery(`to=${to}`)).toBeUndefined();
    expect(windowFromQuery(`from=${to}&to=${from}`)).toBeUndefined();
    expect(windowFromQuery(`from=nope&to=${to}`)).toBeUndefined();
    expect(windowFromQuery(`from=${from}&to=nope`)).toBeUndefined();
    expect(windowFromQuery('from=2020-01-01T00:00:00Z&to=2026-01-01T00:00:00Z')).toBeUndefined();
    expect(windowFromQuery('from=2026-03-01T00:00:00Z&to=2026-03-01T00:01:00Z')).toBeUndefined();
  });

  it('strips from, to and task from the filter query, keeping the filter', () => {
    expect(withoutWindow(`tag=x&from=${from}&to=${to}`)).toBe('tag=x');
    expect(withoutWindow(`from=${from}&to=${to}`)).toBe('');
    expect(withoutWindow('tag=x&task=T-1')).toBe('tag=x');
    expect(withoutWindow('task=T-1&from=2026-03-01T00:00:00Z')).toBe('');
    expect(withoutWindow('tag=x&actor=bob')).toBe('tag=x&actor=bob');
  });

  it('writes the filter, then the window to the second, and reads it back', () => {
    expect(timelineViewHash(DEFAULT_TIMELINE_FILTER)).toBe('#/timeline');
    expect(timelineViewHash(DEFAULT_TIMELINE_FILTER, w)).toBe(`#/timeline?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`);
    const hash = timelineViewHash({ ...DEFAULT_TIMELINE_FILTER, tags: ['x'] }, { from: new Date('2026-03-01T00:00:00.789Z'), to: w.to });
    expect(hash).toMatch(/^#\/timeline\?tag=x&from=2026-03-01T00%3A00%3A00Z&to=/);
    expect(windowFromQuery(hash.split('?')[1]!)).toEqual(w);
  });

  it('opens on the window a fragment names, with the filter it names and none saved over it', () => {
    const KEY = 'board.view.v1.timeline';
    const storage = memory({ [KEY]: JSON.stringify({ v: 1, query: 'tag=old' }) });
    const e = viewEnv(storage);
    expect(openTimeline(e, `#/timeline?from=${from}&to=${to}`)).toEqual({ filter: DEFAULT_TIMELINE_FILTER, restored: false, window: w });
    expect(e.hashes).toEqual([]);
    expect(JSON.parse(storage.data.get(KEY)!).query).toBe('tag=old');
    expect(openTimeline(e, `#/timeline?tag=new&from=${from}&to=${to}`)).toEqual({ filter: { ...DEFAULT_TIMELINE_FILTER, tags: ['new'] }, restored: false, window: w });
    expect(JSON.parse(storage.data.get(KEY)!).query).toBe('tag=new');
  });

  it('never saves the window or the task: the saved query is the filter alone', () => {
    const KEY = 'board.view.v1.timeline';
    const storage = memory();
    openTimeline(viewEnv(storage), `#/timeline?actor=bob&from=${from}&to=${to}&task=T-1`);
    expect(JSON.parse(storage.data.get(KEY)!)).toEqual({ v: 1, query: 'actor=bob' });
    const bare = memory();
    openTimeline(viewEnv(bare), `#/timeline?task=T-1&from=${from}&to=${to}`);
    expect(bare.data.has(KEY)).toBe(false);
  });

  it('ignores a window it cannot use and opens on the default one', () => {
    expect(openTimeline(viewEnv(memory()), `#/timeline?from=${to}&to=${from}`).window).toBeUndefined();
  });
});

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

  it('opens a fragment that names a previewed task with it, and a bare preview restores nothing', () => {
    const storage = memory({ [KEY]: JSON.stringify({ v: 1, query: 'tag=saved' }) });
    const e = viewEnv(storage);
    const from = '2026-03-01T00:00:00Z';
    const to = '2026-03-08T00:00:00Z';
    expect(openTimeline(e, '#/timeline?task=board-3')).toEqual({ filter: DEFAULT_TIMELINE_FILTER, restored: false, task: 'board-3' });
    expect(e.hashes).toEqual([]);
    expect(openTimeline(e, `#/timeline?tag=x&from=${from}&to=${to}&task=board-3`)).toEqual({
      filter: { ...DEFAULT_TIMELINE_FILTER, tags: ['x'] },
      restored: false,
      window: { from: new Date(from), to: new Date(to) },
      task: 'board-3',
    });
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

describe('the collapsed initiatives of an entry', () => {
  const rows = [
    { id: 'i:a', nestedGroups: ['t:1'], showNested: true },
    { id: 'i:b', nestedGroups: ['t:2'], showNested: true },
    { id: 't:1' },
  ];

  it('is saved under one name, sorted and without repeats', () => {
    expect(encodeCollapsed(new Set(['i:b', 'i:a']))).toEqual(['i:a', 'i:b']);
    expect(encodeCollapsed(['i:b', 'i:a', 'i:b'])).toEqual(['i:a', 'i:b']);
    expect(encodeCollapsed([])).toEqual([]);
  });

  it('restores what it saved and nothing else', () => {
    expect(collapsedOf(encodeCollapsed(['i:b', 'i:a']))).toEqual(['i:a', 'i:b']);
    expect(collapsedOf(JSON.parse(JSON.stringify(['i:a'])))).toEqual(['i:a']);
    for (const bad of [undefined, null, 'i:a', 3, { 0: 'i:a' }]) expect(collapsedOf(bad)).toEqual([]);
    expect(collapsedOf(['i:a', 4, '', null, 'i:a', 'i:c'])).toEqual(['i:a', 'i:c']);
    expect(collapsedOf(Array.from({ length: 501 }, (_, i) => `i:${i}`))).toEqual([]);
    expect(collapsedOf(Array.from({ length: 500 }, (_, i) => `i:${i}`))).toHaveLength(500);
  });

  it('draws the named initiatives collapsed and touches no task row', () => {
    const out = withCollapsed(rows, new Set(['i:b', 't:1']));
    expect(out.map((g) => g.showNested)).toEqual([true, false, undefined]);
    expect(rows[1]!.showNested).toBe(true); // the model's own rows are not changed
    expect(withCollapsed(rows, new Set())).toEqual(rows);
  });

  it('follows what vis-timeline holds: an open one leaves the set, a shut one joins it, one not held stays', () => {
    const held = [
      { id: 'i:a', nestedGroups: ['t:1'], showNested: false },
      { id: 'i:b', nestedGroups: ['t:2'], showNested: true },
      { id: 't:1', showNested: false },
    ];
    expect([...collapseAfter(new Set(['i:b', 'i:gone']), held)].sort()).toEqual(['i:a', 'i:gone']);
  });
});
