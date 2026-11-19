// UX prototype: the Now view's row model. Needs me is the attention sections regrouped into bands
// (asks, then expired leases with conflicts, then overdue watches, then done-not-closed at the bottom);
// each row also carries a time, a first actor and the task refs it is on, for sorting and tag filtering.
import type { SectionKey } from '../attention';
import { rowsFor, type Attention, type ItemRow } from '../attentionView';
import type { TaskListItem } from '../initiatives';

export type NowSort = 'newest' | 'oldest' | 'actor';
export const NOW_SORTS: readonly { value: NowSort; label: string }[] = [
  { value: 'newest', label: 'Newest first' },
  { value: 'oldest', label: 'Oldest / longest waiting' },
  { value: 'actor', label: 'Actor' },
];
export const isNowSort = (v: unknown) => v === 'newest' || v === 'oldest' || v === 'actor';

export interface NowRow extends ItemRow {
  section: SectionKey;
  /** The time the row is sorted by (ms): when the ask was raised, the lease expired, the check fell due, the task was done. */
  at: number;
  /** The handle sorted by under "Actor". */
  actor: string;
  /** The task refs the row is on (a conflict can span several). */
  refs: string[];
}

export type BandKey = 'asks' | 'stuck' | 'overdue' | 'done';

export interface Band {
  key: BandKey;
  title: string;
  hint: string;
  sections: SectionKey[];
  empty: string;
  /** Muted at the bottom. */
  muted: boolean;
}

export const BANDS: readonly Band[] = [
  { key: 'asks', title: 'Open asks', hint: 'someone is waiting on an answer', sections: ['open_asks'], empty: 'Nobody is waiting on an answer.', muted: false },
  {
    key: 'stuck',
    title: 'Expired leases & conflicts',
    hint: 'work that stopped, or two holders on one resource',
    sections: ['expired_leases', 'conflicts'],
    empty: 'No expired leases, no conflicts.',
    muted: false,
  },
  { key: 'overdue', title: 'Overdue watches', hint: 'a check fell due', sections: ['overdue_watches'], empty: 'No watch is overdue.', muted: false },
  { key: 'done', title: 'Done, not closed', hint: 'ready for you to verify and close', sections: ['done_not_closed'], empty: 'Nothing is waiting to be closed.', muted: true },
];

interface Meta {
  at: number;
  actor: string;
  refs: string[];
}

function metas(key: SectionKey, a: Attention, now: Date, leaseStart: ReadonlyMap<string, number>): Meta[] {
  const t = (s: string | undefined) => (s === undefined ? 0 : Date.parse(s));
  switch (key) {
    case 'open_asks':
      return (a.open_asks ?? []).map((x) => ({ at: now.getTime() - x.age_seconds * 1000, actor: x.asker.handle, refs: x.task === undefined ? [] : [x.task] }));
    case 'expired_leases':
      return (a.expired_leases ?? []).map((x) => ({ at: t(x.expired_at), actor: x.holder.handle, refs: [x.task] }));
    case 'overdue_watches':
      return (a.overdue_watches ?? []).map((x) => ({ at: t(x.next_check_at), actor: x.owner.handle, refs: [x.task] }));
    case 'done_not_closed':
      return (a.done_not_closed ?? []).map((x) => ({ at: t(x.done_at), actor: x.done_by.handle, refs: [x.task] }));
    case 'conflicts':
      // A conflict began when its newest holder started.
      return (a.conflicts ?? []).map((x) => ({
        at: Math.max(0, ...x.holders.map((h) => leaseStart.get(h.lease) ?? 0)),
        actor: x.holders[0]?.actor.handle ?? '',
        refs: [...x.tasks],
      }));
  }
}

/** The rows of a band, each with its section, time, actor and refs. */
export function bandRows(band: Band, a: Attention, now: Date, leaseStart: ReadonlyMap<string, number>): NowRow[] {
  return band.sections.flatMap((section) => {
    const meta = metas(section, a, now, leaseStart);
    return rowsFor(section, a, now).map((row, n) => ({ ...row, section, ...(meta[n] ?? { at: 0, actor: '', refs: [] }) }));
  });
}

/** Sorts rows (any with `at` and `actor`); equal keys keep their order. */
export function sortNow<T extends { at: number; actor: string }>(rows: readonly T[], sort: NowSort): T[] {
  const indexed = rows.map((row, i) => ({ row, i }));
  indexed.sort((x, y) => {
    if (sort === 'actor') return x.row.actor.localeCompare(y.row.actor) || y.row.at - x.row.at || x.i - y.i;
    return (sort === 'newest' ? y.row.at - x.row.at : x.row.at - y.row.at) || x.i - y.i;
  });
  return indexed.map((e) => e.row);
}

/** The live leases of the board by lease ref → start time, from the task list. */
export function leaseStarts(tasks: readonly TaskListItem[]): Map<string, number> {
  const map = new Map<string, number>();
  for (const task of tasks) for (const l of task.live_leases) map.set(l.ref, Date.parse(l.started_at));
  return map;
}

/** The time an in-flight task sorts by: its newest lease activity (start or renewal). */
export function flightAt(task: TaskListItem): number {
  return Math.max(0, ...task.live_leases.map((l) => Math.max(Date.parse(l.started_at), Date.parse(l.renewed_at))));
}
