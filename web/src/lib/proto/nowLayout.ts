// UX prototype: the Now page's dock layout. Columns, each with a width fraction and an ordered list of
// widgets, plus an optional max height per widget. Pure functions only; Now.svelte keeps it in state and
// saves it with savePref('now.layout').

export type WidgetKey = 'asks' | 'stuck' | 'overdue' | 'done' | 'flight' | 'recent';

export const WIDGETS: readonly WidgetKey[] = ['asks', 'stuck', 'overdue', 'done', 'flight', 'recent'];

export const WIDGET_TITLES: Record<WidgetKey, string> = {
  asks: 'Open asks',
  stuck: 'Expired leases & conflicts',
  overdue: 'Overdue watches',
  done: 'Done, not closed',
  flight: 'Live leases',
  recent: 'Recently touched',
};

export interface DockColumn {
  /** Fraction of the row (all columns sum to 1). */
  width: number;
  widgets: WidgetKey[];
}

export interface NowLayout {
  columns: DockColumn[];
  /** Max body height in px; absent means auto. */
  heights: Partial<Record<WidgetKey, number>>;
}

export type Drop = { kind: 'col'; col: number; index: number } | { kind: 'new'; side: 'left' | 'right' };

export const MAX_COLUMNS = 4;
export const MIN_WIDTH = 0.18;
export const MIN_HEIGHT = 80;
export const MAX_HEIGHT = 4000;

/** Today's split: Needs me on the left, In flight on the right, 50/50. */
export function defaultLayout(): NowLayout {
  return {
    columns: [
      { width: 0.5, widgets: ['asks', 'stuck', 'overdue', 'done'] },
      { width: 0.5, widgets: ['flight', 'recent'] },
    ],
    heights: {},
  };
}

const DEFAULT_COLUMN: Record<WidgetKey, number> = { asks: 0, stuck: 0, overdue: 0, done: 0, flight: 1, recent: 1 };

const isWidget = (v: unknown): v is WidgetKey => typeof v === 'string' && (WIDGETS as readonly string[]).includes(v);

/** Widths summing to 1; equal widths when any would fall under the minimum. */
function fixWidths(columns: DockColumn[]): DockColumn[] {
  if (columns.length === 0) return columns;
  const sum = columns.reduce((n, c) => n + c.width, 0);
  let widths = columns.map((c) => (sum > 0 ? c.width / sum : 1 / columns.length));
  if (widths.some((w) => !(w >= MIN_WIDTH - 1e-9))) widths = columns.map(() => 1 / columns.length);
  return columns.map((c, i) => ({ ...c, width: widths[i]! }));
}

/**
 * A layout that can be trusted: every widget exactly once, no empty columns, at most MAX_COLUMNS, widths
 * summing to 1 and none under MIN_WIDTH, heights in range. Anything unreadable gives the default.
 */
export function normalizeLayout(raw: unknown): NowLayout {
  if (typeof raw !== 'object' || raw === null || !Array.isArray((raw as { columns?: unknown }).columns)) return defaultLayout();
  const seen = new Set<WidgetKey>();
  let columns: DockColumn[] = [];
  for (const c of (raw as { columns: unknown[] }).columns) {
    if (typeof c !== 'object' || c === null) continue;
    const list = (c as { widgets?: unknown }).widgets;
    const w = (c as { width?: unknown }).width;
    const widgets = (Array.isArray(list) ? list : []).filter((k): k is WidgetKey => isWidget(k) && !seen.has(k) && (seen.add(k), true));
    if (widgets.length === 0) continue;
    columns.push({ width: typeof w === 'number' && Number.isFinite(w) && w > 0 ? w : 1, widgets });
  }
  if (columns.length === 0) return defaultLayout();
  while (columns.length > MAX_COLUMNS) {
    const extra = columns.pop()!;
    columns[columns.length - 1]!.widgets.push(...extra.widgets);
  }
  for (const k of WIDGETS) {
    if (seen.has(k)) continue;
    columns[Math.min(DEFAULT_COLUMN[k], columns.length - 1)]!.widgets.push(k);
  }
  columns = fixWidths(columns);
  const heights: Partial<Record<WidgetKey, number>> = {};
  const rawHeights = (raw as { heights?: unknown }).heights;
  if (typeof rawHeights === 'object' && rawHeights !== null) {
    for (const [k, v] of Object.entries(rawHeights)) {
      if (isWidget(k) && typeof v === 'number' && Number.isFinite(v)) heights[k] = Math.round(Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, v)));
    }
  }
  return { columns, heights };
}

