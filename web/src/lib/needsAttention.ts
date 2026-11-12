// The Needs attention section of the Tasks view and the initiative page (PRD R2, SPEC 11): the tasks that need a person,
// read with `attention=true` under the view's own filters and `limit=200`, ordered by `orderNeedsAttention`, shown above the
// other tasks, and a `+K more` line past the 200. `readNeeds` pages the API, `NeedsStore` holds the answer (newest fetch wins),
// and `withoutNeeds` takes the section's tasks out of the table below it so no task is listed twice.
import type { components } from './api/schema';
import { orderNeedsAttention } from './attention';
import { errorText, Fetches } from './fetches';
import type { TaskListItem } from './initiatives';

export type TaskList = components['schemas']['TaskList'];

/** How many tasks the section shows; the rest are a `+K more` line. */
export const NEEDS_LIMIT = 200;
/** A page of the count past the limit is as large as the API allows, so a long tail takes few requests. */
const COUNT_PAGE = '1000';

export interface NeedsAttention {
  /** At most NEEDS_LIMIT tasks, in the Needs attention order. */
  items: TaskListItem[];
  /** How many more tasks need attention than `items` holds. */
  more: number;
}

export const NO_NEEDS: NeedsAttention = { items: [], more: 0 };

/** The query of the section for a view's query: the same filters, `attention=true` and the limit. */
export function needsQuery(query: URLSearchParams): URLSearchParams {
  const params = new URLSearchParams(query);
  params.set('attention', 'true');
  params.set('limit', String(NEEDS_LIMIT));
  params.delete('after');
  return params;
}

/** The `+K more` line: how many more tasks need attention than the section shows. */
export function moreText(more: number): string {
  return `+${more} more`;
}

/** The heading of the section: its title and the number of tasks it shows. */
export function needsHeading(needs: Pick<NeedsAttention, 'items' | 'more'>): string {
  return `Needs attention (${needs.items.length + needs.more})`;
}

/** Reads one page of `GET /tasks` for a query. */
export type PageReader = (query: URLSearchParams) => Promise<TaskList>;

/** The first 200 tasks that need attention under `query`, ordered, and how many more there are (counted through the following pages). */
export async function readNeeds(read: PageReader, query: URLSearchParams): Promise<NeedsAttention> {
  const first = await read(needsQuery(query));
  const items = orderNeedsAttention(first.items.slice(0, NEEDS_LIMIT));
  let more = 0;
  let after = first.next_after;
  while (after !== undefined) {
    const params = needsQuery(query);
    params.set('limit', COUNT_PAGE);
    params.set('after', after);
    const page = await read(params);
    more += page.items.length;
    after = page.next_after;
  }
  return { items, more };
}

/** The tasks that are not in the section: what the table below it shows. */
export function withoutNeeds<T extends { id: string }>(tasks: readonly T[], needs: Pick<NeedsAttention, 'items'>): T[] {
  const shown = new Set(needs.items.map((task) => task.id));
  return tasks.filter((task) => !shown.has(task.id));
}

export interface NeedsState {
  needs: NeedsAttention;
  /** Why the section could not be read; the section keeps what it had. */
  error: string | undefined;
}

/** Holds the section: a fetch that lands after a newer one has landed is dropped, a failed one keeps the section it had. */
export class NeedsStore {
  private readonly fetch: (query: URLSearchParams) => Promise<NeedsAttention>;
  private readonly onChange: (state: NeedsState) => void;
  private current: NeedsState = { needs: NO_NEEDS, error: undefined };
  private readonly fetches = new Fetches();

  constructor(fetch: (query: URLSearchParams) => Promise<NeedsAttention>, onChange: (state: NeedsState) => void) {
    this.fetch = fetch;
    this.onChange = onChange;
  }

  get state(): NeedsState {
    return this.current;
  }

  /** Reads the section under a view's query; `null` is a view whose filter cannot hold a task that needs attention, so the section is empty. */
  async load(query: URLSearchParams | null): Promise<void> {
    const ticket = this.fetches.begin();
    let outcome: NeedsState;
    try {
      outcome = { needs: query === null ? NO_NEEDS : await this.fetch(query), error: undefined };
    } catch (e) {
      outcome = { needs: this.current.needs, error: errorText(e) };
    }
    if (this.fetches.end(ticket)) {
      this.current = outcome;
      this.onChange(outcome);
    }
  }
}
