// The green badge on the Now navigation item (PRD R5, SPEC 11): how many open tasks hold at least one live lease.
// The count is the length of `GET /api/v1/tasks?status=open&active=true` (`active` is "has a live lease", the server
// expires due leases before it reads); it is read once the shell is up and again, at most once per burst, on every
// live event that names a task.
import type { TasksApi } from './tasks';

/** The query of the tasks the badge counts: open and active. */
export function liveTasksQuery(): URLSearchParams {
  return new URLSearchParams({ status: 'open', active: 'true', limit: '1000' });
}

/** How many open tasks hold a live lease. A failed read rejects: the caller keeps the count it showed. */
export async function countLiveTasks(api: Pick<TasksApi, 'listQuery'>): Promise<number> {
  return (await api.listQuery(liveTasksQuery())).length;
}

/** Whether a live event can change the count: any event that names a task (a lease starting, ending or expiring, a close). */
export function affectsLiveTasks(event: { type: string; task?: string }): boolean {
  return event.task !== undefined;
}
