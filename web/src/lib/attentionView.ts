// The Attention view's data layer and row model (PRD R9, SPEC 5.3 and 11). `AttentionApi` reads `GET /attention`
// under the filter, `AttentionStore` holds the latest answer, and `rowsFor` turns one section into the rows the
// view draws: a title, a link to the task, detail lines and the actors involved, each with the way back to copy.
import { mountPrefix } from './apibase';
import type { components } from './api/schema';
import { initiativeOfHref } from './attentionActions';
import {
  ageText,
  askerWaitingLabel,
  apiQuery,
  DEFAULT_ATTENTION_FILTER,
  filterFromQuery,
  filterToQuery,
  type AttentionFilter,
  type SectionKey,
} from './attention';
import { errorText, Fetches } from './fetches';
import { relativeTime } from './format';
import { problemText } from './keys';
import type { Browser } from './session';
import { clearViewQuery, openView, saveViewQuery, type ViewEnv, type ViewStorage } from './viewState';

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
  /** The slug of the initiative the item belongs to, when it does (an ask's own initiative, or its task's). */
  initiative: string | undefined;
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
  return items.map((a) => {
    const href = a.task === undefined ? undefined : taskHref(a.task);
    return {
      id: a.ref,
      title: `${a.ref}: ${a.question}`,
      href,
      initiative: a.initiative ?? initiativeOfHref(href),
      details: [`asked ${ageText(a.age_seconds)}`, a.mode === 'stop' ? 'asker stopped' : askerWaitingLabel(a.asker_waiting), ...(a.blocking ? ['blocking'] : [])],
      actors: [actor('asked by', a.asker)],
    };
  });
}

function doneRows(items: NonNullable<Attention['done_not_closed']>, now: Date): ItemRow[] {
  return items.map((t) => {
    const lease = t.last_lease;
    const outcome = lease?.outcome === undefined || lease.outcome === '' ? [] : [`last lease ${lease.lease}: ${lease.outcome}`];
    return {
      id: t.task,
      title: `${t.task} ${t.title}`,
      href: taskHref(t.task),
      initiative: initiativeOfHref(taskHref(t.task)),
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
    initiative: initiativeOfHref(taskHref(l.task)),
    details: [l.intent, `expired ${relativeTime(l.expired_at, now)}`],
    actors: [actor('held by', l.holder)],
  }));
}

function watchRows(items: NonNullable<Attention['overdue_watches']>, now: Date): ItemRow[] {
  return items.map((w) => ({
    id: w.task,
    title: `${w.task} ${w.title}`,
    href: taskHref(w.task),
    initiative: initiativeOfHref(taskHref(w.task)),
    details: [`check was due ${relativeTime(w.next_check_at, now)}`, ...(w.last_check === undefined ? [] : [`last check: ${w.last_check.summary}`])],
    actors: [actor('owner', w.owner), ...(w.last_check === undefined ? [] : [actor('last check by', w.last_check.by)])],
  }));
}

function conflictRows(items: NonNullable<Attention['conflicts']>): ItemRow[] {
  return items.map((c) => ({
    id: c.resource,
    title: c.resource,
    href: c.tasks.length === 1 ? taskHref(c.tasks[0]!) : undefined,
    initiative: initiativeOfHref(c.tasks.length === 1 ? taskHref(c.tasks[0]!) : undefined),
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

/** The name of the view's saved state (PRD R16): the key is `vh.view.v1.attention`. */
export const ATTENTION_VIEW = 'attention';

/** What opening the view found: the filter to start with, and whether it came from the saved query (a bare route). */
export interface OpenedAttention {
  filter: AttentionFilter;
  restored: boolean;
}

/**
 * Opens the view for the fragment it is on, before its first fetch. A fragment with a query wins and is saved; a bare
 * one (`#/attention`, or any fragment the router sends here) restores the saved query by rewriting the fragment in
 * place, so the view reads `GET /attention` once, under the restored filter.
 */
export function openAttention(env: ViewEnv, hash: string): OpenedAttention {
  const at = hash.indexOf('?');
  const own = at < 0 ? '' : hash.slice(at + 1);
  const query = openView(env, ATTENTION_VIEW, '#/attention', own);
  return { filter: filterFromQuery(query), restored: own === '' && query !== '' };
}

/** Saves the filter's query after a change (the empty filter is the default and removes the key). */
export function saveAttention(storage: ViewStorage | undefined, filter: AttentionFilter): void {
  saveViewQuery(storage, ATTENTION_VIEW, filterToQuery(filter));
}

/** Clear filters: forgets the saved query. */
export function forgetAttention(storage: ViewStorage | undefined): void {
  clearViewQuery(storage, ATTENTION_VIEW);
}

/**
 * A restored filter without the initiatives and tags the board no longer has: `known` is the list of initiatives with
 * their tags. A filter with nothing to drop is returned as it is (the same object).
 */
export function dropUnknown(filter: AttentionFilter, known: readonly { slug: string; tags: readonly string[] }[]): AttentionFilter {
  const slugs = new Set(known.map((i) => i.slug));
  const tags = new Set(known.flatMap((i) => i.tags));
  const initiatives = filter.initiatives.filter((slug) => slugs.has(slug));
  const kept = filter.tags.filter((tag) => tags.has(tag));
  if (initiatives.length === filter.initiatives.length && kept.length === filter.tags.length) return filter;
  return { initiatives, tags: kept };
}

/**
 * The filter the view starts from (or moves to on a hash change). Only a restored filter is checked, and only once the
 * lookup answered: `known` is the board's initiatives with their tags, or `undefined` when the lookup failed. An empty
 * list is an answer (the board has no initiatives), so every restored initiative and tag is dropped. A link's own query
 * is kept as it is, and the sections show the server's answer for it.
 */
export function startFilter(
  opened: OpenedAttention,
  known: readonly { slug: string; tags: readonly string[] }[] | undefined,
): AttentionFilter {
  if (!opened.restored || known === undefined) return opened.filter;
  return dropUnknown(opened.filter, known);
}
