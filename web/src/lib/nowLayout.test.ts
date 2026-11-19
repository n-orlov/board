import { describe, expect, it } from 'vitest';
import {
  LAYOUT_STORAGE_KEY,
  MAX_COLUMNS,
  MAX_HEIGHT,
  MIN_HEIGHT,
  MIN_WIDTH,
  WIDGETS,
  addColumn,
  defaultLayout,
  fitWidths,
  isDefaultLayout,
  loadLayout,
  locate,
  moveWidget,
  nudgeWidget,
  parseLayout,
  positionLabel,
  removeColumn,
  resetLayout,
  resizeColumns,
  saveLayout,
  setHeight,
  setOpen,
  type NowLayout,
  type WidgetKey,
} from './nowLayout';

const widths = (l: NowLayout) => l.columns.map((c) => c.width);
const sum = (l: NowLayout) => widths(l).reduce((a, b) => a + b, 0);
const names = (l: NowLayout) => l.columns.map((c) => c.widgets.join(','));
const store = (initial?: string) => {
  const data = new Map<string, string>();
  if (initial !== undefined) data.set(LAYOUT_STORAGE_KEY, initial);
  return { data, getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => void data.set(k, v) };
};

describe('the default layout', () => {
  it('is Needs me on the left and In flight on the right, half each, with every widget once', () => {
    const d = defaultLayout();
    expect(names(d)).toEqual(['asks,expired,watches,done', 'flight,recent']);
    expect(widths(d)).toEqual([0.5, 0.5]);
    expect(d.columns.flatMap((c) => c.widgets).sort()).toEqual([...WIDGETS].sort());
    expect(d.heights).toEqual({});
    expect(d.open).toEqual({});
    expect(isDefaultLayout(d)).toBe(true);
  });

  it('is told apart from a changed layout by columns, order, widths, heights and open states', () => {
    const d = defaultLayout();
    expect(isDefaultLayout(moveWidget(d, 'done', { kind: 'col', col: 1, index: 0 }))).toBe(false);
    expect(isDefaultLayout(nudgeWidget(d, 'asks', 'down'))).toBe(false);
    expect(isDefaultLayout(resizeColumns(d, 0, 0.1))).toBe(false);
    expect(isDefaultLayout(setHeight(d, 'asks', 300))).toBe(false);
    expect(isDefaultLayout(setOpen(d, 'done', true))).toBe(false);
    expect(isDefaultLayout(addColumn(d))).toBe(false);
  });

  it('counts a layout dragged back to the default widths as the default', () => {
    expect(isDefaultLayout(resizeColumns(resizeColumns(defaultLayout(), 0, 0.1), 0, -0.1))).toBe(true);
  });
});

