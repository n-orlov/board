import { describe, expect, it } from 'vitest';
import { COMPACT_ROWS, ROOMY_ROWS, laneStyle, leaseSlotBox } from './timeline';
import { currentRows, rowsName, watchRows, type RowsEnv } from './timelineRows';

function fakeEnv(state: { compact: boolean; desktop: boolean }) {
  const listeners = new Set<() => void>();
  const env: RowsEnv = {
    compact: () => state.compact,
    desktop: () => state.desktop,
    subscribe: (callback) => {
      listeners.add(callback);
      return () => listeners.delete(callback);
    },
  };
  return { env, notify: () => listeners.forEach((l) => l()), listeners };
}

describe('the Timeline rows follow density and viewport', () => {
  it('is tight only for Compact from 768 px', () => {
    expect(currentRows({ compact: () => true, desktop: () => true })).toBe(COMPACT_ROWS);
    expect(currentRows({ compact: () => false, desktop: () => true })).toBe(ROOMY_ROWS);
    expect(currentRows({ compact: () => true, desktop: () => false })).toBe(ROOMY_ROWS); // a phone keeps 44 px targets
    expect(currentRows({ compact: () => false, desktop: () => false })).toBe(ROOMY_ROWS);
  });

  it('reports the rows now, then each change but not a repeat, and stops on request', () => {
    const state = { compact: true, desktop: true };
    const { env, notify, listeners } = fakeEnv(state);
    const seen: string[] = [];
    const stop = watchRows(env, (rows) => seen.push(rowsName(rows)));
    expect(seen).toEqual(['compact']);
    notify();
    expect(seen).toEqual(['compact']);
    state.compact = false;
    notify();
    state.desktop = false; // still roomy: no new report
    notify();
    state.compact = true; // a phone, still roomy
    notify();
    state.desktop = true;
    notify();
    expect(seen).toEqual(['compact', 'roomy', 'compact']);
    stop();
    expect(listeners.size).toBe(0);
  });
});

describe('the row geometries', () => {
  it('has a shorter task row, initiative row and lease area in Compact than in Sparse', () => {
    expect(COMPACT_ROWS.taskRow).toBeLessThan(ROOMY_ROWS.taskRow);
    expect(COMPACT_ROWS.initiativeRow).toBeLessThan(ROOMY_ROWS.initiativeRow);
    expect(laneStyle(COMPACT_ROWS)).toContain('--tl-task-row:40px');
    expect(laneStyle(COMPACT_ROWS)).toContain('--tl-initiative-row:32px');
    expect(laneStyle()).toBe(laneStyle(ROOMY_ROWS));
  });

  for (const geometry of [ROOMY_ROWS, COMPACT_ROWS]) {
    it(`keeps every lane inside the ${geometry.taskRow} px row, in order, above 24 px targets`, () => {
      const { planned, actual, lease } = geometry.lanes;
      expect(planned.top + planned.height).toBeLessThanOrEqual(actual.top);
      expect(actual.top + actual.height).toBeLessThanOrEqual(lease.top);
      expect(geometry.initiativeRow).toBeGreaterThanOrEqual(24);
      for (const count of [1, 2, 3]) {
        const boxes = Array.from({ length: count }, (_, slot) => leaseSlotBox(slot, count, geometry));
        for (const box of boxes) expect(box.top + box.height).toBeLessThanOrEqual(geometry.taskRow);
        // every bar shows a strip no other bar covers
        const rowsOf = (b: { top: number; height: number }) => Array.from({ length: Math.round(b.height * 2) }, (_, n) => b.top + n / 2);
        boxes.forEach((box, i) => {
          const others = new Set(boxes.filter((_, j) => j !== i).flatMap(rowsOf));
          expect(rowsOf(box).some((r) => !others.has(r))).toBe(true);
        });
      }
    });
  }
});
