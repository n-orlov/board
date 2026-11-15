// The Timeline view's data layer and its vis-timeline glue (PRD R6, SPEC 11): `TimelineApi` reads `GET /timeline` for
// the view's filter and window, `TimelineViewStore` holds the mapped model (lib/timeline.ts) the view draws, and
// `mountChart` hands that model to vis-timeline. vis-timeline is bundled with the app (an npm dependency, no CDN); the
// view imports its classes and passes them in as `ChartLib`, so this module stays testable without a DOM.
import { errorText, Fetches } from './fetches';
import { ThreadApi } from './threads';
import { apiQuery, buildTimeline, filterFromQuery, filterToQuery, taskOfGroup, type Timeline, type TimelineFilter, type TimelineItem, type TimelineModel } from './timeline';
import { drawArrows, type ArrowSurface } from './timelineArrows';
import { clearViewQuery, openView, saveViewQuery, type ViewEnv, type ViewStorage } from './viewState';

export const HOUR_MS = 3_600_000;
export const DAY_MS = 24 * HOUR_MS;
/** The zoom range: an hour to a quarter. */
export const ZOOM_MIN_MS = HOUR_MS;
export const ZOOM_MAX_MS = 92 * DAY_MS;
/** The window the view opens on, the server's own default: two weeks back, one week ahead. */
export const DAYS_BEFORE = 14;
export const DAYS_AHEAD = 7;

/** The time range drawn and read. */
export interface TimeWindow {
  from: Date;
  to: Date;
}

/** The window the view opens on around `now`. */
export function initialWindow(now: Date): TimeWindow {
  return { from: new Date(now.getTime() - DAYS_BEFORE * DAY_MS), to: new Date(now.getTime() + DAYS_AHEAD * DAY_MS) };
}

/** A pan or zoom that stays within this fraction of the window already read is not a new window. */
export const SAME_RANGE = 0.01;
/** How long the view waits after the range last moved before it reads the new one. */
export const PAN_DEBOUNCE_MS = 300;

/** Whether `a` and `b` are the same range, give or take `SAME_RANGE` of its length at either end. */
export function sameRange(a: TimeWindow, b: TimeWindow): boolean {
  const slack = (a.to.getTime() - a.from.getTime()) * SAME_RANGE;
  return Math.abs(a.from.getTime() - b.from.getTime()) <= slack && Math.abs(a.to.getTime() - b.to.getTime()) <= slack;
}

export class TimelineApi extends ThreadApi {
  /** The handles of every actor the board has recorded, for the actor filter. */
  async actors(): Promise<string[]> {
    const response = await this.call('/api/v1/actors', 'Listing the actors', { headers: { Accept: 'application/json' } });
    return ((await response.json()) as { items: { handle: string }[] }).items.map((a) => a.handle);
  }

  /** The timeline under the filter in the window. */
  async read(filter: TimelineFilter, window: TimeWindow): Promise<Timeline> {
    const query = apiQuery(filter, window.from, window.to);
    const response = await this.call(`/api/v1/timeline?${query.toString()}`, 'Reading the timeline', {
      headers: { Accept: 'application/json' },
    });
    return (await response.json()) as Timeline;
  }
}

export interface TimelineViewState {
  filter: TimelineFilter;
  window: TimeWindow;
  model: TimelineModel;
  /** Whether the first read has landed (until then the view shows that it is loading). */
  loaded: boolean;
  loading: boolean;
  /** The refusal of the last read, empty when it worked. */
  error: string;
}

/** The timers the store's debounce uses; tests hand in their own. */
export interface Timers {
  set(fn: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

const BROWSER_TIMERS: Timers = { set: (fn, ms) => setTimeout(fn, ms), clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>) };

const EMPTY_MODEL: TimelineModel = { groups: [], items: [], dependencies: [] };

export class TimelineViewStore {
  private readonly api: Pick<TimelineApi, 'read'>;
  private readonly publish: (state: TimelineViewState) => void;
  private readonly now: () => Date;
  private readonly timers: Timers;
  private readonly fetches = new Fetches();
  private panTimer: unknown;
  state: TimelineViewState;