describe('docking', () => {
  it('docks a widget into another column at a position', () => {
    const l = moveWidget(defaultLayout(), 'asks', { kind: 'col', col: 1, index: 1 });
    expect(names(l)).toEqual(['expired,watches,done', 'flight,asks,recent']);
    expect(locate(l, 'asks')).toEqual({ col: 1, index: 1 });
  });

  it('docks at the top and the end of another column, clamping an index out of range', () => {
    const d = defaultLayout();
    expect(names(moveWidget(d, 'done', { kind: 'col', col: 1, index: 0 }))[1]).toBe('done,flight,recent');
    expect(names(moveWidget(d, 'done', { kind: 'col', col: 1, index: 99 }))[1]).toBe('flight,recent,done');
    expect(names(moveWidget(d, 'done', { kind: 'col', col: 1, index: -5 }))[1]).toBe('done,flight,recent');
  });

  it('reorders within a column, the index counting the column as it was before the move', () => {
    const d = defaultLayout();
    expect(names(moveWidget(d, 'asks', { kind: 'col', col: 0, index: 3 }))[0]).toBe('expired,watches,asks,done');
    expect(names(moveWidget(d, 'done', { kind: 'col', col: 0, index: 0 }))[0]).toBe('done,asks,expired,watches');
  });

  it('keeps every widget exactly once and never changes the widths when docking', () => {
    const l = moveWidget(defaultLayout(), 'flight', { kind: 'col', col: 0, index: 2 });
    expect(l.columns.flatMap((c) => c.widgets).sort()).toEqual([...WIDGETS].sort());
    expect(widths(l)).toEqual([0.5, 0.5]);
  });

  it('keeps a column that is left empty (it stays a place to drop into)', () => {
    let l = defaultLayout();
    l = moveWidget(l, 'flight', { kind: 'col', col: 0, index: 0 });
    l = moveWidget(l, 'recent', { kind: 'col', col: 0, index: 0 });
    expect(names(l)).toEqual(['recent,flight,asks,expired,watches,done', '']);
    expect(l.columns).toHaveLength(2);
  });

  it('opens a new column at an edge when a widget is dropped there', () => {
    const left = moveWidget(defaultLayout(), 'recent', { kind: 'new', side: 'left' });
    expect(names(left)).toEqual(['recent', 'asks,expired,watches,done', 'flight']);
    const right = moveWidget(defaultLayout(), 'asks', { kind: 'new', side: 'right' });
    expect(names(right)).toEqual(['expired,watches,done', 'flight,recent', 'asks']);
    for (const l of [left, right]) {
      expect(sum(l)).toBeCloseTo(1, 9);
      expect(Math.min(...widths(l))).toBeGreaterThanOrEqual(MIN_WIDTH - 1e-9);
    }
  });

  it('refuses a new column past four, from a lone widget at that edge, and for an unknown widget or column', () => {
    let l = defaultLayout();
    l = moveWidget(l, 'asks', { kind: 'new', side: 'right' });
    l = moveWidget(l, 'done', { kind: 'new', side: 'right' });
    expect(l.columns).toHaveLength(4);
    expect(moveWidget(l, 'watches', { kind: 'new', side: 'left' })).toBe(l);
    const lone = moveWidget(defaultLayout(), 'asks', { kind: 'new', side: 'right' });
    expect(moveWidget(lone, 'asks', { kind: 'new', side: 'right' })).toBe(lone);
    expect(moveWidget(defaultLayout(), 'asks', { kind: 'col', col: 7, index: 0 }).columns).toHaveLength(2);
    const d = defaultLayout();
    expect(moveWidget(d, 'nope' as WidgetKey, { kind: 'col', col: 0, index: 0 })).toBe(d);
    expect(moveWidget(d, 'asks', { kind: 'col', col: 7, index: 0 })).toBe(d);
  });

  it('does not change the layout it was given', () => {
    const d = defaultLayout();
    const snapshot = JSON.stringify(d);
    moveWidget(d, 'asks', { kind: 'col', col: 1, index: 0 });
    moveWidget(d, 'asks', { kind: 'new', side: 'left' });
    addColumn(d);
    removeColumn(d, 0);
    expect(JSON.stringify(d)).toBe(snapshot);
  });
});

describe('the 18% clamp', () => {
  it('stops a splitter at 18% for the column on either side', () => {
    const d = defaultLayout();
    expect(widths(resizeColumns(d, 0, -0.9))).toEqual([MIN_WIDTH, 1 - MIN_WIDTH]);
    const right = widths(resizeColumns(d, 0, 0.9));
    expect(right[0]).toBeCloseTo(1 - MIN_WIDTH, 9);
    expect(right[1]).toBeCloseTo(MIN_WIDTH, 9);
  });

  it('moves a splitter by its delta inside the limits and changes only the two columns beside it', () => {
    const three = addColumn(defaultLayout());
    const l = resizeColumns(three, 1, 0.05);
    expect(l.columns[1]!.width).toBeCloseTo(three.columns[1]!.width + 0.05, 9);
    expect(l.columns[0]!.width).toBe(three.columns[0]!.width);
    expect(sum(l)).toBeCloseTo(1, 9);
  });

  it('ignores a splitter that does not exist or a delta that is not a number', () => {
    const d = defaultLayout();
    expect(resizeColumns(d, 1, 0.1)).toBe(d);
    expect(resizeColumns(d, -1, 0.1)).toBe(d);
    expect(resizeColumns(d, 0, Number.NaN)).toBe(d);
  });

  it('raises a width under 18% to 18% and takes the difference from the others in proportion', () => {
    const w = fitWidths([0.05, 0.45, 0.5]);
    expect(w[0]).toBeCloseTo(MIN_WIDTH, 9);
    expect(w.reduce((a, b) => a + b, 0)).toBeCloseTo(1, 9);
    expect(w[2]! / w[1]!).toBeCloseTo(0.5 / 0.45, 9);
    expect(fitWidths([1, 0, 0, 0]).every((x) => x >= MIN_WIDTH - 1e-9)).toBe(true);
    expect(fitWidths([0, 0]).join()).toBe('0.5,0.5');
    expect(fitWidths([])).toEqual([]);
  });

  it('keeps 18% on every column at four columns whatever the splitters did', () => {
    let l = addColumn(addColumn(defaultLayout()));
    l = addColumn(l);
    for (let i = 0; i < 3; i++) l = resizeColumns(l, i, -1);
    expect(Math.min(...widths(l))).toBeGreaterThanOrEqual(MIN_WIDTH - 1e-9);
    expect(sum(l)).toBeCloseTo(1, 9);
  });
});

