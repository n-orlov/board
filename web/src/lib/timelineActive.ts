// Timeline's Active first sort, Only active filter and the window control's visibility rule (PRD R8, SPEC 11).
// "Active" has one definition, the server's (SPEC 6.10): a live lease, any attention reason, or an event in the last window.
// The page asks `GET /timeline` for it (`active=true&window=`) instead of deriving it, so the sort, the filter and
// the Now view's In flight / Needs me / Recently touched can never disagree about what active means.
import type { SortChoice } from './tableSort';
import type { SortColumn } from './sortControl';
import type { Timeline } from './timeline';

/** The orders of the Timeline's rows: the current one, or the active tasks above the rest. */
export type TimelineOrder = 'default' | 'active';

/** The one column the shared sort control is given on Timeline. */
export const ACTIVE_COLUMNS: readonly SortColumn[] = [{ column: 'active', label: 'Active' }];

/** The options the shared control shows on Timeline: the current order, and Active first (fragment `sort=active&dir=desc`). */
export const ACTIVE_CHOICES: readonly SortChoice[] = [
  { value: '', label: 'Default order' },
  { value: 'active:desc', label: 'Active first' },
];

/** The text under the window control: what the three windows decide. */
export const WINDOW_HINT = 'Active means a live lease, something that needs attention, or a touch inside this window.';

/** The touched-in window control (8h, 24h, 3d) and its hint show only while Only active is ticked or the sort is Active first. */
export function windowVisible(activeOnly: boolean, order: TimelineOrder): boolean {
  return activeOnly || order === 'active';
}

/** Whether the rows need a second read for the active set: Active first on the whole board (Only active already has only active rows). */
export function needsActiveSet(activeOnly: boolean, order: TimelineOrder): boolean {
  return order === 'active' && !activeOnly;
}

/** The items of `list` for which `active` holds, then the rest, each part in its own order. */
function activeThenRest<T>(list: readonly T[], active: (item: T) => boolean): T[] {
  return [...list.filter(active), ...list.filter((item) => !active(item))];
}

/**
 * The answer with every active task before every idle one, each group keeping the order the answer gave it. The tasks
 * are drawn under their initiatives, so the initiatives that hold an active task come first too (their own order kept):
 * read top to bottom, no idle task is above an active one.
 */
export function activeFirst(timeline: Timeline, active: ReadonlySet<string>): Timeline {
  const tasks = activeThenRest(timeline.tasks, (t) => active.has(t.ref));
  const lively = new Set(tasks.filter((t) => active.has(t.ref)).map((t) => t.initiative));
  return { ...timeline, tasks, initiatives: activeThenRest(timeline.initiatives, (i) => lively.has(i.slug)) };
}

/** The refs of the tasks an answer holds: with `active=true` that is the active set. */
export function refsOf(timeline: Timeline): Set<string> {
  return new Set(timeline.tasks.map((t) => t.ref));
}
