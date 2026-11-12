import { DatabaseSync } from 'node:sqlite';
import { join } from 'node:path';

const DAY_MS = 24 * 60 * 60 * 1000;

// The e2e server's own SQLite store (playwright.config.ts picks its directory), for the one thing the API cannot do:
// make a closed item old. Closing stamps `closed_at` with the server's clock, and a spec cannot wait thirty days.
// Times are stored as UTC milliseconds since the epoch (internal/store EncodeTime).
function backdate(table: 'tasks' | 'initiatives', id: string, days: number): void {
  const work = process.env.E2E_WORK;
  if (!work) throw new Error('E2E_WORK is not set (playwright.config.ts sets it)');
  const db = new DatabaseSync(join(work, 'board.db'));
  try {
    db.exec('PRAGMA busy_timeout = 5000');
    const row = db.prepare(`SELECT closed_at FROM ${table} WHERE id = ? AND closed_at IS NOT NULL`).get(id) as { closed_at: number } | undefined;
    if (row === undefined) throw new Error(`${table} ${id} is not closed`);
    db.prepare(`UPDATE ${table} SET closed_at = ? WHERE id = ?`).run(row.closed_at - days * DAY_MS, id);
  } finally {
    db.close();
  }
}

/** Moves the `closed_at` of a closed task (by its ULID) `days` back. */
export function backdateTask(id: string, days: number): void {
  backdate('tasks', id, days);
}

/** Moves the `closed_at` of a closed initiative (by its ULID) `days` back. */
export function backdateInitiative(id: string, days: number): void {
  backdate('initiatives', id, days);
}
