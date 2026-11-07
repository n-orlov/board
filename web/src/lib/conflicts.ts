// Live leases and conflicts as the Now panel and the task page print them (PRD R11, SPEC 5.2 and 11). A conflict
// is two or more live leases that hold the same resource; it is a warning, never a lock. A lease never conflicts
// with itself, but two leases of one actor on different tasks that share a resource do conflict. The functions
// take the leases of `GET /api/v1/leases?state=live` and an instant, so a test needs no fake clock.
import type { components } from './api/schema';
import { expiresIn } from './durations';

export type Lease = components['schemas']['Lease'];

/** A lease that is live at `now`: its state says so and its time has not run out. */
export function isLive(lease: Pick<Lease, 'state' | 'expires_at'>, now: Date): boolean {
  return lease.state === 'live' && new Date(lease.expires_at).getTime() > now.getTime();
}

/** The leases that are live at `now`, in the order given. */
export function liveLeases<T extends Pick<Lease, 'state' | 'expires_at'>>(leases: readonly T[], now: Date): T[] {
  return leases.filter((lease) => isLive(lease, now));
}

/** One lease that holds a contested resource. */
export interface Holder {
  lease: string;
  holder: string;
  task: string;
  intent: string;
  expiresAt: string;
}

/** A resource that two or more live leases hold, with every holder in lease order. */
export interface ResourceConflict {
  resource: string;
  holders: Holder[];
}

function holderOf(lease: Lease): Holder {
  return { lease: lease.ref, holder: lease.actor, task: lease.task, intent: lease.intent, expiresAt: lease.expires_at };
}

/** The live leases per resource, in the order the resources are first seen. */
function byResource(live: readonly Lease[]): Map<string, Lease[]> {
  const groups = new Map<string, Lease[]>();
  for (const lease of live) {
    for (const resource of lease.resources) {
      groups.set(resource, [...(groups.get(resource) ?? []), lease]);
    }
  }
  return groups;
}

/** Every resource held by two or more live leases, sorted by resource name. */
export function resourceConflicts(leases: readonly Lease[], now: Date): ResourceConflict[] {
  return [...byResource(liveLeases(leases, now))]
    .filter(([, group]) => group.length > 1)
    .map(([resource, group]) => ({ resource, holders: group.map(holderOf) }))
    .sort((a, b) => a.resource.localeCompare(b.resource));
}

/** A resource one task's live leases share with other live leases, and the other holders of it. */
export interface TaskConflict {
  resource: string;
  others: Holder[];
}

/**
 * The conflicts of the live leases on one task: per contested resource that a live lease of the task holds, the
 * other holders of it (another actor, the same actor on another task, or a second lease on this task). The
 * task's oldest lease on the resource is the one in conflict; every other holder is listed. Sorted by resource.
 */
export function taskConflicts(task: string, leases: readonly Lease[], now: Date): TaskConflict[] {
  const conflicts: TaskConflict[] = [];
  for (const { resource, holders } of resourceConflicts(leases, now)) {
    const mine = holders.find((holder) => holder.task === task);
    if (mine !== undefined) conflicts.push({ resource, others: holders.filter((holder) => holder !== mine) });
  }
  return conflicts;
}

/** The live leases on one task, oldest first as given. */
export function leasesOfTask(task: string, leases: readonly Lease[], now: Date): Lease[] {
  return liveLeases(leases, now).filter((lease) => lease.task === task);
}

/** One row of the Now panel. */
export interface PanelRow {
  lease: string;
  holder: string;
  task: string;
  intent: string;
  expires: string;
  resources: string[];
  conflicting: boolean;
}

export interface NowPanel {
  rows: PanelRow[];
  conflicts: ResourceConflict[];
  summary: string;
}

function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? '' : 's'}`;
}

/** "2 live leases, 1 conflict"; "no live leases" when the board is idle. */
export function panelSummary(leaseCount: number, conflictCount: number): string {
  if (leaseCount === 0) return 'no live leases';
  const leases = plural(leaseCount, 'live lease');
  return conflictCount === 0 ? leases : `${leases}, ${plural(conflictCount, 'conflict')}`;
}

/** What the Now panel prints: one row per live lease (flagged when it is in a conflict), the conflicts, a summary. */
export function nowPanel(leases: readonly Lease[], now: Date): NowPanel {
  const live = liveLeases(leases, now);
  const conflicts = resourceConflicts(live, now);
  const contested = new Set(conflicts.flatMap((c) => c.holders.map((h) => h.lease)));
  const rows = live.map((lease) => ({
    lease: lease.ref,
    holder: lease.actor,
    task: lease.task,
    intent: lease.intent,
    expires: expiresIn(lease.expires_at, now),
    resources: lease.resources,
    conflicting: contested.has(lease.ref),
  }));
  return { rows, conflicts, summary: panelSummary(rows.length, conflicts.length) };
}

/** "alice on aa-1" (a holder and the task it works on). */
export function holderText(holder: Pick<Holder, 'holder' | 'task'>): string {
  return `${holder.holder} on ${holder.task}`;
}

/** "repo:board: held by alice on aa-1, bob on bb-2": one conflict line of the Now panel. */
export function conflictText(conflict: ResourceConflict): string {
  return `${conflict.resource}: held by ${conflict.holders.map(holderText).join(', ')}`;
}

/** "repo:board: also held by bob on bb-2": one conflict line of the task page. */
export function taskConflictText(conflict: TaskConflict): string {
  return `${conflict.resource}: also held by ${conflict.others.map(holderText).join(', ')}`;
}
