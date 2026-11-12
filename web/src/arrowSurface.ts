// The DOM side of the Timeline view's dependency arrows (see lib/timelineArrows.ts): an SVG laid over vis-timeline's
// centre panel, which clips it to the rows and the time axis. Bars are found by the class `itemClass` gave them.
import { itemClass } from './lib/timeline';
import type { ArrowPath, ArrowSurface, Box } from './lib/timelineArrows';

const SVG = 'http://www.w3.org/2000/svg';

/** The surface over the centre panel of the chart in `root`; `undefined` while vis-timeline has not built it. */
export function domSurface(root: HTMLElement): ArrowSurface | undefined {
  const center = root.querySelector<HTMLElement>('.vis-panel.vis-center');
  if (center === null) return undefined;
  let svg = center.querySelector<SVGSVGElement>(':scope > svg.tl-arrows');
  if (svg === null) {
    svg = document.createElementNS(SVG, 'svg');
    svg.setAttribute('class', 'tl-arrows');
    svg.setAttribute('aria-hidden', 'true');
    svg.setAttribute('data-testid', 'timeline-arrows');
    svg.innerHTML =
      '<defs><marker id="tl-arrowhead" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto">' +
      '<path d="M 0 0 L 10 5 L 0 10 z" class="tl-arrowhead"/></marker></defs>';
    center.appendChild(svg);
  }
  const layer = svg;
  return {
    boxOf(itemId: string): Box | undefined {
      const bar = center.querySelector<HTMLElement>(`.vis-item.${itemClass(itemId)}`);
      if (bar === null) return undefined;
      const at = center.getBoundingClientRect();
      const r = bar.getBoundingClientRect();
      return { left: r.left - at.left, top: r.top - at.top, right: r.right - at.left, bottom: r.bottom - at.top };
    },
    render(paths: readonly ArrowPath[]) {
      for (const old of layer.querySelectorAll('path.tl-arrow')) old.remove();
      for (const p of paths) {
        const el = document.createElementNS(SVG, 'path');
        el.setAttribute('class', 'tl-arrow');
        el.setAttribute('d', p.d);
        el.setAttribute('data-from', p.from);
        el.setAttribute('data-to', p.to);
        el.setAttribute('marker-end', 'url(#tl-arrowhead)');
        layer.appendChild(el);
      }
    },
  };
}
