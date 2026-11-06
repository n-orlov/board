// Timestamps as the lists show them (PRD R6/R7): a relative phrase for the cell, the absolute local time
// for its `title`. Everything is computed from the instants alone (elapsed time, local calendar fields
// read through Date getters), so a caller passes `now` and a test needs no fake clock.
const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** The most days ago still phrased as "N days ago"; anything older is shown as a date. */
export const RELATIVE_DAYS_LIMIT = 7;

function pad(n: number): string {
  return String(n).padStart(2, '0');
}

function plural(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? '' : 's'} ago`;
}

/** The local calendar date, `YYYY-MM-DD`. */
function localDate(at: Date): string {
  return `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}`;
}

/**
 * `timestamp` phrased relative to `now`: "just now" (under a minute, and any future instant), "N minutes ago",
 * "N hours ago", "yesterday" (24 to 48 hours), "N days ago" (up to 7 days) and, older than that, the local date.
 * An unparseable timestamp gives the empty string.
 */
export function relativeTime(timestamp: string, now: Date): string {
  const at = new Date(timestamp);
  if (Number.isNaN(at.getTime())) return '';
  const elapsed = now.getTime() - at.getTime();
  if (elapsed < MINUTE) return 'just now';
  if (elapsed < HOUR) return plural(Math.floor(elapsed / MINUTE), 'minute');
  if (elapsed < DAY) return plural(Math.floor(elapsed / HOUR), 'hour');
  if (elapsed < 2 * DAY) return 'yesterday';
  if (elapsed <= RELATIVE_DAYS_LIMIT * DAY) return plural(Math.floor(elapsed / DAY), 'day');
  return localDate(at);
}

/** The absolute local time, `YYYY-MM-DD HH:mm`, for a `title`. An unparseable timestamp gives the empty string. */
export function absoluteTime(timestamp: string): string {
  const at = new Date(timestamp);
  if (Number.isNaN(at.getTime())) return '';
  return `${localDate(at)} ${pad(at.getHours())}:${pad(at.getMinutes())}`;
}