describe('columns from 1 to 4', () => {
  it('adds empty columns up to four and refuses a fifth', () => {
    let l = defaultLayout();
    l = addColumn(l);
    expect(l.columns).toHaveLength(3);
    expect(l.columns[2]!.widgets).toEqual([]);
    l = addColumn(l);
    expect(l.columns).toHaveLength(MAX_COLUMNS);
    expect(addColumn(l)).toBe(l);
    expect(sum(l)).toBeCloseTo(1, 9);
    expect(Math.min(...widths(l))).toBeGreaterThanOrEqual(MIN_WIDTH - 1e-9);
  });

  it('removes columns down to one, moving the widgets to a neighbour, and refuses below one', () => {
    let l = addColumn(defaultLayout());
    l = removeColumn(l, 2);
    expect(l.columns).toHaveLength(2);
    l = removeColumn(l, 1);
    expect(names(l)).toEqual(['asks,expired,watches,done,flight,recent']);
    expect(widths(l)).toEqual([1]);
    expect(removeColumn(l, 0)).toBe(l);
  });

  it('gives a removed first column to the one on its right, and a later one to the one on its left', () => {
    expect(names(removeColumn(defaultLayout(), 0))).toEqual(['flight,recent,asks,expired,watches,done']);
    expect(names(removeColumn(defaultLayout(), 1))).toEqual(['asks,expired,watches,done,flight,recent']);
    const three = moveWidget(defaultLayout(), 'recent', { kind: 'new', side: 'right' });
    expect(names(removeColumn(three, 1))).toEqual(['asks,expired,watches,done,flight', 'recent']);
  });

  it('gives a removed column its width to the neighbour and keeps 18% and a sum of 1', () => {
    const three = addColumn(defaultLayout());
    const l = removeColumn(three, 2);
    expect(sum(l)).toBeCloseTo(1, 9);
    expect(l.columns[1]!.width).toBeCloseTo(three.columns[1]!.width + three.columns[2]!.width, 9);
  });

  it('refuses to remove a column that does not exist', () => {
    const d = defaultLayout();
    expect(removeColumn(d, 2)).toBe(d);
    expect(removeColumn(d, -1)).toBe(d);
    expect(removeColumn(d, 0.5)).toBe(d);
  });
});

describe('heights and open states', () => {
  it('sets a height rounded and clamped, and clears it with undefined', () => {
    let l = setHeight(defaultLayout(), 'asks', 300.4);
    expect(l.heights).toEqual({ asks: 300 });
    expect(setHeight(l, 'asks', 5).heights.asks).toBe(MIN_HEIGHT);
    expect(setHeight(l, 'asks', 99999).heights.asks).toBe(MAX_HEIGHT);
    l = setHeight(l, 'asks', undefined);
    expect(l.heights).toEqual({});
    expect(setHeight(l, 'asks', Number.NaN)).toBe(l);
  });

  it('keeps the open state a person gave a widget', () => {
    let l = setOpen(defaultLayout(), 'done', true);
    l = setOpen(l, 'asks', false);
    expect(l.open).toEqual({ done: true, asks: false });
  });
});

describe('reset', () => {
  it('restores the default columns, widths, heights and open states', () => {
    let l = moveWidget(defaultLayout(), 'asks', { kind: 'new', side: 'left' });
    l = setHeight(setOpen(resizeColumns(l, 0, 0.1), 'done', true), 'flight', 200);
    expect(isDefaultLayout(l)).toBe(false);
    expect(resetLayout()).toEqual(defaultLayout());
    expect(isDefaultLayout(resetLayout())).toBe(true);
  });
});

