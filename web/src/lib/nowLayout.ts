// The dock layout of the Now view on a wide screen (PRD R4, SPEC 11): which widget sits in which of 1 to 4 columns,
// the column widths (each at least 18%), the band heights and the band open states, with the default, the
// is-default check Reset layout hangs on, and the moves a pointer drag or an arrow key makes. Pure functions: the
// components keep the layout in state and save it under `vh.now.layout.v1`. A stored value that does not parse, or
// that breaks the model, is replaced by the default.
import type { BandKey } from './nowBands';

/** The Needs me bands and the parts of the In flight column, each one draggable by its handle. */
export type WidgetKey = BandKey | 'flight' | 'recent';

export const WIDGETS: readonly WidgetKey[] = ['asks', 'expired', 'watches', 'done', 'flight', 'recent'];

export const WIDGET_TITLES: Record<WidgetKey, string> = {
  asks: 'Open asks',
  expired: 'Expired leases and conflicts',
  watches: 'Overdue watches',
  done: 'Done, not closed',
  flight: 'In flight',
  recent: 'Recently touched',
};

export interface DockColumn {
  /** The fraction of the row the column takes; all columns add up to 1. */
  width: number;
  /** The widgets docked in the column, top to bottom. A column may be empty (a place to drop into). */
  widgets: WidgetKey[];
}

export interface NowLayout {
  columns: DockColumn[];
  /** The height in px a person gave a widget; absent means it takes the height of its content. */
  heights: Partial<Record<WidgetKey, number>>;
  /** The open state a person gave a widget; absent means the default for it. */
  open: Partial<Record<WidgetKey, boolean>>;
}

/** The screens the dock is drawn on: lg (1024 px) and wider. Below it the widgets are stacked. */
export const DOCK_QUERY = '(min-width: 1024px)';

export const MIN_COLUMNS = 1;
export const MAX_COLUMNS = 4;
/** No column is narrower than 18% of the row. */
export const MIN_WIDTH = 0.18;
export const MIN_HEIGHT = 80;
export const MAX_HEIGHT = 4000;
/** The versioned storage key of the layout. */
export const LAYOUT_STORAGE_KEY = 'vh.now.layout.v1';

const EPS = 1e-9;
/** How far a width may be from the default and still count as the default (rounding of a drag). */
const DEFAULT_TOLERANCE = 0.005;
/** A stored width is checked exactly: only floating-point noise is forgiven, never a width under 18%. */
const STORED_TOLERANCE = 1e-6;

/** Needs me on the left, In flight on the right, half each, nothing sized and nothing toggled. */
export function defaultLayout(): NowLayout {
  return {
    columns: [
      { width: 0.5, widgets: ['asks', 'expired', 'watches', 'done'] },
      { width: 0.5, widgets: ['flight', 'recent'] },
    ],
    heights: {},
    open: {},
  };
}

/** Reset layout: the default columns, widths, heights and open states. */
export function resetLayout(): NowLayout {
  return defaultLayout();
}

/** Whether a widget is open when the person has not chosen: every one but Done, not closed. */
export function defaultWidgetOpen(key: WidgetKey): boolean {
  return key !== 'done';
}

/** Whether a widget is open under a layout: the person's choice, else the default. */
export function widgetOpen(layout: NowLayout, key: WidgetKey): boolean {
  return layout.open[key] ?? defaultWidgetOpen(key);
}

/** The default: the default columns and widths, no height given and every open state what it is by default. */
export function isDefaultLayout(layout: NowLayout): boolean {
  const base = defaultLayout();
  if (Object.keys(layout.heights).length > 0) return false;
  if (Object.entries(layout.open).some(([key, open]) => open !== defaultWidgetOpen(key as WidgetKey))) return false;
  if (layout.columns.length !== base.columns.length) return false;
  return layout.columns.every(
    (col, i) =>
      Math.abs(col.width - base.columns[i]!.width) < DEFAULT_TOLERANCE && col.widgets.join() === base.columns[i]!.widgets.join(),
  );
}

/**
 * Widths that add up to 1 with none under MIN_WIDTH. The given proportions are kept as far as the minimum allows:
 * a column under it is raised to it, and the others give up the difference in proportion to their size.
 */