  constructor(
    api: Pick<TimelineApi, 'read'>,
    publish: (state: TimelineViewState) => void,
    filter: TimelineFilter,
    now: () => Date = () => new Date(),
    timers: Timers = BROWSER_TIMERS,
    window?: TimeWindow,
  ) {
    this.api = api;
    this.publish = publish;
    this.now = now;
    this.timers = timers;
    this.state = { filter, window: window ?? initialWindow(now()), model: EMPTY_MODEL, loaded: false, loading: false, error: '' };
  }

  private set(patch: Partial<TimelineViewState>) {
    this.state = { ...this.state, ...patch };
    this.publish(this.state);
  }

  /** Reads the timeline under the current filter and window; a read that lands after a newer one is dropped. */
  async load(): Promise<void> {
    const ticket = this.fetches.begin();
    this.set({ loading: true });
    const { filter, window } = this.state;
    try {
      const answer = await this.api.read(filter, window);
      if (this.fetches.end(ticket)) this.set({ model: buildTimeline(answer, this.now()), loaded: true, error: '' });
    } catch (e) {
      if (this.fetches.end(ticket)) this.set({ error: errorText(e) });
    }
    this.set({ loading: this.fetches.busy });
  }

  /**
   * The chart's visible range moved (a drag, a zoom): once it stops for `PAN_DEBOUNCE_MS` and differs from the window
   * last read, the timeline is read again for it. Moving again inside the delay replaces the pending range.
   */
  panTo(range: TimeWindow): void {
    this.cancelPan();
    if (sameRange(this.state.window, range)) return;
    this.panTimer = this.timers.set(() => {
      this.panTimer = undefined;
      this.set({ window: range });
      void this.load();
    }, PAN_DEBOUNCE_MS);
  }

  /** Drops a pending pan: the view is going away. */
  cancelPan(): void {
    if (this.panTimer !== undefined) this.timers.clear(this.panTimer);
    this.panTimer = undefined;
  }