describe('keyboard moves, one position at a time', () => {
  const d = defaultLayout();

  it('moves up and down inside the column and stops at its ends', () => {
    expect(names(nudgeWidget(d, 'expired', 'up'))[0]).toBe('expired,asks,watches,done');
    expect(names(nudgeWidget(d, 'asks', 'down'))[0]).toBe('expired,asks,watches,done');
    expect(nudgeWidget(d, 'asks', 'up')).toBe(d);
    expect(nudgeWidget(d, 'done', 'down')).toBe(d);
  });

  it('moves one place at a time, so repeated presses walk a widget down the column', () => {
    let l = d;
    for (let i = 0; i < 3; i++) l = nudgeWidget(l, 'asks', 'down');
    expect(names(l)[0]).toBe('expired,watches,done,asks');
  });

  it('moves right and left into the next column at the same place', () => {
    const right = nudgeWidget(d, 'expired', 'right');
    expect(names(right)).toEqual(['asks,watches,done', 'flight,expired,recent']);
    expect(locate(right, 'expired')).toEqual({ col: 1, index: 1 });
    const left = nudgeWidget(d, 'recent', 'left');
    expect(names(left)).toEqual(['asks,recent,expired,watches,done', 'flight']);
    expect(locate(left, 'recent')).toEqual({ col: 0, index: 1 });
  });

  it('puts a widget at the end of a shorter next column', () => {
    expect(names(nudgeWidget(d, 'done', 'right'))[1]).toBe('flight,recent,done');
  });

  it('opens a new column off the edge, and refuses at four columns or for a lone widget', () => {
    expect(names(nudgeWidget(d, 'asks', 'left'))).toEqual(['asks', 'expired,watches,done', 'flight,recent']);
    expect(names(nudgeWidget(d, 'recent', 'right'))).toEqual(['asks,expired,watches,done', 'flight', 'recent']);
    const four = addColumn(addColumn(d));
    expect(nudgeWidget(four, 'asks', 'left')).toBe(four);
    const lone = nudgeWidget(d, 'asks', 'left');
    expect(nudgeWidget(lone, 'asks', 'left')).toBe(lone);
  });

  it('ignores a widget that is not in the layout', () => {
    expect(nudgeWidget(d, 'nope' as WidgetKey, 'up')).toBe(d);
  });

  it('announces just the title of a widget the layout does not hold', () => {
    expect(positionLabel({ ...d, columns: [{ width: 1, widgets: [] }] }, 'asks')).toBe('Open asks');
  });

  it('announces the widget, its column and its position', () => {
    expect(positionLabel(d, 'asks')).toBe('Open asks, column 1 of 2, position 1 of 4');
    expect(positionLabel(nudgeWidget(d, 'asks', 'right'), 'asks')).toBe('Open asks, column 2 of 2, position 1 of 3');
  });
});