export function fitWidths(widths: readonly number[]): number[] {
  const n = widths.length;
  if (n === 0) return [];
  const sum = widths.reduce((a, b) => a + b, 0);
  let out = widths.map((w) => (sum > 0 ? w / sum : 1 / n));
  const pinned = new Set<number>();
  for (;;) {
    const low = out.findIndex((w, i) => !pinned.has(i) && w < MIN_WIDTH - EPS);
    if (low < 0) return out;
    pinned.add(low);
    const free = out.map((_, i) => i).filter((i) => !pinned.has(i));
    const freeSum = free.reduce((a, i) => a + out[i]!, 0);
    const pool = 1 - pinned.size * MIN_WIDTH;
    out = out.map((w, i) => (pinned.has(i) ? MIN_WIDTH : (w / freeSum) * pool));
  }
}

function withWidths(columns: DockColumn[], widths: readonly number[]): DockColumn[] {
  return columns.map((col, i) => ({ ...col, width: widths[i]! }));
}

function cloneColumns(layout: NowLayout): DockColumn[] {
  return layout.columns.map((col) => ({ width: col.width, widgets: [...col.widgets] }));
}

/** Where a widget is: its column and its position in it. */
export function locate(layout: NowLayout, key: WidgetKey): { col: number; index: number } | undefined {
  for (let col = 0; col < layout.columns.length; col++) {
    const index = layout.columns[col]!.widgets.indexOf(key);
    if (index >= 0) return { col, index };
  }
  return undefined;
}

/** Where a drop lands: a position in an existing column, or a new column at one edge. */
export type Drop = { kind: 'col'; col: number; index: number } | { kind: 'new'; side: 'left' | 'right' };

/**
 * Docks a widget at a drop target. Indexes are positions in the target column as it is before the move. A drop
 * that cannot happen (unknown widget or column, a fifth column, a new column from a lone widget already at that
 * edge) returns the layout as it was.
 */
export function moveWidget(layout: NowLayout, key: WidgetKey, drop: Drop): NowLayout {
  const from = locate(layout, key);
  if (from === undefined) return layout;
  return drop.kind === 'new' ? moveToNewColumn(layout, key, from, drop.side) : moveIntoColumn(layout, key, from, drop);
}

function moveToNewColumn(layout: NowLayout, key: WidgetKey, from: { col: number; index: number }, side: 'left' | 'right'): NowLayout {
  const columns = cloneColumns(layout);
  if (columns.length >= MAX_COLUMNS) return layout;
  const source = columns[from.col]!;
  const atEdge = side === 'left' ? from.col === 0 : from.col === columns.length - 1;
  if (source.widgets.length === 1 && atEdge) return layout;
  source.widgets.splice(from.index, 1);
  const fresh: DockColumn = { width: 1 / (columns.length + 1), widgets: [key] };
  const scaled = columns.map((col) => ({ ...col, width: (col.width * columns.length) / (columns.length + 1) }));
  const all = side === 'left' ? [fresh, ...scaled] : [...scaled, fresh];
  return { ...layout, columns: withWidths(all, fitWidths(all.map((c) => c.width))) };
}

function moveIntoColumn(
  layout: NowLayout,
  key: WidgetKey,
  from: { col: number; index: number },
  drop: { col: number; index: number },
): NowLayout {
  const columns = cloneColumns(layout);
  const target = columns[drop.col];
  if (target === undefined) return layout;
  let index = Math.max(0, Math.min(Math.trunc(drop.index), target.widgets.length));
  if (drop.col === from.col && from.index < index) index--;
  columns[from.col]!.widgets.splice(from.index, 1);
  target.widgets.splice(index, 0, key);
  return { ...layout, columns };
}

export type Direction = 'up' | 'down' | 'left' | 'right';

/**
 * The keyboard move: one position. Up and down go one place in the column. Left and right move into the next
 * column, at the same place (or its end when it is shorter); off the edge they open a new column there, while
 * there are fewer than four and the widget is not alone at that edge. A move that cannot happen changes nothing.
 */
