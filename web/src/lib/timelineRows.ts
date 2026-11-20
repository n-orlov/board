// Which rows the Timeline draws: the tight ones of Compact density from 768 px, the roomy ones otherwise (Sparse, and every
// phone, where rows are 44 px targets). The browser tells the chart when either the density or the viewport changes.

import { COMPACT_ROWS, rowGeometry, type RowGeometry } from './timeline';

/** What the watcher needs from the browser: tests pass fakes. */
export interface RowsEnv {
  /** Whether the density preference (`data-density` of the root element) is Compact. */
  compact: () => boolean;
  /** Whether the viewport is 768 px or wider. */
  desktop: () => boolean;
  /** Calls `callback` whenever either may have changed; returns the stop function. */
  subscribe: (callback: () => void) => () => void;
}

/** The rows that hold now. */
export function currentRows(env: Pick<RowsEnv, 'compact' | 'desktop'>): RowGeometry {
  return rowGeometry(env.compact(), env.desktop());
}

/** Reports the rows now, then again each time they change (not on every notification); returns the stop function. */
export function watchRows(env: RowsEnv, report: (rows: RowGeometry) => void): () => void {
  let last = currentRows(env);
  report(last);
  return env.subscribe(() => {
    const next = currentRows(env);
    if (next === last) return;
    last = next;
    report(next);
  });
}

/** Whether a row geometry is the tight one: for tests and the chart's data attribute. */
export function rowsName(rows: RowGeometry): 'compact' | 'roomy' {
  return rows === COMPACT_ROWS ? 'compact' : 'roomy';
}
