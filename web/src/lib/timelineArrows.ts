// The Timeline view's dependency arrows (PRD R6, SPEC 11): one SVG path per `deps` pair, from the right edge of the
// blocking task's bar to the left edge of the dependent task's bar. vis-timeline draws no arrows, so the view lays an
// SVG over the chart and redraws it after every vis redraw (zoom, pan, collapse, new data). This module computes the
// paths; an `ArrowSurface` (the DOM side, in arrowSurface.ts) measures the bars and holds the SVG.
import type { DependencyPair } from './timeline';

/** A rectangle in the arrow layer's own coordinates. */
export interface Box {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

export interface ArrowPath {
  /** The dependency pair's id. */
  id: string;
  /** The blocking and the blocked task's refs. */
  from: string;
  to: string;
  /** The SVG path data. */
  d: string;
}

/** What the arrows are drawn on: it measures the bars vis-timeline drew and holds the SVG. */
export interface ArrowSurface {
  /** The box of the bar of an item id, `undefined` when vis has not drawn it (collapsed row, outside the window). */
  boxOf(itemId: string): Box | undefined;
  /** Replaces the arrows drawn. */
  render(paths: readonly ArrowPath[]): void;
}

/** The horizontal run of an arrow's curve at each end, so it leaves and enters a bar sideways. */
export const MIN_BEND = 24;

function middle(box: Box): number {
  return (box.top + box.bottom) / 2;
}

/** The curve from the right edge of `from` to the left edge of `to`, both at mid-height. */
export function arrowPath(from: Box, to: Box): string {
  const x1 = from.right;
  const y1 = middle(from);
  const x2 = to.left;
  const y2 = middle(to);
  const bend = Math.max(MIN_BEND, Math.abs(x2 - x1) / 2);
  return `M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`;
}

/** The paths of the pairs whose two bars are both drawn. */
export function arrowPaths(pairs: readonly DependencyPair[], boxOf: (itemId: string) => Box | undefined): ArrowPath[] {
  return pairs.flatMap((pair) => {
    const from = boxOf(pair.fromItem);
    const to = boxOf(pair.toItem);
    return from === undefined || to === undefined ? [] : [{ id: pair.id, from: pair.from, to: pair.to, d: arrowPath(from, to) }];
  });
}

/** Draws the arrows of `pairs` on `surface`. */
export function drawArrows(surface: ArrowSurface, pairs: readonly DependencyPair[]): void {
  surface.render(arrowPaths(pairs, (id) => surface.boxOf(id)));
}
