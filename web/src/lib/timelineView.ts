// The Timeline view's data layer and its vis-timeline glue (PRD R6, SPEC 11): `TimelineApi` reads `GET /timeline` for
// the view's filter and window, `TimelineViewStore` holds the mapped model (lib/timeline.ts) the view draws, and
// `mountChart` hands that model to vis-timeline. vis-timeline is bundled with the app (an npm dependency, no CDN); the
// view imports its classes and passes them in as `ChartLib`, so this module stays testable without a DOM.
import { errorText, Fetches } from './fetches';
import { ThreadApi } from './threads';
import { apiQuery, buildTimeline, type Timeline, type TimelineFilter, type TimelineModel } from './timeline';

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

export class TimelineApi extends ThreadApi {
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

const EMPTY_MODEL: TimelineModel = { groups: [], items: [], dependencies: [] };

export class TimelineViewStore {
  private readonly api: Pick<TimelineApi, 'read'>;
  private readonly publish: (state: TimelineViewState) => void;
  private readonly now: () => Date;
  private readonly fetches = new Fetches();
  state: TimelineViewState;

  constructor(api: Pick<TimelineApi, 'read'>, publish: (state: TimelineViewState) => void, filter: TimelineFilter, now: () => Date = () => new Date()) {
    this.api = api;
    this.publish = publish;
    this.now = now;
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

  /** Applies a new filter and reads again. */
  setFilter(filter: TimelineFilter): Promise<void> {
    this.set({ filter });
    return this.load();
  }
}

// --- vis-timeline ---

/** The slice of vis-data's DataSet the chart uses. */
export interface ChartData<T> {
  clear(): void;
  add(rows: T[]): unknown;
}

/** The slice of vis-timeline's Timeline the chart uses. */
export interface ChartTimeline {
  destroy(): void;
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
  /** The chart grows with its rows; the frame around it scrolls. */
  verticalScroll: boolean;
  maxHeight: string;
}

/** The vis-timeline options of the view for a window. */
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
    verticalScroll: true,
    maxHeight: '70vh',
  };
}

/** A mounted chart: `show` replaces what it draws, `destroy` takes it off the page. */
export interface Chart {
  show(model: TimelineModel): void;
  destroy(): void;
}

/** Draws `model` with vis-timeline into `container`. */
export function mountChart(lib: ChartLib, container: HTMLElement, model: TimelineModel, window: TimeWindow): Chart {
  const items = new lib.DataSet<unknown>(model.items);
  const groups = new lib.DataSet<unknown>(model.groups);
  const timeline = new lib.Timeline(container, items, groups, chartOptions(window));
  return {
    show(next: TimelineModel) {
      items.clear();
      groups.clear();
      groups.add(next.groups);
      items.add(next.items);
    },
    destroy() {
      timeline.destroy();
    },
  };
}
