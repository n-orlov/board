// The Initiatives page's data layer (SPEC 11). `InitiativesApi` wraps the two initiative
// operations the page uses: every call carries the session cookie, and the create carries the
// session's `X-CSRF-Token` (SPEC 3.2). `InitiativesStore` holds what the page shows: the list
// and the error of the last action, which is how a server validation error reaches the form.
import { mountPrefix } from './apibase';
import type { components } from './api/schema';
import { problemText } from './keys';
import type { Browser } from './session';

export type Initiative = components['schemas']['Initiative'];
export type InitiativeCreate = components['schemas']['InitiativeCreate'];

/** What the create form collects: tags are one comma-separated line. */
export interface InitiativeForm {
  slug: string;
  title: string;
  description: string;
  tags: string;
}

/** A refused or failed call; `status` is 0 when the server could not be reached. */
export class InitiativesError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'InitiativesError';
    this.status = status;
  }
}

/** The request body for a form: text is trimmed, blank optional fields are left out. */
export function toCreate(form: InitiativeForm): InitiativeCreate {
  const body: InitiativeCreate = { slug: form.slug.trim(), title: form.title.trim() };
  const description = form.description.trim();
  if (description !== '') body.description = description;
  const tags = form.tags
    .split(',')
    .map((tag) => tag.trim())
    .filter((tag) => tag !== '');
  if (tags.length > 0) body.tags = tags;
  return body;
}

export class InitiativesApi {
  private readonly browser: Browser;
  private readonly csrf: () => string;

  /** `csrf` is read at each write, so a page that rotates its token is followed. */
  constructor(browser: Browser, csrf: () => string) {
    this.browser = browser;
    this.csrf = csrf;
  }

  private async call(path: string, what: string, init: RequestInit): Promise<Response> {
    let response: Response;
    try {
      response = await this.browser.fetch(mountPrefix(this.browser.pathname) + path, { ...init, credentials: 'same-origin' });
    } catch {
      throw new InitiativesError(0, 'The server is not reachable.');
    }
    if (!response.ok) throw new InitiativesError(response.status, await problemText(response, what));
    return response;
  }

  /** Every initiative, following `next_after` through the pages. */
  async list(): Promise<Initiative[]> {
    const items: Initiative[] = [];
    let after: string | undefined;
    do {
      const query = after === undefined ? '' : `?after=${encodeURIComponent(after)}`;
      const response = await this.call('/api/v1/initiatives' + query, 'Listing the initiatives', { headers: { Accept: 'application/json' } });
      const page = (await response.json()) as components['schemas']['InitiativeList'];
      items.push(...page.items);
      after = page.next_after;
    } while (after !== undefined);
    return items;
  }

  async create(body: InitiativeCreate): Promise<Initiative> {
    const response = await this.call('/api/v1/initiatives', 'Creating the initiative', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrf() },
      body: JSON.stringify(body),
    });
    return (await response.json()) as Initiative;
  }
}

export interface InitiativesState {
  initiatives: Initiative[];
  loading: boolean;
  /** Why the list could not be read. */
  error: string | undefined;
  /** Why the last create was refused; shown inside the form, which keeps what was typed. */
  formError: string | undefined;
}

export class InitiativesStore {
  private readonly api: Pick<InitiativesApi, 'list' | 'create'>;
  private readonly onChange: (state: InitiativesState) => void;
  private current: InitiativesState = { initiatives: [], loading: true, error: undefined, formError: undefined };

  constructor(api: Pick<InitiativesApi, 'list' | 'create'>, onChange: (state: InitiativesState) => void) {
    this.api = api;
    this.onChange = onChange;
  }

  get state(): InitiativesState {
    return this.current;
  }

  private set(patch: Partial<InitiativesState>): void {
    this.current = { ...this.current, ...patch };
    this.onChange(this.current);
  }

  private async run(action: () => Promise<Partial<InitiativesState>>, field: 'error' | 'formError'): Promise<boolean> {
    try {
      this.set({ ...(await action()), loading: false, [field]: undefined });
      return true;
    } catch (e) {
      this.set({ loading: field === 'error' ? false : this.current.loading, [field]: e instanceof Error ? e.message : String(e) });
      return false;
    }
  }

  async load(): Promise<void> {
    await this.run(async () => ({ initiatives: await this.api.list() }), 'error');
  }

  /** Makes the initiative and reloads the list. Whether it worked; on a refusal `error` says why. */
  async create(form: InitiativeForm): Promise<boolean> {
    const ok = await this.run(async () => {
      await this.api.create(toCreate(form));
      return {};
    }, 'formError');
    if (ok) await this.load();
    return ok;
  }

  /** Forgets a refusal, for a form that is opened again. */
  clearFormError(): void {
    this.set({ formError: undefined });
  }
}