  /** Applies a new filter and reads again. */
  setFilter(filter: TimelineFilter): Promise<void> {
    this.set({ filter });
    return this.load();
  }
}

// --- zoom ---

/** One button press scales the visible range by this factor (in) or its inverse (out). */
export const ZOOM_FACTOR = 0.5;
/** A range within this fraction of a zoom limit counts as at it (vis-timeline clamps with rounding). */
const LIMIT_SLACK = 1.02;

/** The length of the visible range in milliseconds. */
export function spanOf(range: { start: Date; end: Date }): number {
  return range.end.getTime() - range.start.getTime();
}

export function canZoomIn(spanMs: number): boolean {
  return spanMs > ZOOM_MIN_MS * LIMIT_SLACK;
}

export function canZoomOut(spanMs: number): boolean {
  return spanMs < ZOOM_MAX_MS / LIMIT_SLACK;
}

/** A visible range as words: `6 hours`, `3 days`, `2 weeks`, `3 months` (the nearest whole unit). */
export function spanLabel(spanMs: number): string {
  const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? '' : 's'}`;
  if (spanMs < DAY_MS) return plural(Math.max(1, Math.round(spanMs / HOUR_MS)), 'hour');
  const days = spanMs / DAY_MS;
  if (days < 14) return plural(Math.round(days), 'day');
  if (days < 60) return plural(Math.round(days / 7), 'week');
  return plural(Math.round(days / 30), 'month');
}

// --- vis-timeline ---

/** The slice of vis-data's DataSet the chart uses. */
export interface ChartData<T> {
  getIds(): (string | number)[];
  remove(ids: (string | number)[]): unknown;
  /** Adds the rows it does not hold and merges the fields of the ones it does. */
  update(rows: T[]): unknown;
}

/** What vis-timeline tells a `click` listener: what was hit, and the item or group under the pointer. */
export interface ChartClick {
  what: string | null;
  item?: string | number | null;
  group?: string | number | null;
}

/** The slice of vis-timeline's Timeline the chart uses. */
export interface ChartTimeline {
  destroy(): void;
  on(event: 'click', listener: (props: ChartClick) => void): void;
  on(event: 'changed' | 'rangechange' | 'rangechanged', listener: () => void): void;
  getWindow(): { start: Date; end: Date };
  setWindow(start: Date | number, end: Date | number, options: { animation: boolean }): void;
  setOptions(options: Partial<ChartOptions>): void;
  redraw(): void;
  zoomIn(percentage: number, options: { animation: boolean }): void;
  zoomOut(percentage: number, options: { animation: boolean }): void;
}

/** vis-timeline's classes, passed in by the view that imports them. */
export interface ChartLib {
  DataSet: new <T>(rows: T[]) => ChartData<T>;
  Timeline: new (container: HTMLElement, items: ChartData<unknown>, groups: ChartData<unknown>, options: ChartOptions) => ChartTimeline;
}

export interface ChartOptions {
  start: Date;
  end: Date;
  zoomMin: number;
  zoomMax: number;
  orientation: 'top';
  /** No stacking: every item sits at the top of its row and its lane's CSS places it, so rows keep one height. */
  stack: boolean;
  margin: { item: { horizontal: number; vertical: number } };
  groupOrder: 'order';
  selectable: boolean;
  /** Rows scroll inside the chart (so the time axis stays in view), by wheel, drag and the side scrollbar. */
  verticalScroll: boolean;
  /** The chart's tallest; past it the rows scroll. */
  maxHeight: number;
  /** vis-timeline's own wheel zoom needs this key held: the view's wheel handler takes Shift first, so it never does. */
  zoomKey: 'shiftKey';
}

/** The chart's height when the view does not give one. */
export const DEFAULT_CHART_HEIGHT = 480;
/** The chart is never squeezed below this, however little room the screen leaves under the filters. */
export const MIN_CHART_HEIGHT = 300;

/**
 * The vis-timeline options of the view for a window. Navigation (PRD R1): the wheel and a one-finger drag pan on both
 * axes, Shift+wheel, Ctrl+wheel and a two-finger pinch zoom. vis-timeline itself does the drag, the pinch and the
 * vertical wheel (it scrolls the rows); `listenWheel` does the sideways wheel and the wheel zooms.
 */
export function chartOptions(window: TimeWindow, maxHeight: number = DEFAULT_CHART_HEIGHT): ChartOptions {
  return {
    start: window.from,
    end: window.to,
    zoomMin: ZOOM_MIN_MS,
    zoomMax: ZOOM_MAX_MS,
    orientation: 'top',
    stack: false,
    margin: { item: { horizontal: 10, vertical: 0 } },
    groupOrder: 'order',
    selectable: false,
    verticalScroll: true,
    maxHeight,
    zoomKey: 'shiftKey',
  };
}

/** Where the filters leave the chart little room (a phone), it still gets this share of the screen, a scroll down. */
export const CHART_SCREEN_SHARE = 0.7;

/**
 * The chart's tallest: the screen below its top, less what the page keeps below it (the phone tab bar), so the time axis
 * stays in view while the rows scroll. Where that is short (the filters wrap on a phone) the chart takes
 * CHART_SCREEN_SHARE of the screen instead, which leaves a strip of page to drag the page by.
 */
export function chartHeight(viewportHeight: number, topOnPage: number, belowOnPage: number): number {
  const fits = viewportHeight - topOnPage - belowOnPage;
  const share = (viewportHeight - belowOnPage) * CHART_SCREEN_SHARE;
  return Math.floor(Math.max(MIN_CHART_HEIGHT, fits, share));
}

// --- wheel ---

/** The slice of a WheelEvent the gesture reads. */
export interface WheelInput {
  deltaX: number;
  deltaY: number;
  /** 0 pixels, 1 lines, 2 pages. */
  deltaMode: number;
  shiftKey: boolean;
  /** Set by a trackpad pinch in Chromium, Firefox and Safari. */
  ctrlKey: boolean;
}

/** One wheel step in line or page mode, in pixels. */
const LINE_PX = 40;
const PAGE_PX = 800;
/** A zoom of e^(px x rate): one notch of a mouse wheel (100 px) scales the range by about 1.22. */
export const WHEEL_ZOOM_RATE = 0.002;

/**
 * What a wheel event over the chart does. Shift (or a trackpad pinch, which arrives as Ctrl+wheel) zooms by the larger
 * of the two deltas (browsers turn Shift+wheel into deltaX); otherwise a mostly horizontal delta pans the time axis, and
 * a mostly vertical one is left to vis-timeline (it scrolls the rows, and the page once the rows are at an end). A plain
 * wheel never zooms.
 */
export function wheelGesture(e: WheelInput): { kind: 'zoom'; px: number } | { kind: 'pan'; px: number } | { kind: 'rows' } {
  const scale = e.deltaMode === 1 ? LINE_PX : e.deltaMode === 2 ? PAGE_PX : 1;
  const dx = e.deltaX * scale;
  const dy = e.deltaY * scale;
  if (e.shiftKey || e.ctrlKey) return { kind: 'zoom', px: Math.abs(dy) >= Math.abs(dx) ? dy : dx };
  if (Math.abs(dx) > Math.abs(dy)) return { kind: 'pan', px: dx };
  return { kind: 'rows' };
}

/** A range in milliseconds. */
export interface MsRange {
  start: number;
  end: number;
}

/**
 * `range` zoomed by `factor` (above 1 out, below 1 in) around `anchor`, which keeps its place on screen. The length is
 * held between ZOOM_MIN_MS and ZOOM_MAX_MS.
 */
export function zoomAround(range: MsRange, anchor: number, factor: number): MsRange {
  const span = range.end - range.start;
  const next = Math.min(ZOOM_MAX_MS, Math.max(ZOOM_MIN_MS, span * factor));
  const at = span > 0 ? (anchor - range.start) / span : 0.5;
  const start = anchor - at * next;
  return { start, end: start + next };
}

/** `range` moved by `px` pixels of a chart `width` pixels wide (positive is later). */
export function panBy(range: MsRange, px: number, width: number): MsRange {
  const shift = width > 0 ? ((range.end - range.start) * px) / width : 0;
  return { start: range.start + shift, end: range.end + shift };
}

/** The time under `x` pixels from the left edge of a chart `width` pixels wide. */
export function timeAt(range: MsRange, x: number, width: number): number {
  return width > 0 ? range.start + ((range.end - range.start) * x) / width : (range.start + range.end) / 2;
}

/** The part of the DOM the wheel handler and the gesture tracker read: vis-timeline's container and its centre panel. */
interface ChartDom {
  addEventListener?: HTMLElement['addEventListener'];
  removeEventListener?: HTMLElement['removeEventListener'];
  querySelector?: HTMLElement['querySelector'];
}

/** Takes the chart's wheel events before vis-timeline sees them, for the sideways pan and the zooms. */
function listenWheel(container: ChartDom, timeline: ChartTimeline): () => void {
  if (typeof container.addEventListener !== 'function') return () => undefined;
  const onWheel = (event: WheelEvent) => {
    const gesture = wheelGesture(event);
    if (gesture.kind === 'rows') return; // vis-timeline scrolls the rows
    const centre = container.querySelector?.('.vis-panel.vis-center')?.getBoundingClientRect();
    if (centre === undefined || centre.width <= 0) return;
    event.preventDefault();
    event.stopPropagation();
    const { start, end } = timeline.getWindow();
    const range = { start: start.getTime(), end: end.getTime() };
    const next =
      gesture.kind === 'pan'
        ? panBy(range, gesture.px, centre.width)
        : zoomAround(range, timeAt(range, event.clientX - centre.left, centre.width), Math.exp(gesture.px * WHEEL_ZOOM_RATE));
    timeline.setWindow(next.start, next.end, NO_ANIMATION);
  };
  container.addEventListener('wheel', onWheel, { capture: true, passive: false });
  return () => container.removeEventListener?.('wheel', onWheel, { capture: true });
}

// --- touch and pointer gestures ---

/** How far (px) a pointer may move between going down and coming up and still count as a tap: past it, it was a drag. */
export const TAP_SLOP_PX = 10;

/** A gesture on the chart so far: how many pointers took part and the farthest any of them moved from where it went down. */
export interface Gesture {
  pointers: number;
  moved: number;
}

/** What a gesture was: a tap (at most one pointer, moved at most the slop; none seen is a synthetic click), a one-finger drag or a pinch. */
export function gestureKind(gesture: Gesture, slop: number = TAP_SLOP_PX): 'tap' | 'drag' | 'pinch' {
  if (gesture.pointers > 1) return 'pinch';
  return gesture.moved <= slop ? 'tap' : 'drag';
}

/**
 * Follows the pointers on the chart, so a drag or a pinch that ends over a bar does not open it (vis-timeline pans and
 * pinches by itself; this only says afterwards which it was). A gesture starts when the first pointer goes down: one that
 * is primary (no other pointer of its kind is down) or none other is known to be.
 *
 * A pointer can leave without its pointerup or pointercancel reaching the chart: on Chrome for Android a swipe on a row
 * label lost its capture and never ended. Were it still counted, every later tap would look like a pinch and open
 * nothing until a reload; so a primary pointer starts afresh, and losing the capture ends a pointer too.
 */
export function gestureTracker(container: ChartDom) {
  const down = new Map<number, { x: number; y: number }>();
  let gesture: Gesture = { pointers: 0, moved: 0 };
  const ondown = (e: PointerEvent) => {
    if (e.isPrimary) down.clear();
    if (down.size === 0) gesture = { pointers: 0, moved: 0 };
    down.set(e.pointerId, { x: e.clientX, y: e.clientY });
    gesture.pointers += 1;
  };
  const onmove = (e: PointerEvent) => {
    const at = down.get(e.pointerId);
    if (at !== undefined) gesture.moved = Math.max(gesture.moved, Math.hypot(e.clientX - at.x, e.clientY - at.y));
  };
  const onup = (e: PointerEvent) => down.delete(e.pointerId);
  const listeners = [
    ['pointerdown', ondown],
    ['pointermove', onmove],
    ['pointerup', onup],
    ['pointercancel', onup],
    ['lostpointercapture', onup],
  ] as const;
  const options = { capture: true, passive: true };
  if (typeof container.addEventListener === 'function') for (const [name, fn] of listeners) container.addEventListener(name, fn, options);
  return {
    kind: () => gestureKind(gesture),
    stop: () => {
      for (const [name, fn] of listeners) container.removeEventListener?.(name, fn, options);
    },
  };
}

/** The ref of the task a click on the chart opens: a bar of any kind (task, lease or marker) or a task row's label. */
export function clickedTask(click: ChartClick, itemOf: (id: string) => TimelineItem | undefined): string | undefined {
  if (click.what === 'item' && click.item !== undefined && click.item !== null) return itemOf(String(click.item))?.task;
  if (click.what === 'group-label' && click.group !== undefined && click.group !== null) return taskOfGroup(String(click.group));
  return undefined;
}

/** What the view hands the chart: where a click on a task goes, where arrows are drawn, and who hears the range. */
export interface ChartHooks {
  open(ref: string): void;
  arrows: ArrowSurface;
  /** The visible range's length, after every redraw. */
  onSpan(spanMs: number): void;
  /** The visible range, once it stops moving (a drag or a zoom ended). */
  onRange(range: TimeWindow): void;
  /** The visible range, after every redraw and while it moves. */
  onWindow?(range: TimeWindow): void;
}

/** A mounted chart: `show` replaces what it draws, `zoom` steps the range, `destroy` takes it off the page. */
export interface Chart {
  show(model: TimelineModel): void;
  zoom(direction: 'in' | 'out'): void;
  /** Shows `window` (the fragment named another one); the range hooks then follow as for a drag. */
  moveTo(window: TimeWindow): void;
  /** Sets the chart's tallest (the screen changed size). */
  setHeight(px: number): void;
  destroy(): void;
}

const NO_ANIMATION = { animation: false } as const;

/** A group as it is when vis-timeline already holds it: without `showNested`, which is the user's to change (a collapse). */
function withoutCollapse<T extends { id: string }>(group: T & { showNested?: boolean }): T {
  const rest = { ...group };
  delete rest.showNested;
  return rest;
}

/** The rows a chart holds, by id, as the text they were last drawn from: what a refresh compares against. */
type Held = Map<string, string>;

/**
 * Makes `data` hold exactly `rows`: rows that are gone are removed, rows that changed are merged in, new rows are added,
 * and rows that are the same as last time are not touched at all. Nothing is cleared and re-added, so a row that stays
 * keeps what vis-timeline holds about it (a collapse), a refresh does not flicker, and a drag or a click under way is not
 * disturbed by an answer that changed nothing. `merged` shapes a changed row that `data` already holds.
 */
function replaceRows<T extends { id: string }>(data: ChartData<unknown>, rows: readonly T[], held: Held, merged: (row: T) => T) {
  const next: Held = new Map(rows.map((row) => [row.id, JSON.stringify(row)]));
  data.remove([...held.keys()].filter((id) => !next.has(id)));
  data.update(rows.filter((row) => held.get(row.id) !== next.get(row.id)).map((row) => (held.has(row.id) ? merged(row) : row)));
  held.clear();
  next.forEach((text, id) => held.set(id, text));
}

/** Draws `model` with vis-timeline into `container`. */
export function mountChart(lib: ChartLib, container: HTMLElement, model: TimelineModel, window: TimeWindow, hooks: ChartHooks, height?: number): Chart {
  const items = new lib.DataSet<unknown>(model.items);
  const groups = new lib.DataSet<unknown>(model.groups);
  const timeline = new lib.Timeline(container, items, groups, chartOptions(window, height));
  const unlistenWheel = listenWheel(container, timeline);
  const gestures = gestureTracker(container);
  let current = model;
  const heldGroups: Held = new Map(model.groups.map((g) => [g.id, JSON.stringify(g)]));
  const heldItems: Held = new Map(model.items.map((i) => [i.id, JSON.stringify(i)]));
  const byId = (id: string) => current.items.find((item) => item.id === id);
  timeline.on('click', (click) => {
    if (gestures.kind() !== 'tap') return; // a drag or a pinch that ended over a bar opens nothing
    const ref = clickedTask(click, byId);
    if (ref !== undefined) hooks.open(ref);
  });
  // vis-timeline fires `changed` after a redraw (a collapse, new data) and `rangechange` and `rangechanged` while the
  // range moves (a zoom, a pan). The span readout and the arrows follow all three.
  const sync = () => {
    const shown = timeline.getWindow();
    hooks.onSpan(spanOf(shown));
    hooks.onWindow?.({ from: shown.start, to: shown.end });
    drawArrows(hooks.arrows, current.dependencies);
  };
  timeline.on('changed', sync);
  timeline.on('rangechange', sync);
  // vis-timeline counts its first draw done only on a redraw after the first `rangechanged` (200 ms after it opens); until
  // then its loading screen covers the chart and swallows the wheel. Nothing else redraws a chart that is left alone.
  timeline.on('rangechanged', () => {
    timeline.redraw();
    sync();
    const { start, end } = timeline.getWindow();
    hooks.onRange({ from: start, to: end });
  });
  return {
    show(next: TimelineModel) {
      current = next;
      replaceRows(groups, next.groups, heldGroups, withoutCollapse);
      replaceRows(items, next.items, heldItems, (row) => row);
    },
    zoom(direction) {
      if (direction === 'in') timeline.zoomIn(ZOOM_FACTOR, NO_ANIMATION);
      else timeline.zoomOut(ZOOM_FACTOR, NO_ANIMATION);
    },
    moveTo(window: TimeWindow) {
      timeline.setWindow(window.from, window.to, NO_ANIMATION);
    },
    setHeight(px: number) {
      timeline.setOptions({ maxHeight: px });
    },
    destroy() {
      unlistenWheel();
      gestures.stop();
      timeline.destroy();
    },
  };
}

/** The name of the view's saved state (PRD R16): the key is `board.view.v1.timeline`. */
export const TIMELINE_VIEW = 'timeline';

/** The filter the view opens with, and whether it came from the saved query (a bare route) rather than a link's own query. */
export interface OpenedTimeline {
  filter: TimelineFilter;
  restored: boolean;
  /** The window the fragment names (`from`/`to`), when it names a usable one. */
  window?: TimeWindow;
}

/**
 * The query keys that are the view's state rather than its filter: the window and the previewed task. They go in the
 * fragment (a link or Back shows the same range) but are never saved.
 */
const VIEW_KEYS = ['from', 'to', 'task'] as const;

/** A window time in the fragment: ISO to the second, UTC. */
function windowTime(d: Date): string {
  return d.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

/** The window a query names, or undefined unless both ends parse, `from` is before `to` and the length is in the zoom range. */
export function windowFromQuery(query: string): TimeWindow | undefined {
  const params = new URLSearchParams(query);
  const from = Date.parse(params.get('from') ?? '');
  const to = Date.parse(params.get('to') ?? '');
  if (!Number.isFinite(from) || !Number.isFinite(to)) return undefined;
  const span = to - from;
  if (span < ZOOM_MIN_MS / 2 || span > ZOOM_MAX_MS * 2) return undefined;
  return { from: new Date(from), to: new Date(to) };
}

/** A query without the window's and the task's keys: what the filter is, and what is saved. */
export function withoutWindow(query: string): string {
  const params = new URLSearchParams(query);
  for (const key of VIEW_KEYS) params.delete(key);
  return params.toString();
}

/** The fragment of the view: the filter's query, then the window when there is one (`#/timeline?tag=x&from=…&to=…`). */
export function timelineViewHash(filter: TimelineFilter, window?: TimeWindow): string {
  const params = new URLSearchParams(filterToQuery(filter));
  if (window !== undefined) {
    params.set('from', windowTime(window.from));
    params.set('to', windowTime(window.to));
  }
  const query = params.toString();
  return query === '' ? '#/timeline' : `#/timeline?${query}`;
}

/**
 * Opens the view for the fragment it is on, before its first fetch. A fragment with a query wins and is saved; a bare one
 * (`#/timeline`, or any fragment the router sends here) restores the saved query by rewriting the fragment in place, so
 * the view reads `GET /timeline` once, under the restored filter.
 */
export function openTimeline(env: ViewEnv, hash: string): OpenedTimeline {
  const at = hash.indexOf('?');
  const full = at < 0 ? '' : hash.slice(at + 1);
  const own = withoutWindow(full);
  const window = windowFromQuery(full);
  // A fragment that names a window describes the whole view (a link, or Back to this entry): its filter, even none, is
  // the one shown, and nothing saved replaces it.
  if (window !== undefined && own === '') return { filter: filterFromQuery(''), restored: false, window };
  const query = openView(env, TIMELINE_VIEW, '#/timeline', own);
  const opened = { filter: filterFromQuery(query), restored: own === '' && query !== '' };
  return window === undefined ? opened : { ...opened, window };
}

/** Saves the filter's query after a change (the empty filter is the default and removes the key). */
export function saveTimeline(storage: ViewStorage | undefined, filter: TimelineFilter): void {
  saveViewQuery(storage, TIMELINE_VIEW, filterToQuery(filter));
}

/** Clear filters: forgets the saved query. */
export function forgetTimeline(storage: ViewStorage | undefined): void {
  clearViewQuery(storage, TIMELINE_VIEW);
}

/**
 * A restored filter without the initiatives and tags the board no longer has: `known` is the list of initiatives with
 * their tags. The actor and the closed-within choice are kept. A filter with nothing to drop is returned as it is (the
 * same object).
 */
export function dropUnknown(filter: TimelineFilter, known: readonly { slug: string; tags: readonly string[] }[]): TimelineFilter {
  const slugs = new Set(known.map((i) => i.slug));
  const tags = new Set(known.flatMap((i) => i.tags));
  const initiatives = filter.initiatives.filter((slug) => slugs.has(slug));
  const kept = filter.tags.filter((tag) => tags.has(tag));
  if (initiatives.length === filter.initiatives.length && kept.length === filter.tags.length) return filter;
  return { ...filter, initiatives, tags: kept };
}

/**
 * The filter the view starts from (or moves to on a hash change). Only a restored filter is checked, and only once the
 * lookup answered: `known` is the board's initiatives with their tags, or `undefined` when the lookup failed. An empty
 * list is an answer (the board has no initiatives), so every restored initiative and tag is dropped. A link's own query
 * is kept as it is, and the chart shows the server's answer for it.
 */
export function startFilter(opened: OpenedTimeline, known: readonly { slug: string; tags: readonly string[] }[] | undefined): TimelineFilter {
  if (!opened.restored || known === undefined) return opened.filter;
  return dropUnknown(opened.filter, known);
}