/** Where a widget is: its column and index, or undefined. */
export function locate(layout: NowLayout, key: WidgetKey): { col: number; index: number } | undefined {
  for (let col = 0; col < layout.columns.length; col++) {
    const index = layout.columns[col]!.widgets.indexOf(key);
    if (index >= 0) return { col, index };
  }
  return undefined;
}

/** Moves a widget to a drop target. Indexes are in the target column as it is before the move. */
export function moveWidget(layout: NowLayout, key: WidgetKey, drop: Drop): NowLayout {
  const from = locate(layout, key);
  if (from === undefined) return layout;
  let columns = layout.columns.map((c) => ({ width: c.width, widgets: [...c.widgets] }));
  if (drop.kind === 'new') {
    if (columns.length >= MAX_COLUMNS) return layout;
    if (columns[from.col]!.widgets.length === 1 && (drop.side === 'left' ? from.col === 0 : from.col === columns.length - 1)) return layout;
    columns[from.col]!.widgets.splice(from.index, 1);
    const n = columns.length;
    columns = columns.map((c) => ({ ...c, width: (c.width * n) / (n + 1) }));
    const fresh = { width: 1 / (n + 1), widgets: [key] };
    if (drop.side === 'left') columns.unshift(fresh);
    else columns.push(fresh);
  } else {
    const target = columns[drop.col];
    if (target === undefined) return layout;
    let index = Math.max(0, Math.min(drop.index, target.widgets.length));
    if (drop.col === from.col && from.index < index) index--;
    columns[from.col]!.widgets.splice(from.index, 1);
    target.widgets.splice(index, 0, key);
  }
  return { columns: fixWidths(columns.filter((c) => c.widgets.length > 0)), heights: { ...layout.heights } };
}

/** Moves a widget one step for the keyboard: up/down in its column, left/right into the next column's end. */
export function nudgeWidget(layout: NowLayout, key: WidgetKey, dir: 'up' | 'down' | 'left' | 'right'): NowLayout {
  const at = locate(layout, key);
  if (at === undefined) return layout;
  if (dir === 'up') return at.index === 0 ? layout : moveWidget(layout, key, { kind: 'col', col: at.col, index: at.index - 1 });
  if (dir === 'down') return moveWidget(layout, key, { kind: 'col', col: at.col, index: at.index + 2 });
  const col = at.col + (dir === 'left' ? -1 : 1);
  if (col < 0 || col >= layout.columns.length) return moveWidget(layout, key, { kind: 'new', side: dir });
  return moveWidget(layout, key, { kind: 'col', col, index: layout.columns[col]!.widgets.length });
}

/** Moves the splitter after column `left` by `delta` (a fraction of the row); both columns keep MIN_WIDTH. */
export function resizeColumns(layout: NowLayout, left: number, delta: number): NowLayout {
  const a = layout.columns[left];
  const b = layout.columns[left + 1];
  if (a === undefined || b === undefined) return layout;
  const pair = a.width + b.width;
  const wa = Math.min(pair - MIN_WIDTH, Math.max(MIN_WIDTH, a.width + delta));
  const columns = layout.columns.map((c, i) => (i === left ? { ...c, width: wa } : i === left + 1 ? { ...c, width: pair - wa } : c));
  return { ...layout, columns };
}

/** Sets or clears (undefined) a widget's max height. */
export function setHeight(layout: NowLayout, key: WidgetKey, height: number | undefined): NowLayout {
  const heights = { ...layout.heights };
  if (height === undefined) delete heights[key];
  else heights[key] = Math.round(Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, height)));
  return { ...layout, heights };
}

export function isDefaultLayout(layout: NowLayout): boolean {
  const d = defaultLayout();
  if (Object.keys(layout.heights).length > 0 || layout.columns.length !== d.columns.length) return false;
  return layout.columns.every((c, i) => Math.abs(c.width - d.columns[i]!.width) < 0.005 && c.widgets.join() === d.columns[i]!.widgets.join());
}
