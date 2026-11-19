// Timeline's Active first sort, Only active filter and the window control's visibility rule (PRD R8, SPEC 11).
// "Active" has one definition, the server's (SPEC 6.10): a live lease, any attention reason, or an event in the last window.
// The page asks `GET /timeline` for it (`active=true&window=`) instead of deriving it, so the sort, the filter and
// the Now view's In flight / Needs me / Recently touched can never disagree about what active means.
import type { SortChoice } from './tableSort';
import type { SortColumn } from './sortControl';
import type { Timeline, TimelineAnswer } from './timeline';

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

/**
 * The answer marked to be drawn Active first: `activeFirst` lists the refs of its tasks in `active`, and `buildTimeline`
 * draws the current order of those tasks, then the current order of the rest (an initiative holding both gets a row
 * in each part), so read top to bottom every active row is above every idle one and each part keeps the current order.
 */
export function activeFirst(timeline: Timeline, active: ReadonlySet<string>): TimelineAnswer {
  return { ...timeline, activeFirst: timeline.tasks.filter((t) => active.has(t.ref)).map((t) => t.ref) };
}

/** The refs of the tasks an answer holds: with `active=true` that is the active set. */
export function refsOf(timeline: Timeline): Set<string> {
  return new Set(timeline.tasks.map((t) => t.ref));
}
