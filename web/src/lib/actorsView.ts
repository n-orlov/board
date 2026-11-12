// The Actors view's data layer and mapping (PRD R7, SPEC 11): `ActorsApi` reads `GET /actors`, `actorRows` turns the
// answer into the rows the view draws (the actors that hold a live lease first, each with its leases linking to their
// tasks, then the rest by last seen), and `ActorsStore` keeps the newest answer. A live lease row names the lease, the
// task it is on, the intent and when it runs out.
import type { components } from './api/schema';
import { expiresIn } from './durations';
import { errorText, Fetches } from './fetches';
import { absoluteTime, relativeTime } from './format';
import { taskHref } from './taskPage';
import { ThreadApi } from './threads';
import { sortFromQuery, sortToQuery, type Columns, type SortState } from './tableSort';
import type { BoardEvent } from './live';

export type ActorView = components['schemas']['ActorView'];

/** One live lease of an actor as the view draws it. */
export interface LeaseLine {
  ref: string;
  task: string;
  /** The fragment of the task page the lease links to. */
  href: string;
  intent: string;
  /** "expires in 41m". */
  expires: string;
  resources: string[];
}

/** One actor as the view draws it. */
export interface ActorRow {
  handle: string;
  kind: string;
  /** True when the actor holds a live lease: such rows come first. */
  alive: boolean;
  /** The way back to copy; undefined when the board has none. */
  wayBack: string | undefined;
  /** "3 minutes ago", and the absolute time for its title. */
  lastSeen: string;
  lastSeenTitle: string;
  /** The instant of `last_seen`, which the Last seen column sorts by. */
  lastSeenAt: string;
  leases: LeaseLine[];
  /** How many asks the actor has open. */
  openAsks: number;
}

/** The order of the view: actors with a live lease first, each group by `last_seen` newest first, ties by handle. */
export function orderActors(actors: readonly ActorView[]): ActorView[] {
  const alive = (a: ActorView) => a.leases.length > 0;
  return [...actors].sort(
    (a, b) =>
      Number(alive(b)) - Number(alive(a)) ||
      Date.parse(b.last_seen) - Date.parse(a.last_seen) ||
      (a.handle < b.handle ? -1 : a.handle > b.handle ? 1 : 0),
  );
}

function leaseLine(lease: ActorView['leases'][number], now: Date): LeaseLine {
  return {
    ref: lease.ref,
    task: lease.task,
    href: taskHref(lease.task),
    intent: lease.intent,
    expires: expiresIn(lease.expires_at, now),
    resources: lease.resources,
  };
}

function actorRow(actor: ActorView, now: Date): ActorRow {
  return {
    handle: actor.handle,
    kind: actor.kind,
    alive: actor.leases.length > 0,
    wayBack: actor.way_back === undefined || actor.way_back === '' ? undefined : actor.way_back,
    lastSeen: relativeTime(actor.last_seen, now),
    lastSeenTitle: absoluteTime(actor.last_seen),
    lastSeenAt: actor.last_seen,
    leases: actor.leases.map((l) => leaseLine(l, now)),
    openAsks: actor.asks.length,
  };
}

/** The rows of the view, in its order; `now` phrases the ages. */
export function actorRows(actors: readonly ActorView[], now: Date): ActorRow[] {
  return orderActors(actors).map((a) => actorRow(a, now));
}

/** The name of the view's saved state: the key is `board.view.v1.actors`. */
export const ACTORS_VIEW = 'actors';

/**
 * The sortable columns of the table, by the name the `sort` query uses. A lease column sorts by the ref of the actor's first
 * (oldest) live lease, so an actor without one is empty and sits last; Way back is not a sortable value.
 */
export const ACTOR_COLUMNS: Columns<ActorRow> = {
  actor: { type: 'text', value: (row) => row.handle },
  leases: { type: 'ref', value: (row) => row.leases[0]?.ref },
  seen: { type: 'time', value: (row) => row.lastSeenAt },
};

/** The column names, in the order of the table. */
export const ACTOR_SORT_COLUMNS: readonly string[] = Object.keys(ACTOR_COLUMNS);

/** The choices of the phone's Sort select: a label for each column. */
export const ACTOR_SORT_LABELS: readonly { column: string; label: string }[] = [
  { column: 'actor', label: 'Actor' },
  { column: 'leases', label: 'Live leases' },
  { column: 'seen', label: 'Last seen' },
];

/** The sort a query names: an unknown column or a bad `dir` is the default order (live leases first, then last seen). */
export function sortOfQuery(query: string): SortState {
  return sortFromQuery(query, ACTOR_SORT_COLUMNS);
}

/** The fragment of the view for a sort: `#/actors`, or `#/actors?sort=<column>&dir=<dir>`. */
export function actorsHash(sort: SortState): string {
  const query = sortToQuery(sort);
  return query === '' ? '#/actors' : `#/actors?${query}`;
}

/** The accessible name of an actor's copy button. */
export function copyWayBackLabel(handle: string): string {
  return `Copy way back of ${handle}`;
}

/** The toast after a copy: the outcome in words, never the way back itself. */
export function copyToast(handle: string, ok: boolean): string {
  return ok ? `Copied the way back of ${handle}` : 'Copying failed. Select the way back and copy it by hand.';
}

/** Whether a live event can change the roster: any write moves `last_seen`, so everything but API key events does. */
export function affectsActors(event: Pick<BoardEvent, 'type'>): boolean {
  return !event.type.startsWith('key_');
}

export class ActorsApi extends ThreadApi {
  /** Every actor the board has recorded. */
  async list(): Promise<ActorView[]> {
    const response = await this.call('/api/v1/actors', 'Listing the actors', { headers: { Accept: 'application/json' } });
    return ((await response.json()) as components['schemas']['ActorList']).items;
  }
}

export interface ActorsState {
  actors: ActorView[];
  /** Whether the first read has landed. */
  loaded: boolean;
  loading: boolean;
  /** The refusal of the last read, empty when it worked. */
  error: string;
}

export class ActorsStore {
  private readonly api: Pick<ActorsApi, 'list'>;
  private readonly onChange: (state: ActorsState) => void;
  private current: ActorsState = { actors: [], loaded: false, loading: true, error: '' };
  private readonly fetches = new Fetches();

  constructor(api: Pick<ActorsApi, 'list'>, onChange: (state: ActorsState) => void) {
    this.api = api;
    this.onChange = onChange;
  }

  get state(): ActorsState {
    return this.current;
  }

  private publish(change: Partial<ActorsState>): void {
    this.current = { ...this.current, ...change };
    this.onChange(this.current);
  }

  /** Reads the roster. A refresh keeps what is shown until the answer lands; an answer older than one already shown is dropped. */
  async load(): Promise<void> {
    const ticket = this.fetches.begin();
    this.publish({ loading: true });
    let outcome: Partial<ActorsState>;
    try {
      outcome = { actors: await this.api.list(), loaded: true, error: '' };
    } catch (e) {
      outcome = { error: errorText(e) };
    }
    const show = this.fetches.end(ticket);
    this.publish({ ...(show ? outcome : {}), loading: this.fetches.busy });
  }
}