describe('a stored layout', () => {
  type Loose = { columns: { width: unknown; widgets: unknown[] }[]; heights?: unknown; open?: unknown };
  const good = () => JSON.parse(JSON.stringify(defaultLayout())) as Loose;
  const parse = (v: unknown) => parseLayout(JSON.stringify(v));

  it('round-trips through the versioned key', () => {
    const s = store();
    const l = setHeight(setOpen(moveWidget(resizeColumns(defaultLayout(), 0, 0.2), 'done', { kind: 'col', col: 1, index: 0 }), 'asks', false), 'flight', 240);
    saveLayout(s, l);
    expect([...s.data.keys()]).toEqual(['vh.now.layout.v1']);
    expect(loadLayout(s)).toEqual(l);
  });

  it('is the default when nothing is stored', () => {
    expect(loadLayout(store())).toEqual(defaultLayout());
    expect(loadLayout(undefined)).toEqual(defaultLayout());
    expect(parseLayout(null)).toEqual(defaultLayout());
    expect(parseLayout(undefined)).toEqual(defaultLayout());
  });

  it('is the default when the value does not parse', () => {
    for (const raw of ['', '{', 'nope', 'null', '7', '"x"', '[]', '{}', '{"columns":5}']) {
      expect(parseLayout(raw)).toEqual(defaultLayout());
    }
  });

  it('is the default when the model is broken', () => {
    const cases: Record<string, (l: Loose) => void> = {
      'a fifth column': (l) => {
        l.columns = [0, 1, 2, 3, 4].map((i) => ({ width: 0.2, widgets: i === 0 ? [...WIDGETS] : [] }));
      },
      'no column': (l) => {
        l.columns = [];
      },
      'a width under 18%': (l) => {
        l.columns[0].width = 0.1;
        l.columns[1].width = 0.9;
      },
      'widths that do not add up to 1': (l) => {
        l.columns[0].width = 0.3;
      },
      'a widget twice': (l) => {
        l.columns[1].widgets.push('asks');
      },
      'a widget missing': (l) => {
        l.columns[1].widgets.pop();
      },
      'an unknown widget': (l) => {
        l.columns[1].widgets[0] = 'nope';
      },
      'a width that is not a number': (l) => {
        l.columns[0].width = '0.5';
      },
      'a column that is not an object': (l) => {
        l.columns[0] = 3 as never;
      },
      'a column without widgets': (l) => {
        delete (l.columns[0] as { widgets?: unknown }).widgets;
      },
      'a height under the minimum': (l) => {
        l.heights = { asks: 10 };
      },
      'a height over the maximum': (l) => {
        l.heights = { asks: 1e6 };
      },
      'a height for an unknown widget': (l) => {
        l.heights = { nope: 200 };
      },
      'heights that are not an object': (l) => {
        l.heights = [200];
      },
      'an open state that is not a boolean': (l) => {
        l.open = { asks: 'yes' };
      },
      'an open state for an unknown widget': (l) => {
        l.open = { nope: true };
      },
    };
    for (const [name, damage] of Object.entries(cases)) {
      const l = good();
      damage(l);
      expect(parse(l), name).toEqual(defaultLayout());
    }
  });

  it('is accepted when valid, with heights and open states optional', () => {
    const l = good();
    delete l.heights;
    delete l.open;
    expect(parse(l)).toEqual(defaultLayout());
    const four = { columns: [0.25, 0.25, 0.25, 0.25].map((width, i) => ({ width, widgets: i === 0 ? [...WIDGETS] : [] })), heights: { asks: 200 }, open: { done: true } };
    expect(parse(four).columns).toHaveLength(4);
    expect(parse(four).heights).toEqual({ asks: 200 });
    expect(parse(four).open).toEqual({ done: true });
  });

  it('accepts widths within rounding of 1 and 18%, and refits them exactly', () => {
    const l = parse({ columns: [{ width: 0.181, widgets: ['asks'] }, { width: 0.819, widgets: WIDGETS.slice(1) }] });
    expect(l.columns[0]!.width).toBeGreaterThanOrEqual(MIN_WIDTH - 1e-9);
    expect(sum(l)).toBeCloseTo(1, 9);
  });

  it('is the default when a width is under 18% by any visible amount, not clamped', () => {
    const pair = (a: number, b: number) => parse({ columns: [{ width: a, widgets: ['asks'] }, { width: b, widgets: WIDGETS.slice(1) }] });
    for (const [a, b] of [
      [0.176, 0.824],
      [0.1799, 0.8201],
      [0.17999, 0.82001],
    ] as const) {
      expect(pair(a, b), `${a}/${b}`).toEqual(defaultLayout());
      expect(pair(b, a), `${b}/${a}`).toEqual(defaultLayout());
    }
    const edge = pair(0.18, 0.82);
    expect(edge.columns.map((c) => c.width)).toEqual([0.18, 0.82]);
  });

  it('is the default when widths miss 1 by any visible amount', () => {
    for (const [a, b] of [
      [0.5, 0.496],
      [0.5, 0.504],
      [0.5, 0.4999],
    ] as const) {
      expect(parse({ columns: [{ width: a, widgets: ['asks'] }, { width: b, widgets: WIDGETS.slice(1) }] }), `${a}+${b}`).toEqual(defaultLayout());
    }
  });

  it('accepts every layout the model itself writes', () => {
    let l = defaultLayout();
    const steps: ((x: NowLayout) => NowLayout)[] = [
      (x) => resizeColumns(x, 0, -0.4),
      (x) => addColumn(x),
      (x) => addColumn(x),
      (x) => resizeColumns(x, 1, 0.123456789),
      (x) => moveWidget(x, 'flight', { kind: 'col', col: 3, index: 0 }),
      (x) => removeColumn(x, 2),
      (x) => resizeColumns(x, 0, -1),
    ];
    for (const step of steps) {
      l = step(l);
      expect(l.columns.every((c) => c.width >= MIN_WIDTH - 1e-9)).toBe(true);
      expect(parseLayout(JSON.stringify(l))).toEqual(l);
    }
  });

  it('falls back to the default when storage throws', () => {
    const blocked = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    expect(loadLayout(blocked)).toEqual(defaultLayout());
    expect(() => saveLayout(blocked, defaultLayout())).not.toThrow();
    expect(() => saveLayout(undefined, defaultLayout())).not.toThrow();
  });
});
