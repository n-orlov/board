// The browser session as the UI sees it. The server owns authentication (SPEC 3.2): a
// navigation with no session never reaches the app, it is redirected to /auth/login, which
// starts the OIDC flow. The UI covers the other way a session ends, while the page is open
// (expiry, logout elsewhere): the first API call answers 401, and the page goes to the same
// login path instead of showing a broken screen.
import { mountPrefix } from './apibase';
import type { components } from './api/schema';

export type Session = components['schemas']['Session'];

export type SessionState =
  | { kind: 'signed-in'; session: Session }
  | { kind: 'redirecting' }
  | { kind: 'error'; message: string };

/** What the session store needs from the browser: tests pass fakes. */
export interface Browser {
  fetch: (input: string, init?: RequestInit) => Promise<Response>;
  /** Navigates the page (location.assign). */
  assign: (url: string) => void;
  pathname: string;
}

/** The login page for the app mounted at `pathname`: "/auth/login" at the root. */
export function loginUrl(pathname: string): string {
  return mountPrefix(pathname) + '/auth/login';
}

/** The slice of `window` the browser adapter reads: tests pass a fake. */
export interface PageWindow {
  fetch: Browser['fetch'];
  location: { assign: (url: string) => void; pathname: string };
}

/** The browser this page runs in. */
export function currentBrowser(win: PageWindow = window): Browser {
  return {
    fetch: (input, init) => win.fetch(input, init),
    assign: (url) => win.location.assign(url),
    pathname: win.location.pathname,
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
    return { kind: 'error', message: `The session could not be read (HTTP ${response.status}).` };
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
