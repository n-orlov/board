// The Timeline view's data layer and its vis-timeline glue (PRD R6, SPEC 11): `TimelineApi` reads `GET /timeline` for
// the view's filter and window, `TimelineViewStore` holds the mapped model (lib/timeline.ts) the view draws, and
// `mountChart` hands that model to vis-timeline. vis-timeline is bundled with the app (an npm dependency, no CDN); the
// view imports its classes and passes them in as `ChartLib`, so this module stays testable without a DOM.
import { errorText, Fetches } from './fetches';
import { RESTORE_WAIT_MS } from './historyEntry';
import { ThreadApi } from './threads';
import { labelNode, type LabelElement, type LabelGroup } from './timelineLabel';
import { apiQuery, buildTimeline, filterFromQuery, filterToQuery, taskOfGroup, type Timeline, type TimelineAnswer, type TimelineFilter, type TimelineItem, type TimelineModel } from './timeline';
import { drawArrows, type ArrowSurface } from './timelineArrows';
import { activeFirst, needsActiveSet, refsOf } from './timelineActive';
import { DEFAULT_WINDOW, type NowWindow } from './nowFlight';
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

  /** One `GET /timeline` under a query. */
  private async fetchTimeline(query: URLSearchParams): Promise<Timeline> {
    const response = await this.call(`/api/v1/timeline?${query.toString()}`, 'Reading the timeline', {
      headers: { Accept: 'application/json' },
    });
    return (await response.json()) as Timeline;
  }

  /**
   * The timeline under the filter in the window. `active` is the touched-in window of the active definition (what Only
   * active and Active first use): Only active sends it with `active=true`, and Active first on the whole board reads
   * the active set the same way and puts those tasks first, so the server alone decides what active means.
   */
  async read(filter: TimelineFilter, window: TimeWindow, active: NowWindow = DEFAULT_WINDOW): Promise<TimelineAnswer> {
    return this.readRows(filter, window, active);
  }

  private async readRows(filter: TimelineFilter, window: TimeWindow, active: NowWindow): Promise<TimelineAnswer> {
    const only = filter.activeOnly ? active : undefined;
    const answer = this.fetchTimeline(apiQuery(filter, window.from, window.to, only));
    if (!needsActiveSet(filter.activeOnly, filter.order)) return answer;
    const [all, set] = await Promise.all([answer, this.fetchTimeline(apiQuery(filter, window.from, window.to, active))]);
    return activeFirst(all, refsOf(set));
  }
}

