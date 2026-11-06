// The browser session as the UI sees it. The server owns authentication (SPEC 3.2): a
// navigation with no session never reaches the app, it is redirected to /auth/login, which
// starts the OIDC flow. The UI covers the other way a session ends, while the page is open
// (expiry, logout elsewhere): the first API call answers 401, and the page goes to the same
// login path instead of showing a broken screen.
import { mountPrefix, problemDetail } from './apibase';
import type { components } from './api/schema';

export type Session = components['schemas']['Session'];

export type SessionState =
  | { kind: 'loading' }
  | { kind: 'signed-in'; session: Session }
  | { kind: 'redirecting' }
  | { kind: 'error'; message: string }
  | { kind: 'signed-out' };

/** The fragment of the Signed out view. */
export const SIGNED_OUT_HASH = '#/signed-out';

/** What the session store needs from the browser: tests pass fakes. */
export interface Browser {
  fetch: (input: string, init?: RequestInit) => Promise<Response>;
  /** Navigates the page (location.assign). */
  assign: (url: string) => void;
  pathname: string;
  /** Replaces the page's fragment (location.hash = ...). */
  setHash: (hash: string) => void;
}

/** The login page for the app mounted at `pathname`: "/auth/login" at the root. */
export function loginUrl(pathname: string): string {
  return mountPrefix(pathname) + '/auth/login';
}

/** The slice of `window` the browser adapter reads: tests pass a fake. */
export interface PageWindow {
  fetch: Browser['fetch'];
  location: { assign: (url: string) => void; pathname: string; hash: string };
}

/** The browser this page runs in. */
export function currentBrowser(win: PageWindow = window): Browser {
  return {
    fetch: (input, init) => win.fetch(input, init),
    assign: (url) => win.location.assign(url),
    pathname: win.location.pathname,
    setHash: (hash) => {
      win.location.hash = hash;
    },
  };
}

/**
 * Asks the server who this session is. A 401 means there is no live session: the page is sent
 * to the login page, which starts the OIDC flow, and the state is `redirecting`. Anything else
 * that is not a session is an error the page shows, never a redirect: a server that is down
 * must not send the browser round a login loop.
 */
export async function loadSession(browser: Browser): Promise<SessionState> {
  let response: Response;
  try {
    response = await browser.fetch(mountPrefix(browser.pathname) + '/auth/session', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  } catch {
    return { kind: 'error', message: 'The server is not reachable.' };
  }
  if (response.status === 401) {
    browser.assign(loginUrl(browser.pathname));
    return { kind: 'redirecting' };
  }
  if (!response.ok) {
    const detail = await problemDetail(response);
    return { kind: 'error', message: detail ?? `The session could not be read (HTTP ${response.status}).` };
  }
  return { kind: 'signed-in', session: (await response.json()) as Session };
}

/**
 * Ends the session: `POST /auth/logout` with the session's CSRF token (SPEC 3.2). True when the
 * server cleared it; false when the call failed, so the caller keeps showing the signed-in app.
 */
export async function logout(browser: Browser, csrf: string): Promise<boolean> {
  try {
    const response = await browser.fetch(mountPrefix(browser.pathname) + '/auth/logout', {
      method: 'POST',
      headers: { 'X-CSRF-Token': csrf },
      credentials: 'same-origin',
    });
    return response.ok;
  } catch {
    return false;
  }
}

/** Where the page starts: the Signed out view when the fragment names it, else loading. */
export function initialState(hash: string): SessionState {
  return hash === SIGNED_OUT_HASH ? { kind: 'signed-out' } : { kind: 'loading' };
}

/** What the page does after Sign out was asked for. */
export type SignOutResult = 'signed-out' | 'failed';

/**
 * The session as one page holds it. Every state change goes to `onchange`. The Signed out view
 * is the one state that never starts the login redirect: it neither loads the session nor
 * follows a 401; only the Sign in button (a plain link to the login page) leaves it.
 */
export class SessionController {
  state: SessionState;

  constructor(
    private readonly browser: Browser,
    private readonly onchange: (state: SessionState) => void,
    hash: string,
  ) {
    this.state = initialState(hash);
  }

  private set(state: SessionState): void {
    this.state = state;
    this.onchange(state);
  }

  /** Starts the page: loads the session unless the page opened on the Signed out view. */
  async start(): Promise<void> {
    this.onchange(this.state);
    if (this.state.kind === 'loading') await this.load();
  }

  private async load(): Promise<void> {
    this.set({ kind: 'loading' });
    this.set(await loadSession(this.browser));
  }

  /** Retry on the error card: loads the session again. Any other state is left alone. */
  async retry(): Promise<void> {
    if (this.state.kind === 'error') await this.load();
  }

  /** Sign out: the server clears the session, then the Signed out view shows. */
  async signOut(): Promise<SignOutResult> {
    if (this.state.kind !== 'signed-in') return 'failed';
    if (!(await logout(this.browser, this.state.session.csrf_token))) return 'failed';
    this.browser.setHash(SIGNED_OUT_HASH);
    this.set({ kind: 'signed-out' });
    return 'signed-out';
  }

  /**
   * The fragment changed. Leaving the Signed out view (Back) has no session to show, so the
   * session is loaded again, which is allowed to redirect: the page is no longer on that view.
   * Opening the Signed out fragment while signed in is not a sign out: the page returns home.
   */
  async hashChanged(hash: string): Promise<void> {
    if (this.state.kind === 'signed-out' && hash !== SIGNED_OUT_HASH) await this.load();
    else if (this.state.kind === 'signed-in' && hash === SIGNED_OUT_HASH) this.browser.setHash('#/');
  }
}
