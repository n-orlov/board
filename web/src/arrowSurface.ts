// The DOM side of the Timeline view's dependency arrows (see lib/timelineArrows.ts): an SVG laid over vis-timeline's
// centre panel, which clips it to the rows and the time axis. Bars are found by the class `itemClass` gave them.
import { itemClass } from './lib/timeline';
import type { ArrowPath, ArrowSurface, Box } from './lib/timelineArrows';

const SVG = 'http://www.w3.org/2000/svg';

const ARROWHEAD =
  '<defs><marker id="tl-arrowhead" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto">' +
  '<path d="M 0 0 L 10 5 L 0 10 z" class="tl-arrowhead"/></marker></defs>';

/** The overlay of `center`, made on first use. */
function layerOf(center: HTMLElement): SVGSVGElement {
  return center.querySelector<SVGSVGElement>(':scope > svg.tl-arrows') ?? addLayer(center);
}

function addLayer(center: HTMLElement): SVGSVGElement {
  const svg = document.createElementNS(SVG, 'svg');
  svg.setAttribute('class', 'tl-arrows');
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('data-testid', 'timeline-arrows');
  svg.innerHTML = ARROWHEAD;
  center.appendChild(svg);
  return svg;
}

/** The box of a bar relative to the centre panel, `undefined` while the bar is not drawn. */
function boxIn(center: HTMLElement, itemId: string): Box | undefined {
  const bar = center.querySelector<HTMLElement>(`.vis-item.${itemClass(itemId)}`);
  return bar === null ? undefined : relativeBox(bar.getBoundingClientRect(), center.getBoundingClientRect());
}

function relativeBox(r: DOMRect, at: DOMRect): Box {
  return { left: r.left - at.left, top: r.top - at.top, right: r.right - at.left, bottom: r.bottom - at.top };
}

function appendArrow(layer: SVGSVGElement, p: ArrowPath): void {
  const el = document.createElementNS(SVG, 'path');
  el.setAttribute('class', 'tl-arrow');
  el.setAttribute('d', p.d);
  el.setAttribute('data-from', p.from);
  el.setAttribute('data-to', p.to);
  el.setAttribute('marker-end', 'url(#tl-arrowhead)');
  layer.appendChild(el);
}

function renderArrows(layer: SVGSVGElement, paths: readonly ArrowPath[]): void {
  layer.querySelectorAll('path.tl-arrow').forEach((old) => old.remove());
  paths.forEach((p) => appendArrow(layer, p));
}

/** The surface over the centre panel of the chart in `root`; `undefined` while vis-timeline has not built it. */
export function domSurface(root: HTMLElement): ArrowSurface | undefined {
  const center = root.querySelector<HTMLElement>('.vis-panel.vis-center');
  if (center === null) return undefined;
  const layer = layerOf(center);
  return { boxOf: (itemId) => boxIn(center, itemId), render: (paths) => renderArrows(layer, paths) };
}
