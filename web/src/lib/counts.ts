// How task counts and live leases are presented (PRD R9): the counts of an initiative (`{open, done, closed}`)
// become one summary line and a list of badges, and the live lease count becomes a phrase.
import type { components } from './api/schema';
import type { Tone } from './presentation';

export type TaskCounts = components['schemas']['TaskCounts'];

export interface CountBadge {
  key: keyof TaskCounts;
  label: string;
  count: number;
  tone: Tone;
}

const BADGES: readonly Omit<CountBadge, 'count'>[] = [
  { key: 'open', label: 'open', tone: 'success' },
  { key: 'done', label: 'done', tone: 'warning' },
  { key: 'closed', label: 'closed', tone: 'neutral' },
];

/** How many tasks the counts hold in all. */
export function totalTasks(counts: TaskCounts): number {
  return counts.open + counts.done + counts.closed;
}

/** One badge per status, in the order open, done, closed; a status with no tasks is kept, with a zero. */
export function countBadges(counts: TaskCounts): CountBadge[] {
  return BADGES.map((badge) => ({ ...badge, count: counts[badge.key] }));
}

/** The summary line: "2 open · 1 done · 3 closed", or "No tasks" when there are none. Statuses at zero are left out. */
export function countsText(counts: TaskCounts): string {
  const parts = countBadges(counts)
    .filter((badge) => badge.count > 0)
    .map((badge) => `${badge.count} ${badge.label}`);
  return parts.length === 0 ? 'No tasks' : parts.join(' · ');
}

/** The live lease phrase: "no live leases", "1 live lease", "3 live leases". */
export function leasesText(live: number): string {
  if (live === 0) return 'no live leases';
  return `${live} live ${live === 1 ? 'lease' : 'leases'}`;
}
