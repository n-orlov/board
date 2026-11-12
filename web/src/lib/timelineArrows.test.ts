import { describe, expect, it } from 'vitest';
import { arrowPath, arrowPaths, drawArrows, MIN_BEND, type ArrowPath, type ArrowSurface, type Box } from './timelineArrows';
import type { DependencyPair } from './timeline';

const box = (left: number, top: number, right: number, bottom: number): Box => ({ left, top, right, bottom });
const pair = (from: string, to: string): DependencyPair => ({ id: `dep:${from}>${to}`, from, fromItem: `act:${from}`, to, toItem: `act:${to}` });

describe('arrowPath', () => {
  it('starts at the right edge of the blocker and ends at the left edge of the dependent, at mid-height', () => {
    expect(arrowPath(box(10, 0, 110, 20), box(300, 40, 400, 60))).toBe('M 110 10 C 205 10, 205 50, 300 50');
  });

  it('bends at least MIN_BEND when the bars are close or the dependent starts before the blocker ends', () => {
    expect(arrowPath(box(0, 0, 100, 10), box(110, 30, 200, 40))).toBe(`M 100 5 C ${100 + MIN_BEND} 5, ${110 - MIN_BEND} 35, 110 35`);
    expect(arrowPath(box(0, 0, 100, 10), box(50, 30, 200, 40))).toBe('M 100 5 C 125 5, 25 35, 50 35');
  });
});

describe('arrowPaths', () => {
  const boxes = new Map<string, Box>([
    ['act:a', box(0, 0, 10, 10)],
    ['act:b', box(50, 20, 60, 30)],
    ['act:c', box(90, 40, 100, 50)],
  ]);
  const boxOf = (id: string) => boxes.get(id);

  it('makes one path per pair whose bars are both drawn, in the order of the pairs', () => {
    const paths = arrowPaths([pair('a', 'b'), pair('b', 'c')], boxOf);
    expect(paths.map((p) => p.id)).toEqual(['dep:a>b', 'dep:b>c']);
    expect(paths[0]).toEqual({ id: 'dep:a>b', from: 'a', to: 'b', d: arrowPath(box(0, 0, 10, 10), box(50, 20, 60, 30)) });
  });

  it('leaves out a pair with a bar that is not drawn, at either end', () => {
    expect(arrowPaths([pair('a', 'x'), pair('x', 'b'), pair('a', 'c')], boxOf).map((p) => p.id)).toEqual(['dep:a>c']);
    expect(arrowPaths([], boxOf)).toEqual([]);
  });
});

describe('drawArrows', () => {
  it('measures through the surface and renders what it got', () => {
    const drawn: ArrowPath[][] = [];
    const surface: ArrowSurface = {
      boxOf: (id) => (id === 'act:a' ? box(0, 0, 10, 10) : id === 'act:b' ? box(20, 0, 30, 10) : undefined),
      render: (paths) => drawn.push([...paths]),
    };
    drawArrows(surface, [pair('a', 'b'), pair('a', 'z')]);
    expect(drawn).toHaveLength(1);
    expect(drawn[0]!.map((p) => p.id)).toEqual(['dep:a>b']);
  });
});
