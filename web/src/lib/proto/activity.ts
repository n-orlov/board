// UX prototype: what is in flight and what was touched lately, assembled in the browser from
// GET /tasks (live leases, attention) and GET /events (the window's events, lease ends included).
import { mountPrefix } from '../apibase';
import type { components } from '../api/schema';
import type { Browser } from '../session';

type BoardEvent = components['schemas']['Event'];

export type RecentWindow = '8h' | '24h' | '3d';
export const WINDOWS: readonly RecentWindow[] = ['8h', '24h', '3d'];
const HOUR = 3600_000;
export const WINDOW_MS: Record<RecentWindow, number> = { '8h': 8 * HOUR, '24h': 24 * HOUR, '3d': 72 * HOUR };
export const isWindow = (v: unknown) => v === '8h' || v === '24h' || v === '3d';

async function getJson<T>(browser: Browser, path: string): Promise<T> {
  const response = await browser.fetch(mountPrefix(browser.pathname) + path, { headers: { Accept: 'application/json' }, credentials: 'same-origin' });
  if (!response.ok) throw new Error(`Reading ${path.split('?')[0]} failed (${response.status})`);
  return (await response.json()) as T;
}

/** Every event since the time (or after the event id), oldest first, up to `pages` pages of 1000. */
export async function eventsSince(browser: Browser, since: Date | string, pages = 10): Promise<BoardEvent[]> {
  const out: BoardEvent[] = [];
  let after: string | undefined;
  for (let n = 0; n < pages; n++) {
    const q = new URLSearchParams({ since: typeof since === 'string' ? since : since.toISOString(), limit: '1000' });
    if (after !== undefined) q.set('after', after);
    const page = await getJson<{ items: BoardEvent[]; next_after?: string }>(browser, `/api/v1/events?${q.toString()}`);
    out.push(...page.items);
    if (page.next_after === undefined) break;
    after = page.next_after;
  }
  return out;
}

export interface Touch {
  /** The last time anything happened on the task inside the window (ms). */
  at: number;
  /** Who did it. */
  actor: string;
  /** What it was. */
  summary: string;
}

/** The last touch of each task inside the window: its latest event. */
export function lastTouches(events: readonly BoardEvent[], since: number): Map<string, Touch> {
  const map = new Map<string, Touch>();
  for (const e of events) {
    if (e.task === undefined) continue;
    const at = Date.parse(e.at);
    if (at < since) continue;
    const old = map.get(e.task);
    if (old === undefined || old.at <= at) map.set(e.task, { at, actor: e.actor, summary: e.summary });
  }
  return map;
}
/** "in 1h 20m" / "5m ago" from ms. */
export function shortSpan(ms: number): string {
  const s = Math.max(0, Math.round(Math.abs(ms) / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return m % 60 === 0 ? `${h}h` : `${h}h ${m % 60}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}
