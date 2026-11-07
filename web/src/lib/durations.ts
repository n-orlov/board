// Durations as the task views print them (PRD R10): a watch task's cadence ("every 5 minutes") and a lease's
// "expires in". Both take the instants as arguments, so a test needs no fake clock.
const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

function count(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? '' : 's'}`;
}

/** A length in seconds as the largest unit that divides it exactly: 86400 is "1 day", 5400 is "90 minutes", 90 is "90 seconds". */
export function durationText(seconds: number): string {
  if (seconds >= DAY && seconds % DAY === 0) return count(seconds / DAY, 'day');
  if (seconds >= HOUR && seconds % HOUR === 0) return count(seconds / HOUR, 'hour');
  if (seconds >= MINUTE && seconds % MINUTE === 0) return count(seconds / MINUTE, 'minute');
  return count(seconds, 'second');
}

/** A watch task's cadence: "every 5 minutes"; empty for a task that has none. */
export function cadenceText(seconds: number | undefined): string {
  return seconds === undefined ? '' : `every ${durationText(seconds)}`;
}

/** A length of whole seconds as "3h 10m", "2h", "41m" or "20s" (the CLI's span). */
function span(totalSeconds: number): string {
  const hours = Math.floor(totalSeconds / HOUR);
  const minutes = Math.floor((totalSeconds % HOUR) / MINUTE);
  if (hours > 0) return minutes === 0 ? `${hours}h` : `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m`;
  return `${totalSeconds}s`;
}

/** Whole seconds from `now` to the instant (negative when it has passed); undefined for an unparseable instant. */
function secondsUntil(instant: string, now: Date): number | undefined {
  const at = new Date(instant).getTime();
  return Number.isNaN(at) ? undefined : Math.floor((at - now.getTime()) / 1000);
}

/**
 * When a lease runs out relative to `now`: "expires in 41m", "expires in under a minute" (less than 60 seconds
 * left) or "expired 2m ago". An unparseable instant gives the empty string.
 */
export function expiresIn(expiresAt: string, now: Date): string {
  const left = secondsUntil(expiresAt, now);
  if (left === undefined) return '';
  if (left < 0) return `expired ${span(-left)} ago`;
  if (left < MINUTE) return 'expires in under a minute';
  return `expires in ${span(left)}`;
}

/**
 * When a watch task's next check is due relative to `now`: "due in 14m", "due in under a minute" or "overdue by 2h".
 * An unparseable instant gives the empty string.
 */
export function nextCheckText(nextCheckAt: string, now: Date): string {
  const left = secondsUntil(nextCheckAt, now);
  if (left === undefined) return '';
  if (left < 0) return `overdue by ${span(-left)}`;
  if (left < MINUTE) return 'due in under a minute';
  return `due in ${span(left)}`;
}
