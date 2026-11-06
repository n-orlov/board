// The Settings -> API keys page's data layer (SPEC 11, 3.3). `KeysApi` wraps the three key
// operations: every call carries the session cookie, and the writes carry the session's
// `X-CSRF-Token` (SPEC 3.2). `KeysStore` holds what the page shows and is what the page drives:
// the list, the one-time new key, and the error of the last action.
import { mountPrefix, problemDetail } from './apibase';
import type { components } from './api/schema';
import type { Browser } from './session';

export type Key = components['schemas']['Key'];
export type CreatedKey = components['schemas']['CreatedKey'];

/** A refused or failed call; `status` is 0 when the server could not be reached. */
export class KeysError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'KeysError';
    this.status = status;
  }
}

/** The text of a problem document's detail (or title), else a line naming the HTTP status. */
export async function problemText(response: Response, what: string): Promise<string> {
  return (await problemDetail(response)) ?? `${what} failed (HTTP ${response.status}).`;
}

export class KeysApi {
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
      throw new KeysError(0, 'The server is not reachable.');
    }
    if (!response.ok) throw new KeysError(response.status, await problemText(response, what));
    return response;
  }

  async list(): Promise<Key[]> {
    const response = await this.call('/api/v1/keys', 'Listing the keys', { headers: { Accept: 'application/json' } });
    return ((await response.json()) as components['schemas']['KeyList']).items;
  }

  /** `expiresIn` is "90d" or "12h"; blank means the key does not expire. */
  async create(name: string, expiresIn: string): Promise<CreatedKey> {
    const body: components['schemas']['KeyCreate'] = { name };
    if (expiresIn !== '') body.expires_in = expiresIn;
    const response = await this.call('/api/v1/keys', 'Creating the key', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrf() },
      body: JSON.stringify(body),
    });
    return (await response.json()) as CreatedKey;
  }

  async revoke(name: string): Promise<Key> {
    const response = await this.call(`/api/v1/keys/${encodeURIComponent(name)}/revoke`, 'Revoking the key', {
      method: 'POST',
      headers: { Accept: 'application/json', 'X-CSRF-Token': this.csrf() },
    });
    return (await response.json()) as Key;
  }
}

export interface KeysState {
  keys: Key[];
  loading: boolean;
  /** The key just created: shown this once, dropped by `dismiss` or by the next action. */
  created: CreatedKey | undefined;
  /** Why the list or a revoke failed; shown on the page. */
  error: string | undefined;
  /** Why the last create was refused; shown inside the form, which keeps what was typed. */
  formError: string | undefined;
}

export class KeysStore {
  private readonly api: Pick<KeysApi, 'list' | 'create' | 'revoke'>;
  private readonly onChange: (state: KeysState) => void;
  private current: KeysState = { keys: [], loading: true, created: undefined, error: undefined, formError: undefined };

  constructor(api: Pick<KeysApi, 'list' | 'create' | 'revoke'>, onChange: (state: KeysState) => void) {
    this.api = api;
    this.onChange = onChange;
  }

  get state(): KeysState {
    return this.current;
  }

  private set(patch: Partial<KeysState>): void {
    this.current = { ...this.current, ...patch };
    this.onChange(this.current);
  }

  private async run(action: () => Promise<Partial<KeysState>>, field: 'error' | 'formError' = 'error'): Promise<boolean> {
    try {
      this.set({ ...(await action()), [field]: undefined });
      return true;
    } catch (e) {
      this.set({ [field]: e instanceof Error ? e.message : String(e) });
      return false;
    }
  }

  /** Every fetch of the list, the first and each refresh, is `loading` while the request is pending and not after it, whether it worked or failed. */
  async load(): Promise<void> {
    this.set({ loading: true });
    try {
      this.set({ keys: await this.api.list(), loading: false, error: undefined });
    } catch (e) {
      this.set({ loading: false, error: e instanceof Error ? e.message : String(e) });
    }
  }

  /** Makes the key, keeps it for the page to show once, and reloads the list. Whether it worked; on a refusal `formError` says why. */
  async create(name: string, expiresIn: string): Promise<boolean> {
    const ok = await this.run(async () => ({ created: await this.api.create(name.trim(), expiresIn.trim()) }), 'formError');
    if (ok) await this.load();
    return ok;
  }

  /** Forgets a refusal, for a form that is opened again. */
  clearFormError(): void {
    this.set({ formError: undefined });
  }

  async revoke(name: string): Promise<boolean> {
    const ok = await this.run(async () => {
      await this.api.revoke(name);
      return { created: undefined };
    });
    if (ok) await this.load();
    return ok;
  }

  /** Drops the one-time key from the page. */
  dismiss(): void {
    this.set({ created: undefined });
  }
}
