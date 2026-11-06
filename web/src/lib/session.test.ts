import { describe, expect, it, vi } from 'vitest';
import { currentBrowser, loadSession, loginUrl, logout, type Browser, type Session } from './session';

const session: Session = { email: 'op@example.test', principal: 'oidc:op@example.test', actor: 'operator', csrf_token: 'tok' };

function browser(respond: () => Promise<Response>, pathname = '/') {
  const assign = vi.fn();
  const fetchFn = vi.fn((_input: string, _init?: RequestInit) => respond());
  const b: Browser = { fetch: fetchFn, assign, pathname };
  return { b, assign, fetchFn };
}

describe('loginUrl', () => {
  it.each([
    ['/', '/auth/login'],
    ['/index.html', '/auth/login'],
    ['/board/', '/board/auth/login'],
  ])('is the login page of the app mounted at %j', (pathname, want) => {
    expect(loginUrl(pathname)).toBe(want);
  });
});

describe('loadSession', () => {
  it('sends an unauthenticated page to the login page, which starts the OIDC flow', async () => {
    const { b, assign } = browser(async () => new Response('{}', { status: 401 }));
    expect(await loadSession(b)).toEqual({ kind: 'redirecting' });
    expect(assign).toHaveBeenCalledTimes(1);
    expect(assign).toHaveBeenCalledWith('/auth/login');
  });

  it('keeps the mount prefix on the login redirect', async () => {
    const { b, assign } = browser(async () => new Response('{}', { status: 401 }), '/board/');
    await loadSession(b);
    expect(assign).toHaveBeenCalledWith('/board/auth/login');
  });

  it('returns the session of a signed-in page and does not navigate', async () => {
    const { b, assign, fetchFn } = browser(async () => Response.json(session));
    expect(await loadSession(b)).toEqual({ kind: 'signed-in', session });
    expect(assign).not.toHaveBeenCalled();
    expect(fetchFn).toHaveBeenCalledTimes(1);
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/auth/session');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.headers).toEqual({ Accept: 'application/json' });
  });

  it('asks the session under the mount prefix', async () => {
    const { b, fetchFn } = browser(async () => Response.json(session), '/board/');
    await loadSession(b);
    expect(fetchFn.mock.calls[0][0]).toBe('/board/auth/session');
  });

  it('shows an error, and does not redirect, when the server fails', async () => {
    const { b, assign } = browser(async () => new Response('{}', { status: 503 }));
    expect(await loadSession(b)).toEqual({ kind: 'error', message: 'The session could not be read (HTTP 503).' });
    expect(assign).not.toHaveBeenCalled();
  });

  it('shows an error, and does not redirect, when the server is unreachable', async () => {
    const { b, assign } = browser(async () => {
      throw new TypeError('network');
    });
    expect(await loadSession(b)).toEqual({ kind: 'error', message: 'The server is not reachable.' });
    expect(assign).not.toHaveBeenCalled();
  });
});

describe('logout', () => {
  it('posts the logout with the session CSRF token and reports success', async () => {
    const { b, fetchFn } = browser(async () => new Response(null, { status: 204 }));
    expect(await logout(b, 'tok')).toBe(true);
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/auth/logout');
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.headers).toEqual({ 'X-CSRF-Token': 'tok' });
  });

  it('posts under the mount prefix', async () => {
    const { b, fetchFn } = browser(async () => new Response(null, { status: 204 }), '/board/');
    await logout(b, 'tok');
    expect(fetchFn.mock.calls[0][0]).toBe('/board/auth/logout');
  });

  it('reports failure when the server refuses', async () => {
    const { b } = browser(async () => new Response('{}', { status: 403 }));
    expect(await logout(b, 'bad')).toBe(false);
  });

  it('reports failure when the server is unreachable', async () => {
    const { b } = browser(async () => {
      throw new TypeError('network');
    });
    expect(await logout(b, 'tok')).toBe(false);
  });
});

describe('currentBrowser', () => {
  it('forwards fetch and navigation to the window and reads its path', async () => {
    const assign = vi.fn();
    const fetchFn = vi.fn(async (_input: string, _init?: RequestInit) => new Response('{}', { status: 200 }));
    const b = currentBrowser({ fetch: fetchFn, location: { assign, pathname: '/board/' } });
    expect(b.pathname).toBe('/board/');
    await b.fetch('/x', { method: 'GET' });
    expect(fetchFn).toHaveBeenCalledWith('/x', { method: 'GET' });
    b.assign('/auth/login');
    expect(assign).toHaveBeenCalledWith('/auth/login');
  });
});
