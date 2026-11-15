// The Timeline's state per history entry (R7, R8): which initiatives are collapsed and how far the rows are scrolled
// belong to the history entry the chart is displayed on, and are saved under THAT entry's key.
//
// A report that comes late (the rows' scroll settles 100 ms after the last wheel tick, vis-timeline redraws after a
// collapse) would otherwise land under whatever entry `history.state` names by then: after a Back that is the one
// being shown, not the one the state was left on. So the view keeps its own owner key, and every save goes to it. The
// owner changes only in `navigated`, after the chart has reported what it displayed to the entry being left; a page
// that unloads (`leaving`) does the same.

import { ensureKey, loadEntry, saveEntry, sessionStore, scrollOf, type EntryHistory, type EntryStorage } from './historyEntry';
import { collapsedOf } from './timelineView';

/** The name the rows' scroll is saved under for a history entry. */
export const ROW_SCROLL = 'timelineRows';
/** The name the collapsed initiatives are saved under for a history entry. */
export const COLLAPSED = 'timelineCollapsed';

/** The part of a chart this module drives. */
export interface EntryChart {
  flush(): void;
  showEntry(state: { collapsed: readonly string[]; rows: number }): void;
}

/** The slice of `Window` this module uses: tests pass a fake. */
export interface EntryWindow {
  readonly history: EntryHistory;
  readonly sessionStorage?: EntryStorage;
}

export class TimelineEntry {
  private owner: string;

  /**
   * @param chart the chart mounted now, if any
   * @param onTimeline whether the page's fragment is still the Timeline's (a Back to another view unmounts it next)
   */
  constructor(
    private readonly win: EntryWindow,
    private readonly chart: () => EntryChart | undefined,
    private readonly onTimeline: () => boolean,
  ) {
    this.owner = ensureKey(win.history);
  }

  /** The key of the entry the chart is displayed on. */
  key(): string {
    return this.owner;
  }

  /** The collapsed initiatives saved for the owning entry (none: every initiative open). */
  collapsed(): string[] {
    return collapsedOf(loadEntry(sessionStore(this.win), this.owner, COLLAPSED));
  }

  /** The rows' scroll saved for the owning entry, or undefined. */
  rows(): number | undefined {
    return scrollOf(loadEntry(sessionStore(this.win), this.owner, ROW_SCROLL));
  }

  saveRows(px: number): void {
    saveEntry(sessionStore(this.win), this.owner, ROW_SCROLL, px);
  }

  saveCollapsed(ids: string[]): void {
    saveEntry(sessionStore(this.win), this.owner, COLLAPSED, ids);
  }

  /** Has the chart report what it displays to the owning entry now (before a new entry is pushed, before the page goes). */
  leaving(): void {
    this.chart()?.flush();
  }

  /**
   * The page moved to another entry (popstate): the entry left first gets what the chart displays, then the chart shows
   * the entry now current, which has nothing saved when a link made it (every initiative open, rows at the top). A move
   * to another view changes nothing here: the chart is about to go, and it reports to the entry it was displayed on.
   */
  navigated(): void {
    this.leaving();
    if (!this.onTimeline()) return;
    const next = ensureKey(this.win.history);
    if (next === this.owner) return;
    this.owner = next;
    this.chart()?.showEntry({ collapsed: this.collapsed(), rows: this.rows() ?? 0 });
  }
}
