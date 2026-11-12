// The Timeline view's data layer and its vis-timeline glue (PRD R6, SPEC 11): `TimelineApi` reads `GET /timeline` for
// the view's filter and window, `TimelineViewStore` holds the mapped model (lib/timeline.ts) the view draws, and
// `mountChart` hands that model to vis-timeline. vis-timeline is bundled with the app (an npm dependency, no CDN); the
// view imports its classes and passes them in as `ChartLib`, so this module stays testable without a DOM.
import { errorText, Fetches } from './fetches';
import { ThreadApi } from './threads';
import { apiQuery, buildTimeline, taskOfGroup, type Timeline, type TimelineFilter, type TimelineItem, type TimelineModel } from './timeline';
import { drawArrows, type ArrowSurface } from './timelineArrows';

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
  ) {
    this.api = api;
    this.publish = publish;
    this.now = now;
    this.timers = timers;
    this.state = { filter, window: initialWindow(now()), model: EMPTY_MODEL, loaded: false, loading: false, error: '' };
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
  stack: boolean;
  stackSubgroups: boolean;
  groupOrder: 'order';
  selectable: boolean;
}

/** The vis-timeline options of the view for a window. The wheel zooms (vis-timeline's default), a pinch zooms, a drag pans. */
export function chartOptions(window: TimeWindow): ChartOptions {
  return {
    start: window.from,
    end: window.to,
    zoomMin: ZOOM_MIN_MS,
    zoomMax: ZOOM_MAX_MS,
    orientation: 'top',
    stack: false,
    stackSubgroups: true,
    groupOrder: 'order',
    selectable: false,
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
}

/** A mounted chart: `show` replaces what it draws, `zoom` steps the range, `destroy` takes it off the page. */
export interface Chart {
  show(model: TimelineModel): void;
  zoom(direction: 'in' | 'out'): void;
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
export function mountChart(lib: ChartLib, container: HTMLElement, model: TimelineModel, window: TimeWindow, hooks: ChartHooks): Chart {
  const items = new lib.DataSet<unknown>(model.items);
  const groups = new lib.DataSet<unknown>(model.groups);
  const timeline = new lib.Timeline(container, items, groups, chartOptions(window));
  let current = model;
  const heldGroups: Held = new Map(model.groups.map((g) => [g.id, JSON.stringify(g)]));
  const heldItems: Held = new Map(model.items.map((i) => [i.id, JSON.stringify(i)]));
  const byId = (id: string) => current.items.find((item) => item.id === id);
  timeline.on('click', (click) => {
    const ref = clickedTask(click, byId);
    if (ref !== undefined) hooks.open(ref);
  });
  // vis-timeline fires `changed` after a redraw (a collapse, new data) and `rangechange` and `rangechanged` while the
  // range moves (a zoom, a pan). The span readout and the arrows follow all three.
  const sync = () => {
    hooks.onSpan(spanOf(timeline.getWindow()));
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
    destroy() {
      timeline.destroy();
    },
  };
}