export function nudgeWidget(layout: NowLayout, key: WidgetKey, dir: Direction): NowLayout {
  const at = locate(layout, key);
  if (at === undefined) return layout;
  return dir === 'up' || dir === 'down' ? nudgeVertically(layout, key, at, dir) : nudgeSideways(layout, key, at, dir);
}

function nudgeVertically(layout: NowLayout, key: WidgetKey, at: { col: number; index: number }, dir: 'up' | 'down'): NowLayout {
  const size = layout.columns[at.col]!.widgets.length;
  if (dir === 'up') return at.index === 0 ? layout : moveWidget(layout, key, { kind: 'col', col: at.col, index: at.index - 1 });
  return at.index >= size - 1 ? layout : moveWidget(layout, key, { kind: 'col', col: at.col, index: at.index + 2 });
}

function nudgeSideways(layout: NowLayout, key: WidgetKey, at: { col: number; index: number }, dir: 'left' | 'right'): NowLayout {
  const col = at.col + (dir === 'left' ? -1 : 1);
  if (col < 0 || col >= layout.columns.length) return moveWidget(layout, key, { kind: 'new', side: dir });
  return moveWidget(layout, key, { kind: 'col', col, index: Math.min(at.index, layout.columns[col]!.widgets.length) });
}

/** What a focused handle announces after a move: the widget, its column and its place in it. */
export function positionLabel(layout: NowLayout, key: WidgetKey): string {
  const at = locate(layout, key);
  if (at === undefined) return WIDGET_TITLES[key];
  const col = layout.columns[at.col]!;
  return `${WIDGET_TITLES[key]}, column ${at.col + 1} of ${layout.columns.length}, position ${at.index + 1} of ${col.widgets.length}`;
}

/** Opens a new empty column at the right, which takes its share from the others; a fifth is refused. */
export function addColumn(layout: NowLayout): NowLayout {
  const n = layout.columns.length;
  if (n >= MAX_COLUMNS) return layout;
  const columns = [...layout.columns.map((c) => ({ width: (c.width * n) / (n + 1), widgets: [...c.widgets] })), { width: 1 / (n + 1), widgets: [] }];
  return { ...layout, columns: withWidths(columns, fitWidths(columns.map((c) => c.width))) };
}

/**
 * Removes a column: its widgets go to the end of the neighbour on the left (on the right for the first column),
 * which also takes its width. The last column is never removed.
 */
export function removeColumn(layout: NowLayout, col: number): NowLayout {
  const n = layout.columns.length;
  if (n <= MIN_COLUMNS || !Number.isInteger(col) || col < 0 || col >= n) return layout;
  const columns = cloneColumns(layout);
  const [gone] = columns.splice(col, 1);
  const heir = columns[col === 0 ? 0 : col - 1]!;
  heir.widgets.push(...gone!.widgets);
  heir.width += gone!.width;
  return { ...layout, columns: withWidths(columns, fitWidths(columns.map((c) => c.width))) };
}

/** Drops the columns left with no widget (a drag that emptied one); their width goes to a neighbour. */
export function pruneEmpty(layout: NowLayout): NowLayout {
  let out = layout;
  for (let col = out.columns.length - 1; col >= 0; col--) if (out.columns[col]!.widgets.length === 0) out = removeColumn(out, col);
  return out;
}

/** Moves the splitter after column `left` by `delta` (a fraction of the row); both columns keep MIN_WIDTH. */
export function resizeColumns(layout: NowLayout, left: number, delta: number): NowLayout {
  const a = layout.columns[left];
  const b = layout.columns[left + 1];
  if (a === undefined || b === undefined || !Number.isFinite(delta)) return layout;
  const pair = a.width + b.width;
  const wa = Math.min(pair - MIN_WIDTH, Math.max(MIN_WIDTH, a.width + delta));
  return { ...layout, columns: layout.columns.map((c, i) => (i === left ? { ...c, width: wa } : i === left + 1 ? { ...c, width: pair - wa } : c)) };
}

function clampHeight(height: number): number {
  return Math.round(Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, height)));
}

