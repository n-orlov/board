// The Attention view's data layer and row model (PRD R9, SPEC 5.3 and 11). `AttentionApi` reads `GET /attention`
// under the filter, `AttentionStore` holds the latest answer, and `rowsFor` turns one section into the rows the
// view draws: a title, a link to the task, detail lines and the actors involved, each with the way back to copy.
import { mountPrefix } from './apibase';
import type { components } from './api/schema';
import { ageText, askerWaitingLabel, apiQuery, DEFAULT_ATTENTION_FILTER, type AttentionFilter, type SectionKey } from './attention';
import { errorText, Fetches } from './fetches';
import { relativeTime } from './format';
import { problemText } from './keys';
import type { Browser } from './session';

export type Attention = components['schemas']['Attention'];
type Actor = components['schemas']['AttentionActor'];
type BoardEvent = components['schemas']['Event'];

/** An actor on an item: what they did there, their handle and the way back the board has for them. */
export interface ItemActor {
  role: string;
  handle: string;
  wayBack: string | undefined;
}

/** One item of a section as the view draws it. */
export interface ItemRow {
  /** A key that is unique inside the section. */
  id: string;
  title: string;
  /** The fragment of the task the item is on, when it is on one. */
  href: string | undefined;
  details: string[];
  actors: ItemActor[];
}

function actor(role: string, who: Actor): ItemActor {
  return { role, handle: who.handle, wayBack: who.way_back === undefined || who.way_back === '' ? undefined : who.way_back };
}

function taskHref(ref: string): string {
  return `#/t/${encodeURIComponent(ref)}`;
}

function askRows(items: NonNullable<Attention['open_asks']>): ItemRow[] {
  return items.map((a) => ({
    id: a.ref,
    title: `${a.ref}: ${a.question}`,
    href: a.task === undefined ? undefined : taskHref(a.task),
    details: [`asked ${ageText(a.age_seconds)}`, a.mode === 'stop' ? 'asker stopped' : askerWaitingLabel(a.asker_waiting), ...(a.blocking ? ['blocking'] : [])],
    actors: [actor('asked by', a.asker)],
  }));
}

function doneRows(items: NonNullable<Attention['done_not_closed']>, now: Date): ItemRow[] {
  return items.map((t) => {
    const lease = t.last_lease;
    const outcome = lease?.outcome === undefined || lease.outcome === '' ? [] : [`last lease ${lease.lease}: ${lease.outcome}`];
    return {
      id: t.task,
      title: `${t.task} ${t.title}`,
      href: taskHref(t.task),
      details: [`done ${relativeTime(t.done_at, now)}`, ...outcome],
      actors: [actor('done by', t.done_by), ...(lease === undefined ? [] : [actor('last lease', lease.holder)])],
    };
  });
}

function leaseRows(items: NonNullable<Attention['expired_leases']>, now: Date): ItemRow[] {
  return items.map((l) => ({
    id: l.lease,
    title: `${l.lease} on ${l.task} ${l.title}`,
    href: taskHref(l.task),
    details: [l.intent, `expired ${relativeTime(l.expired_at, now)}`],
    actors: [actor('held by', l.holder)],
  }));
}

function watchRows(items: NonNullable<Attention['overdue_watches']>, now: Date): ItemRow[] {
  return items.map((w) => ({
    id: w.task,
    title: `${w.task} ${w.title}`,
    href: taskHref(w.task),
    details: [`check was due ${relativeTime(w.next_check_at, now)}`, ...(w.last_check === undefined ? [] : [`last check: ${w.last_check.summary}`])],
    actors: [actor('owner', w.owner), ...(w.last_check === undefined ? [] : [actor('last check by', w.last_check.by)])],
  }));
}

function conflictRows(items: NonNullable<Attention['conflicts']>): ItemRow[] {
  return items.map((c) => ({
    id: c.resource,
    title: c.resource,
    href: c.tasks.length === 1 ? taskHref(c.tasks[0]!) : undefined,
    details: c.holders.map((h) => `${h.lease} on ${h.task}: ${h.intent}`),
    actors: c.holders.map((h) => actor(h.lease, h.actor)),
  }));
}