export interface TimelineViewState {
  filter: TimelineFilter;
  window: TimeWindow;
  /** The touched-in window of "active" (8h, 24h or 3d): Now's choice, shared. */
  activeWindow: NowWindow;
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
    activeWindow: NowWindow = DEFAULT_WINDOW,
  ) {
    this.api = api;
    this.publish = publish;
    this.now = now;
    this.timers = timers;
    this.state = { filter, window: window ?? initialWindow(now()), activeWindow, model: EMPTY_MODEL, loaded: false, loading: false, error: '' };
  }

  private set(patch: Partial<TimelineViewState>) {
    this.state = { ...this.state, ...patch };
    this.publish(this.state);
  }

  /** Reads the timeline under the current filter and window; a read that lands after a newer one is dropped. */
  async load(): Promise<void> {
    const ticket = this.fetches.begin();
    this.set({ loading: true });
    const { filter, window, activeWindow } = this.state;
    try {
      const answer = await this.api.read(filter, window, activeWindow);
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

  /** Applies a new touched-in window and reads again. */
  setActiveWindow(activeWindow: NowWindow): Promise<void> {
    this.set({ activeWindow });
    return this.load();
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
  /** Every row it holds, as vis-timeline last left it (a collapse is in `showNested`). */
  get(): T[];
  remove(ids: (string | number)[]): unknown;
  /** Adds the rows it does not hold and merges the fields of the ones it does. */
  update(rows: T[]): unknown;
}

/** What vis-timeline makes of an input event (getEventProperties): what was hit, and the item or group under the pointer. */
export interface ChartClick {
  what: string | null;
  item?: string | number | null;
  group?: string | number | null;
}

/** The slice of vis-timeline's Timeline the chart uses. */
export interface ChartTimeline {
  destroy(): void;
  /** What the chart has under a DOM event: the item from its target, the group from its height. */
  getEventProperties(event: Pick<MouseEvent, 'target' | 'clientX' | 'clientY'>): ChartClick;
  on(event: 'changed' | 'rangechange' | 'rangechanged', listener: () => void): void;
  getWindow(): { start: Date; end: Date };
  setWindow(start: Date | number, end: Date | number, options: { animation: boolean }): void;
  setOptions(options: Partial<ChartOptions>): void;
  redraw(): void;
  zoomIn(percentage: number, options: { animation: boolean }): void;
  zoomOut(percentage: number, options: { animation: boolean }): void;
  /** vis-timeline's row store, where an initiative's collapse is toggled (what its own tap on the label calls); absent in a fake. */
  itemSet?: { groups: Record<string, unknown>; toggleGroupShowNested(group: unknown, force?: boolean): void };
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
  /**
   * Item fields vis-timeline writes as `data-` attributes: `data-task` names the task a bar opens, `data-id` its item
   * (and so its row) for a tap that lands beside it.
   */
  dataAttributes: string[];
  /** Builds a row's label: its text, or for a row with tags the title over the chips as elements (vis-timeline strips styled HTML). */
  groupTemplate: (group: LabelGroup | null) => LabelElement | string;
}

/** The label of a row in the page's document (`labelNode`); only a chart on a page calls it. */
export const groupTemplate = (group: LabelGroup | null): LabelElement | string => labelNode(group, document as unknown as Parameters<typeof labelNode>[1]);

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
    dataAttributes: ['task', 'id'],
    groupTemplate,
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
  ownerDocument?: Partial<Pick<Document, 'elementFromPoint' | 'activeElement' | 'body'>> | null;
  addEventListener?: HTMLElement['addEventListener'];
  removeEventListener?: HTMLElement['removeEventListener'];
  querySelector?: HTMLElement['querySelector'];
  querySelectorAll?: HTMLElement['querySelectorAll'];
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

/**
 * How far (px) from a bar a tap on the empty part of its row still opens it: a fingertip is wider than a short bar (a
 * few px at a wide zoom) or a marker (about 19 px), and Chrome's own touch adjustment does not always reach that far.
 */
export const TAP_REACH_PX = 16;

/** A drawn bar where a tap may land: its task, its row and its box on screen. */
export interface BarBox {
  task: string;
  group: string | undefined;
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/** The bar in `group` nearest to (x, y) and at most `reach` px from it (0 inside it); the first drawn wins a tie. */
export function nearestBar<B extends BarBox>(bars: readonly B[], x: number, y: number, group: string, reach: number = TAP_REACH_PX): B | undefined {
  let best: { bar: B; distance: number } | undefined;
  for (const bar of bars) {
    if (bar.group !== group) continue;
    const distance = Math.hypot(Math.max(bar.left - x, 0, x - bar.right), Math.max(bar.top - y, 0, y - bar.bottom));
    if (distance <= reach && (best === undefined || distance < best.distance)) best = { bar, distance };
  }
  return best?.bar;
}

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
 * is primary (no other pointer of its kind is down) or none other is known to be; `onstart` hears that pointerdown.
 *
 * A pointer can leave without its pointerup or pointercancel reaching the chart: on Chrome for Android a swipe on a row
 * label lost its capture and never ended. Were it still counted, every later tap would look like a pinch and open
 * nothing until a reload; so a primary pointer starts afresh, and losing the capture ends a pointer too.
 * `onrelease` hears the pointerup of a pointer of the gesture (not a cancel, not a lost capture).
 */
export function gestureTracker(
  container: ChartDom,
  onstart: (event: PointerEvent) => void = () => undefined,
  onrelease: (event: PointerEvent) => void = () => undefined,
) {
  const down = new Map<number, { x: number; y: number }>();
  let gesture: Gesture = { pointers: 0, moved: 0 };
  const ondown = (e: PointerEvent) => {
    if (e.isPrimary) down.clear();
    if (down.size === 0) {
      gesture = { pointers: 0, moved: 0 };
      onstart(e);
    }
    down.set(e.pointerId, { x: e.clientX, y: e.clientY });
    gesture.pointers += 1;
  };
  const onmove = (e: PointerEvent) => {
    const at = down.get(e.pointerId);
    if (at !== undefined) gesture.moved = Math.max(gesture.moved, Math.hypot(e.clientX - at.x, e.clientY - at.y));
  };
  const onup = (e: PointerEvent) => down.delete(e.pointerId);
  const onlift = (e: PointerEvent) => {
    if (down.delete(e.pointerId)) onrelease(e);
  };
  const listeners = [
    ['pointerdown', ondown],
    ['pointermove', onmove],
    ['pointerup', onlift],
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

/** The bars that open a task: vis-timeline writes the item's `task` as `data-task` (chartOptions' dataAttributes). */
const TASK_BAR = '.vis-item[data-task]';

/** The task bar an event happened on, or null (an element outside one, or no element at all). */
function barOf(target: unknown): HTMLElement | null {
  const el = target as Partial<Pick<Element, 'closest'>> | null | undefined;
  return typeof el?.closest === 'function' ? el.closest<HTMLElement>(TASK_BAR) : null;
}

/** The task bars drawn in `container`, with their rows (from the model) and boxes. */
function barBoxes(container: ChartDom, itemOf: (id: string) => TimelineItem | undefined): (BarBox & { el: HTMLElement })[] {
  return [...(container.querySelectorAll?.<HTMLElement>(TASK_BAR) ?? [])].map((el) => {
    const box = el.getBoundingClientRect();
    const group = itemOf(el.dataset.id ?? '')?.group;
    return { el, task: el.dataset.task ?? '', group, left: box.left, top: box.top, right: box.right, bottom: box.bottom };
  });
}

/** A task row's or an initiative row's label an event happened on, or null. */
function labelOf(target: unknown): Element | null {
  const el = target as Partial<Pick<Element, 'closest'>> | null | undefined;
  return typeof el?.closest === 'function' ? el.closest('.vis-label') : null;
}

/**
 * What a press or a click is on: `key` names a task to open (`t:` and its ref) or an initiative row (`g:` and its group
 * id), and is empty on anything else; `el` is the element it is on (the bar, the bar beside it or the row label) and
 * `top` that element's top on screen then.
 */
export interface Press {
  key: string;
  el: Element | null;
  top: number;
}

function press(key: string, el: Element | null): Press {
  return { key, el, top: typeof el?.getBoundingClientRect === 'function' ? el.getBoundingClientRect().top : 0 };
}

/** Where an event on the chart happened: the element it is on and its point on screen (a DOM event, or a point hit-tested). */
type PointAt = Pick<MouseEvent, 'target' | 'clientX' | 'clientY'>;

/**
 * Where a pointer came up: the element under the release point and that point. A touch's pointerup is aimed at the element
 * it went down on (the implicit capture) and its click at where it went down, so neither says what the finger left the
 * screen over; the document's hit test does. Undefined without one (a chart outside a document).
 */
export function releasePoint(container: ChartDom, event: PointerEvent): PointAt | undefined {
  const hit = container.ownerDocument?.elementFromPoint;
  if (typeof hit !== 'function') return undefined;
  return { target: hit.call(container.ownerDocument, event.clientX, event.clientY), clientX: event.clientX, clientY: event.clientY };
}

/**
 * What an event on the chart is on: what vis-timeline finds under it (a bar or a row's label), else the nearest bar of
 * the row whose empty part it landed on, within TAP_REACH_PX.
 */
function pressOf(event: PointAt, timeline: ChartTimeline, container: ChartDom, itemOf: (id: string) => TimelineItem | undefined): Press {
  const props = timeline.getEventProperties(event);
  const ref = clickedTask(props, itemOf);
  if (ref !== undefined) return press(`t:${ref}`, barOf(event.target) ?? labelOf(event.target));
  if (props.group === undefined || props.group === null) return press('', null);
  if (props.what === 'group-label') return press(`g:${String(props.group)}`, labelOf(event.target));
  return props.what === 'background' ? pressBeside(event, container, itemOf, String(props.group)) : press('', null);
}

/** The press on the empty part of the row `group`: the nearest bar of that row within TAP_REACH_PX, or none. */
function pressBeside(event: PointAt, container: ChartDom, itemOf: (id: string) => TimelineItem | undefined, group: string): Press {
  const near = nearestBar(barBoxes(container, itemOf), event.clientX, event.clientY, group);
  return near === undefined || near.task === '' ? press('', null) : press(`t:${near.task}`, near.el);
}

/**
 * The task a click opens: the one it is on, but only when the press it ends began on that same task and element and the
 * element has not moved since. Between the press and the click vis-timeline may collapse or expand an initiative (its
 * own tap on the label), or new rows may be drawn, and the click then lands on whatever moved under the finger: it opens
 * nothing. An initiative's label only collapses or expands it. A click with no press seen (a synthetic one) opens what it
 * is on.
 */
export function pressOpens(down: Press | undefined, up: Press): string | undefined {
  if (!up.key.startsWith('t:')) return undefined;
  if (down !== undefined && (down.key !== up.key || down.el !== up.el || Math.abs(down.top - up.top) > 1)) return undefined;
  return up.key.slice(2);
}

/**
 * The task a tap opens: the one its click opens (pressOpens), but only when the pointer also came up on that same element
 * (`released`, taken at pointerup). A finger pressed on one task and lifted over another task's row opens nothing, though
 * the browser aims its click at where the finger went down. No release seen (a synthetic click) leaves the click to decide.
 */
export function tapOpens(down: Press | undefined, released: Press | undefined, up: Press): string | undefined {
  const ref = pressOpens(down, up);
  if (ref === undefined || released === undefined) return ref;
  return pressOpens(down, released) === ref ? ref : undefined;
}

/** Hears the chart's DOM clicks. */
function listenClicks(container: ChartDom, onclick: (event: MouseEvent) => void): () => void {
  if (typeof container.addEventListener !== 'function') return () => undefined;
  container.addEventListener('click', onclick);
  return () => container.removeEventListener?.('click', onclick);
}

/**
 * Lets every task bar take the focus from script (`tabindex=-1`, never the Tab order, and no role: a bar is no standalone
 * control, its row label is the 44 px one), so a click can leave the focus on the bar the preview opens from. Once per bar
 * vis-timeline draws.
 */
function focusableBars(container: ChartDom): void {
  container.querySelectorAll?.<HTMLElement>(`${TASK_BAR}:not([tabindex])`).forEach((bar) => {
    bar.tabIndex = -1;
  });
}

/** A row label: vis-timeline draws one `.vis-label` per visible row, in row order (an initiative's also `.vis-nesting-group`). */
const ROW_LABEL = '.vis-label';

/**
 * Puts every row label in the Tab order, in row order, as a button, and says on an initiative's whether its tasks are
 * shown (`aria-expanded`, from the `expanded` or `collapsed` class vis-timeline keeps on it). A label drawn since the last
 * call is made one; the state is read again on each call. Run after each redraw.
 */
export function operableLabels(container: ChartDom): void {
  container.querySelectorAll?.<HTMLElement>(ROW_LABEL).forEach((label) => {
    if (!label.hasAttribute('tabindex')) {
      label.tabIndex = 0;
      label.setAttribute('role', 'button');
    }
    if (label.classList.contains('vis-nesting-group')) label.setAttribute('aria-expanded', String(label.classList.contains('expanded')));
  });
}

/** Whether a key press activates a focused label: Enter or Space, bare and not held down (a held key would toggle again and again). */
export function activatesLabel(event: Pick<KeyboardEvent, 'key' | 'ctrlKey' | 'altKey' | 'metaKey' | 'shiftKey' | 'repeat'>): boolean {
  if (event.ctrlKey || event.altKey || event.metaKey || event.shiftKey || event.repeat) return false;
  return event.key === 'Enter' || event.key === ' ';
}

/** The point at the middle of a label, where vis-timeline finds its row (the row it is the label of). */
function centreOf(label: Element): PointAt {
  const box = label.getBoundingClientRect();
  return { target: label, clientX: box.left + box.width / 2, clientY: box.top + box.height / 2 };
}

/**
 * Gives `label` the focus back when a redraw dropped it: to reorder its rows after a collapse or an expand vis-timeline takes
 * every label out of the page and puts them back, and a focused one is left with no focus at all (the body has it). A focus
 * that went anywhere else is not taken.
 */
export function refocusLabel(container: ChartDom, label: HTMLElement): void {
  const doc = container.ownerDocument;
  const active = doc?.activeElement;
  if (!label.isConnected || (active !== null && active !== undefined && active !== doc?.body)) return;
  label.focus({ preventScroll: true });
}

/** Hears the chart's key presses. */
function listenKeys(container: ChartDom, onkey: (event: KeyboardEvent) => void): () => void {
  if (typeof container.addEventListener !== 'function') return () => undefined;
  container.addEventListener('keydown', onkey);
  return () => container.removeEventListener?.('keydown', onkey);
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
  /** How far the rows are scrolled, as they move (for Back to put them back). */
  onRowScroll?(px: number): void;
  /** The initiative rows (their ids) that open collapsed: what Back, Forward or a reload restores. */
  collapsed?: readonly string[];
  /** The collapsed initiatives' ids (sorted), each time a collapse or an expand changes them. */
  onCollapse?(ids: string[]): void;
}

/** A mounted chart: `show` replaces what it draws, `zoom` steps the range, `destroy` takes it off the page. */
export interface Chart {
  show(model: TimelineModel): void;
  zoom(direction: 'in' | 'out'): void;
  /** Shows `window` (the fragment named another one); the range hooks then follow as for a drag. */
  moveTo(window: TimeWindow): void;
  /** Sets the chart's tallest (the screen changed size). */
  setHeight(px: number): void;
  /** Draws the chart again at its frame's size (the label column's width changed). */
  redraw(): void;
  /** Scrolls the rows to `px` once they are drawn tall enough (Back to this view). */
  scrollRows(px: number): void;
  /**
   * Reports what the chart displays now, ahead of the debounce and the redraw that would report it: the rows' scroll
   * the user last left (when no restore is still under way) and the collapsed initiatives. The history entry being
   * left calls it before it changes owner; `destroy` does it too.
   */
  flush(): void;
  /** Shows another history entry's state (a Back or a link to this view): its collapsed initiatives and its rows' scroll. */
  showEntry(state: { collapsed: readonly string[]; rows: number }): void;
  destroy(): void;
}

const NO_ANIMATION = { animation: false } as const;

/** A group as it is when vis-timeline already holds it: without `showNested`, which is the user's to change (a collapse). */
function withoutCollapse<T extends { id: string }>(group: T & { showNested?: boolean }): T {
  const rest = { ...group };
  delete rest.showNested;
  return rest;
}

/** The most collapsed initiatives one entry remembers: a saved value past it is not one this view wrote. */
const COLLAPSED_MAX = 500;

/** The collapsed initiatives' ids a saved value holds, in order and without repeats; anything else is no collapse at all. */
export function collapsedOf(value: unknown): string[] {
  if (!Array.isArray(value) || value.length > COLLAPSED_MAX) return [];
  const ids = value.filter((id): id is string => typeof id === 'string' && id !== '');
  return [...new Set(ids)].sort();
}

/** The set to save: sorted, so the same collapse is always the same value. */
export function encodeCollapsed(ids: Iterable<string>): string[] {
  return collapsedOf([...ids]);
}

/** A row as far as a collapse is concerned. */
interface Nesting {
  id: string;
  nestedGroups?: string[];
  showNested?: boolean;
}

/** The groups with the initiatives named in `collapsed` drawn collapsed (a row that is not an initiative is left alone). */
export function withCollapsed<G extends Nesting>(groups: readonly G[], collapsed: ReadonlySet<string>): G[] {
  if (collapsed.size === 0) return [...groups];
  return groups.map((g) => (g.nestedGroups !== undefined && collapsed.has(g.id) ? { ...g, showNested: false } : g));
}

/** `collapsed` after a redraw: an initiative held open leaves it, one held shut joins it, one not held is left as it was. */
export function collapseAfter(collapsed: ReadonlySet<string>, held: readonly Nesting[]): Set<string> {
  const next = new Set(collapsed);
  for (const g of held) {
    if (g.nestedGroups === undefined) continue;
    if (g.showNested === false) next.add(g.id);
    else next.delete(g.id);
  }
  return next;
}

/** The rows a chart holds, by id, as the text they were last drawn from: what a refresh compares against. */
type Held = Map<string, string>;

/**
 * Makes `data` hold exactly `rows`: rows that are gone are removed, rows that changed are merged in, new rows are added,
 * and rows that are the same as last time are not touched at all. Nothing is cleared and re-added, so a row that stays
 * keeps what vis-timeline holds about it (a collapse), a refresh does not flicker, and a drag or a click under way is not
 * disturbed by an answer that changed nothing. `merged` shapes a changed row that `data` already holds.
 */
function replaceRows<T extends { id: string }>(data: ChartData<unknown>, rows: readonly T[], held: Held, merged: (row: T) => T, added: (row: T) => T = (row) => row) {
  const next: Held = new Map(rows.map((row) => [row.id, JSON.stringify(row)]));
  data.remove([...held.keys()].filter((id) => !next.has(id)));
  data.update(rows.filter((row) => held.get(row.id) !== next.get(row.id)).map((row) => (held.has(row.id) ? merged(row) : added(row))));
  held.clear();
  next.forEach((text, id) => held.set(id, text));
}

/** Draws `model` with vis-timeline into `container`. */
export function mountChart(lib: ChartLib, container: HTMLElement, model: TimelineModel, window: TimeWindow, hooks: ChartHooks, height?: number): Chart {
  let collapsed: ReadonlySet<string> = new Set(hooks.collapsed ?? []);
  const items = new lib.DataSet<unknown>(model.items);
  const groups = new lib.DataSet<unknown>(withCollapsed(model.groups, collapsed));
  const timeline = new lib.Timeline(container, items, groups, chartOptions(window, height));
  const unlistenWheel = listenWheel(container, timeline);
  const rows = rowScroll(container, hooks);
  let current = model;
  const heldGroups: Held = new Map(model.groups.map((g) => [g.id, JSON.stringify(g)]));
  const heldItems: Held = new Map(model.items.map((i) => [i.id, JSON.stringify(i)]));
  const byId = (id: string) => current.items.find((item) => item.id === id);
  // What a tap is on is taken when it goes down, before vis-timeline acts on it (it collapses an initiative on its own
  // tap, ahead of the click), and the click opens it only if it is still the same (pressOpens).
  // What it came up on is taken at pointerup, before vis-timeline's own tap acts, from the release point (releasePoint).
  let pressed: Press | undefined;
  let released: Press | undefined;
  const gestures = gestureTracker(
    container,
    (event) => {
      pressed = pressOf(event, timeline, container, byId);
      released = undefined;
    },
    (event) => {
      const at = releasePoint(container, event);
      released = at === undefined ? undefined : pressOf(at, timeline, container, byId);
    },
  );
  // The browser's own click opens a task, not vis-timeline's `click`: that one fires twice for a tap (from its tap
  // recogniser on pointerup, then from the DOM click), and the first, which also drops a press held past 250 ms, would
  // open the dialog before the browser's click, which then lands on the dialog (its backdrop closes it).
  const onclick = (event: MouseEvent) => {
    const down = pressed;
    const lifted = released;
    pressed = undefined;
    released = undefined;
    if (gestures.kind() !== 'tap') return; // a drag or a pinch that ended over a bar opens nothing
    const up = pressOf(event, timeline, container, byId);
    const ref = tapOpens(down, lifted, up);
    if (ref === undefined) return;
    // The bar tapped takes the focus first, so the dialog the view opens gives it back to that bar when it closes.
    barOf(up.el)?.focus({ preventScroll: true });
    hooks.open(ref);
  };
  const unlistenClicks = listenClicks(container, onclick);
  // Enter or Space on a focused row label does what a tap on it does: a task's opens its preview (the label keeps the focus,
  // which the dialog gives back on close), an initiative's collapses or expands it.
  let toggled: HTMLElement | undefined;
  const onkey = (event: KeyboardEvent) => {
    const label = labelOf(event.target);
    if (label === null || label !== event.target || !activatesLabel(event)) return;
    event.preventDefault();
    const row = pressOf(centreOf(label), timeline, container, byId);
    if (row.key.startsWith('t:')) hooks.open(row.key.slice(2));
    else if (row.key.startsWith('g:')) {
      const sets = timeline.itemSet;
      sets?.toggleGroupShowNested(sets.groups[row.key.slice(2)]);
      operableLabels(container);
      toggled = label as HTMLElement;
    }
  };
  const unlistenKeys = listenKeys(container, onkey);
  // vis-timeline fires `changed` after a redraw (a collapse, new data) and `rangechange` and `rangechanged` while the
  // range moves (a zoom, a pan). The span readout and the arrows follow all three.
  // vis-timeline collapses or expands an initiative on its own tap and redraws; the redraw is where it shows.
  const reportCollapse = () => {
    const next = collapseAfter(collapsed, groups.get() as Nesting[]);
    if (next.size === collapsed.size && [...next].every((id) => collapsed.has(id))) return;
    collapsed = next;
    hooks.onCollapse?.(encodeCollapsed(next));
  };
  // The collapse as the data now holds it, which vis-timeline sets when it toggles, ahead of the redraw that calls `sync`;
  // and the rows' scroll, ahead of its debounce.
  const flush = () => {
    rows.flush();
    reportCollapse();
  };
  const sync = () => {
    const shown = timeline.getWindow();
    hooks.onSpan(spanOf(shown));
    hooks.onWindow?.({ from: shown.start, to: shown.end });
    drawArrows(hooks.arrows, current.dependencies);
    rows.settle();
    focusableBars(container);
    operableLabels(container);
    if (toggled !== undefined) refocusLabel(container, toggled);
    toggled = undefined; // only the redraw its own toggle caused
    reportCollapse();
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
      replaceRows(groups, next.groups, heldGroups, withoutCollapse, (row) => withCollapsed([row], collapsed)[0]!);
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
    redraw() {
      timeline.redraw();
    },
    scrollRows(px: number) {
      rows.restore(px);
    },
    flush,
    showEntry(state) {
      collapsed = new Set(state.collapsed);
      const sets = timeline.itemSet;
      for (const g of groups.get() as Nesting[]) {
        if (g.nestedGroups === undefined || (g.showNested === false) === collapsed.has(g.id)) continue;
        sets?.toggleGroupShowNested(sets.groups[g.id], !collapsed.has(g.id));
      }
      operableLabels(container);
      rows.show(state.rows);
    },
    destroy() {
      flush();
      unlistenWheel();
      unlistenClicks();
      unlistenKeys();
      gestures.stop();
      rows.stop();
      timeline.destroy();
    },
  };
}

/** How long the rows' scroll settles before it is reported: a drag of the scrollbar is one report, not hundreds. */
const ROW_SCROLL_REPORT_MS = 100;

/** The input that moves the rows (a wheel, a touch, a key, a press on the scrollbar): the user's own scroll, which wins over a restore. */
const ROW_SCROLL_INPUT = ['wheel', 'touchstart', 'keydown', 'mousedown'] as const;

/**
 * The rows' scroll: vis-timeline keeps it as the label panel's scrollTop (and follows that panel's scroll events), so
 * reading it there reports it and writing it there moves the rows. A restore is bounded and the user's to override,
 * like the page's (historyEntry.ts): it waits for a redraw tall enough to hold it, for at most RESTORE_WAIT_MS, then
 * settles as far as the rows allow; input that scrolls the rows meanwhile cancels it, so no later redraw puts the stale
 * offset back. After a cancel or the give-up the scroll is reported as the user leaves it.
 */
function rowScroll(container: ChartDom, hooks: ChartHooks) {
  const panel = () => container.querySelector?.('.vis-panel.vis-left') ?? null;
  let pending: number | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let deadline: ReturnType<typeof setTimeout> | undefined;
  const report = () => {
    timer = undefined;
    const p = panel();
    // A panel the page has already dropped (the view is being replaced) has no scroll: it reads 0, which is not the user's.
    if (p !== null && p.isConnected !== false && pending === undefined) hooks.onRowScroll?.(p.scrollTop);
  };
  /**
   * Reports where the rows are now, ahead of the debounce (the entry is being left or the page unloaded). The scroll
   * event of a scroll just made may not even have fired yet, so the panel itself is read, not whether a report waits.
   */
  const flush = () => {
    if (timer !== undefined) clearTimeout(timer);
    report();
  };
  const onScroll = () => {
    timer ??= setTimeout(report, ROW_SCROLL_REPORT_MS);
  };
  /** Puts the pending offset as far as the rows allow; true once they hold all of it. */
  const place = () => {
    const p = panel();
    if (pending === undefined || p === null) return false;
    const room = p.scrollHeight - p.clientHeight;
    if (room <= 0) return false;
    p.scrollTop = Math.min(pending, room);
    return room >= pending;
  };
  const end = () => {
    pending = undefined;
    if (deadline !== undefined) clearTimeout(deadline);
    deadline = undefined;
  };
  const giveUp = () => {
    deadline = undefined;
    place();
    end();
  };
  /** A sideways wheel pans the time axis, not the rows: only the rest of the input is the user scrolling them. */
  const onWheel = (event: Event) => {
    if ((event as WheelEvent).deltaY !== 0) end();
  };
  const inputs = ROW_SCROLL_INPUT.map((type) => [type, type === 'wheel' ? onWheel : end] as const);
  container.addEventListener?.('scroll', onScroll, { capture: true, passive: true });
  for (const [type, fn] of inputs) container.addEventListener?.(type, fn, { capture: true, passive: true });
  return {
    restore(px: number) {
      end();
      if (px <= 0) return;
      pending = px;
      deadline = setTimeout(giveUp, RESTORE_WAIT_MS);
    },
    /** After a redraw: apply a pending restore once the rows are tall enough for it. */
    settle() {
      if (place()) end();
    },
    flush,
    /** Shows the rows at `px` for another history entry: no wait, the rows it was left on are the ones it names. */
    show(px: number) {
      end();
      const p = panel();
      if (px <= 0) {
        if (p !== null) p.scrollTop = 0;
        return;
      }
      pending = px;
      if (place()) end();
      else deadline = setTimeout(giveUp, RESTORE_WAIT_MS);
    },
    stop() {
      end();
      if (timer !== undefined) clearTimeout(timer);
      container.removeEventListener?.('scroll', onScroll, { capture: true });
      for (const [type, fn] of inputs) container.removeEventListener?.(type, fn, { capture: true });
    },
  };
}

/** The name of the view's saved state (PRD R16): the key is `vh.view.v1.timeline`. */
export const TIMELINE_VIEW = 'timeline';

/** The filter the view opens with, and whether it came from the saved query (a bare route) rather than a link's own query. */
export interface OpenedTimeline {
  filter: TimelineFilter;
  restored: boolean;
  /** The window the fragment names (`from`/`to`), when it names a usable one. */
  window?: TimeWindow;
  /** The task the fragment previews over the chart (`task`), when it names one. */
  task?: string;
}

/**
 * The query keys that are the view's state rather than its filter: the window and the previewed task. They go in the
 * fragment (a link or Back shows the same range) but are never saved.
 */
const VIEW_KEYS = ['from', 'to', 'task'] as const;

/** The longest task ref the fragment's `task` is taken for: anything longer is not a ref. */
const TASK_REF_MAX = 128;

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

/** The task a query previews, or undefined when `task` is missing, blank or too long to be a ref. */
export function taskFromQuery(query: string): string | undefined {
  const task = new URLSearchParams(query).get('task')?.trim() ?? '';
  return task === '' || task.length > TASK_REF_MAX ? undefined : task;
}

/** A query without the window's and the task's keys: what the filter is, and what is saved. */
export function withoutWindow(query: string): string {
  const params = new URLSearchParams(query);
  for (const key of VIEW_KEYS) params.delete(key);
  return params.toString();
}

/**
 * The fragment of the view: the filter's query, then the window and the previewed task when there are
 * (`#/timeline?tag=x&from=…&to=…&task=…`).
 */
export function timelineViewHash(filter: TimelineFilter, window?: TimeWindow, task?: string): string {
  const params = new URLSearchParams(filterToQuery(filter));
  if (window !== undefined) {
    params.set('from', windowTime(window.from));
    params.set('to', windowTime(window.to));
  }
  if (task !== undefined) params.set('task', task);
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
  const task = taskFromQuery(full);
  const named = { ...(window === undefined ? {} : { window }), ...(task === undefined ? {} : { task }) };
  // A fragment that names a window or a preview describes the whole view (a link, or Back to this entry): its filter,
  // even none, is the one shown, and nothing saved replaces it.
  if ((window !== undefined || task !== undefined) && own === '') return { filter: filterFromQuery(''), restored: false, ...named };
  const query = openView(env, TIMELINE_VIEW, '#/timeline', own);
  return { filter: filterFromQuery(query), restored: own === '' && query !== '', ...named };
}

/** Saves the filter's query after a change (the empty filter is the default and removes the key). */
export function saveTimeline(storage: ViewStorage | undefined, filter: TimelineFilter): void {
  saveViewQuery(storage, TIMELINE_VIEW, filterToQuery(filter));
}

/** Clear filters: forgets the saved query. */
export function forgetTimeline(storage: ViewStorage | undefined): void {
  clearViewQuery(storage, TIMELINE_VIEW);
}

const NO_TAGS: ReadonlySet<string> = new Set();

/**
 * A restored filter without the initiatives and tags the board no longer has: `known` is the list of initiatives with
 * their tags, and `carried` the restored tags some task carries (its own or a derived actor kind; `carriedTags` in
 * lib/tagCarry.ts), which are valid too. The actor and the closed-within choice are kept. A filter with nothing to drop is returned as it is (the
 * same object).
 */
export function dropUnknown(
  filter: TimelineFilter,
  known: readonly { slug: string; tags: readonly string[] }[],
  carried: ReadonlySet<string> = NO_TAGS,
): TimelineFilter {
  const slugs = new Set(known.map((i) => i.slug));
  const tags = new Set([...known.flatMap((i) => i.tags), ...carried]);
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
export function startFilter(
  opened: OpenedTimeline,
  known: readonly { slug: string; tags: readonly string[] }[] | undefined,
  carried: ReadonlySet<string> = NO_TAGS,
): TimelineFilter {
  if (!opened.restored || known === undefined) return opened.filter;
  return dropUnknown(opened.filter, known, carried);
}