/** Sets a widget's height, kept between MIN_HEIGHT and MAX_HEIGHT; undefined gives it back its content height. */
export function setHeight(layout: NowLayout, key: WidgetKey, height: number | undefined): NowLayout {
  const heights = { ...layout.heights };
  if (height === undefined) delete heights[key];
  else if (Number.isFinite(height)) heights[key] = clampHeight(height);
  else return layout;
  return { ...layout, heights };
}

/** A widget opened or closed by the person. */
export function setOpen(layout: NowLayout, key: WidgetKey, open: boolean): NowLayout {
  return { ...layout, open: { ...layout.open, [key]: open } };
}

const isWidget = (value: unknown): value is WidgetKey => typeof value === 'string' && (WIDGETS as readonly string[]).includes(value);
const isRecord = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);

function validColumn(item: unknown, seen: Set<WidgetKey>): DockColumn | undefined {
  if (!isRecord(item) || !Array.isArray(item.widgets) || typeof item.width !== 'number' || !Number.isFinite(item.width)) return undefined;
  const widgets: WidgetKey[] = [];
  for (const key of item.widgets) {
    if (!isWidget(key) || seen.has(key)) return undefined;
    seen.add(key);
    widgets.push(key);
  }
  return { width: item.width, widgets };
}

function validColumns(raw: unknown): DockColumn[] | undefined {
  if (!Array.isArray(raw) || raw.length < MIN_COLUMNS || raw.length > MAX_COLUMNS) return undefined;
  const seen = new Set<WidgetKey>();
  const columns: DockColumn[] = [];
  for (const item of raw) {
    const column = validColumn(item, seen);
    if (column === undefined) return undefined;
    columns.push(column);
  }
  return seen.size === WIDGETS.length && widthsHold(columns) ? columns : undefined;
}

/** Stored widths add up to 1 (floating-point noise aside) and none is under MIN_WIDTH. */
function widthsHold(columns: readonly DockColumn[]): boolean {
  const sum = columns.reduce((a, c) => a + c.width, 0);
  return Math.abs(sum - 1) <= STORED_TOLERANCE && columns.every((c) => c.width >= MIN_WIDTH - STORED_TOLERANCE);
}

function validRecord<T>(raw: unknown, ok: (value: unknown) => value is T): Partial<Record<WidgetKey, T>> | undefined {
  if (raw === undefined) return {};
  if (!isRecord(raw)) return undefined;
  const out: Partial<Record<WidgetKey, T>> = {};
  for (const [key, value] of Object.entries(raw)) {
    if (!isWidget(key) || !ok(value)) return undefined;
    out[key] = value;
  }
  return out;
}

const isHeight = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v) && v >= MIN_HEIGHT && v <= MAX_HEIGHT;
const isBool = (v: unknown): v is boolean => typeof v === 'boolean';

/**
 * A stored value as a layout. It must be JSON of the shape written: 1 to 4 columns holding every widget exactly
 * once, widths adding up to 1 with none under 18%, heights in range, open states booleans. Anything else (not
 * JSON, a missing field, a widget twice, a fifth column, a width of 5%) is the default; nothing is repaired.
 */
export function parseLayout(raw: string | null | undefined): NowLayout {
  if (raw === null || raw === undefined) return defaultLayout();
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return defaultLayout();
  }
  if (!isRecord(value)) return defaultLayout();
  const columns = validColumns(value.columns);
  const heights = validRecord(value.heights, isHeight);
  const open = validRecord(value.open, isBool);
  if (columns === undefined || heights === undefined || open === undefined) return defaultLayout();
  return { columns: withWidths(columns, fitWidths(columns.map((c) => c.width))), heights, open };
}

/** The slice of `Storage` the layout uses. */
export type LayoutStorage = Pick<Storage, 'getItem' | 'setItem'>;

export function loadLayout(storage: LayoutStorage | undefined): NowLayout {
  try {
    return parseLayout(storage?.getItem(LAYOUT_STORAGE_KEY));
  } catch {
    return defaultLayout(); // storage can be blocked
  }
}

export function saveLayout(storage: LayoutStorage | undefined, layout: NowLayout): void {
  try {
    storage?.setItem(LAYOUT_STORAGE_KEY, JSON.stringify(layout));
  } catch {
    // The layout still applies to this page view.
  }
}