const BUILDERS: Record<SectionKey, (attention: Attention, now: Date) => ItemRow[]> = {
  open_asks: (a) => askRows(a.open_asks ?? []),
  done_not_closed: (a, now) => doneRows(a.done_not_closed ?? [], now),
  expired_leases: (a, now) => leaseRows(a.expired_leases ?? [], now),
  overdue_watches: (a, now) => watchRows(a.overdue_watches ?? [], now),
  conflicts: (a) => conflictRows(a.conflicts ?? []),
};

/** The rows of one section; `now` phrases the ages. A section the answer lacks has no rows. */
export function rowsFor(key: SectionKey, attention: Attention, now: Date): ItemRow[] {
  return BUILDERS[key](attention, now);
}

/** Whether a live event can change what needs attention: everything but notes and API keys. */
export function affectsAttention(event: Pick<BoardEvent, 'type'>): boolean {
  return !event.type.startsWith('note_') && !event.type.startsWith('key_');
}

/** The accessible name of the copy button of an actor (shown only when the board has a way back for them). */
export function copyLabel(a: ItemActor): string {
  return `Copy way back of ${a.handle}`;
}

/** The initiatives and tags the filter can offer: those of the initiatives, plus what the filter already names, sorted. */
export function filterChoices(
  initiatives: readonly { slug: string; tags: readonly string[] }[],
  filter: AttentionFilter,
): { initiatives: string[]; tags: string[] } {
  const slugs = new Set([...initiatives.map((i) => i.slug), ...filter.initiatives]);
  const tags = new Set([...initiatives.flatMap((i) => i.tags), ...filter.tags]);
  return { initiatives: [...slugs].sort(), tags: [...tags].sort() };
}

export class AttentionApi {
  private readonly browser: Browser;

  constructor(browser: Browser) {
    this.browser = browser;
  }

  async get(filter: AttentionFilter): Promise<Attention> {
    const query = apiQuery(filter).toString();
    const path = `/api/v1/attention${query === '' ? '' : `?${query}`}`;
    let response: Response;
    try {
      response = await this.browser.fetch(mountPrefix(this.browser.pathname) + path, {
        headers: { Accept: 'application/json' },
        credentials: 'same-origin',
      });
    } catch {
      throw new Error('The server is not reachable.');
    }
    if (!response.ok) throw new Error(await problemText(response, 'Reading what needs attention'));
    return (await response.json()) as Attention;
  }
}

export interface AttentionState {
  attention: Attention | undefined;
  filter: AttentionFilter;
  loading: boolean;
  error: string | undefined;
}

export class AttentionStore {
  private readonly api: Pick<AttentionApi, 'get'>;
  private readonly onChange: (state: AttentionState) => void;
  private current: AttentionState;
  private readonly fetches = new Fetches();

  constructor(api: Pick<AttentionApi, 'get'>, onChange: (state: AttentionState) => void, filter: AttentionFilter = DEFAULT_ATTENTION_FILTER) {
    this.api = api;
    this.onChange = onChange;
    this.current = { attention: undefined, filter, loading: true, error: undefined };
  }

  private publish(change: Partial<AttentionState>): void {
    this.current = { ...this.current, ...change };
    this.onChange(this.current);
  }

  /** Reads the sections under the current filter. A refresh keeps what is shown until the answer lands. */
  async load(): Promise<void> {
    const ticket = this.fetches.begin();
    const filter = this.current.filter;
    this.publish({ loading: true });
    let outcome: Partial<AttentionState>;
    try {
      outcome = { attention: await this.api.get(filter), error: undefined };
    } catch (e) {
      outcome = { error: errorText(e) };
    }
    // An answer that lands after a newer one, or for a filter no longer set, is dropped.
    const show = this.fetches.end(ticket) && filter === this.current.filter;
    this.publish({ ...(show ? outcome : {}), loading: this.fetches.busy });
  }

  async setFilter(filter: AttentionFilter): Promise<void> {
    this.current = { ...this.current, filter };
    await this.load();
  }
}
