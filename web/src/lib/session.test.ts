import { describe, expect, it, vi } from 'vitest';
import {
  currentBrowser,
  initialState,
  loadSession,
  loginUrl,
  logout,
  SessionController,
  SIGNED_OUT_HASH,
  type Browser,
  type Session,
  type SessionState,
} from './session';

const session: Session = { email: 'op@example.test', principal: 'oidc:op@example.test', actor: 'operator', csrf_token: 'tok' };

function browser(respond: () => Promise<Response>, pathname = '/') {
  const assign = vi.fn();
  const fetchFn = vi.fn((_input: string, _init?: RequestInit) => respond());
  const setHash = vi.fn();
  const b: Browser = { fetch: fetchFn, assign, pathname, setHash };
  return { b, assign, fetchFn, setHash };
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

  it('shows the problem detail the server gave, and does not redirect', async () => {
    const detail = 'The session store is temporarily unavailable.';
    const { b, assign } = browser(
      async () =>
        new Response(JSON.stringify({ status: 503, title: 'Service Unavailable', detail }), {
          status: 503,
          headers: { 'Content-Type': 'application/problem+json' },
        }),
    );
    expect(await loadSession(b)).toEqual({ kind: 'error', message: detail });
    expect(assign).not.toHaveBeenCalled();
  });

  it('shows an error naming the status, and does not redirect, when the failure has no problem detail', async () => {
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
    const location = { assign, pathname: '/board/', hash: '' };
    const b = currentBrowser({ fetch: fetchFn, location });
    expect(b.pathname).toBe('/board/');
    await b.fetch('/x', { method: 'GET' });
    expect(fetchFn).toHaveBeenCalledWith('/x', { method: 'GET' });
    b.assign('/auth/login');
    expect(assign).toHaveBeenCalledWith('/auth/login');
    b.setHash('#/signed-out');
    expect(location.hash).toBe('#/signed-out');
  });

  it('hands the window storage over, and none where reading it throws', () => {
    const location = { assign: vi.fn(), pathname: '/', hash: '' };
    const storage = memoryStorage({});
    expect(currentBrowser({ fetch: vi.fn(), location, localStorage: storage }).storage).toBe(storage);
    const blocked = {
      fetch: vi.fn(),
      location,
      get localStorage(): never {
        throw new Error('blocked');
      },
    };
    expect(currentBrowser(blocked).storage).toBeUndefined();
  });

  it('hands the window sessionStorage over, and none where reading it throws', () => {
    const location = { assign: vi.fn(), pathname: '/', hash: '' };
    const tab = memoryStorage({});
    expect(currentBrowser({ fetch: vi.fn(), location, sessionStorage: tab }).sessionStorage).toBe(tab);
    const blocked = {
      fetch: vi.fn(),
      location,
      get sessionStorage(): never {
        throw new Error('blocked');
      },
    };
    expect(currentBrowser(blocked).sessionStorage).toBeUndefined();
  });
});

describe('initialState', () => {
  it('is signed out on the Signed out fragment and loading on any other', () => {
    expect(initialState(SIGNED_OUT_HASH)).toEqual({ kind: 'signed-out' });
    expect(initialState('')).toEqual({ kind: 'loading' });
    expect(initialState('#/')).toEqual({ kind: 'loading' });
    expect(initialState('#/settings/keys')).toEqual({ kind: 'loading' });
  });
});

function memoryStorage(initial: Record<string, string>) {
  const data = { ...initial };
  return {
    data,
    getItem: (k: string) => data[k] ?? null,
    setItem: (k: string, v: string) => {
      data[k] = v;
    },
    removeItem: (k: string) => {
      delete data[k];
    },
    key: (i: number) => Object.keys(data)[i] ?? null,
    get length() {
      return Object.keys(data).length;
    },
  };
}

function controller(respond: () => Promise<Response>, hash = '#/', pathname = '/') {
  const made = browser(respond, pathname);
  const seen: SessionState['kind'][] = [];
  const c = new SessionController(made.b, (s) => seen.push(s.kind), hash);
  return { ...made, c, seen };
}

describe('SessionController', () => {
  it('goes loading -> signed-in', async () => {
    const { c, seen, assign } = controller(async () => Response.json(session));
    await c.start();
    expect(seen).toEqual(['loading', 'loading', 'signed-in']);
    expect(c.state).toEqual({ kind: 'signed-in', session });
    expect(assign).not.toHaveBeenCalled();
  });

  it('goes loading -> redirecting on a 401, and starts the login redirect', async () => {
    const { c, seen, assign } = controller(async () => new Response('{}', { status: 401 }));
    await c.start();
    expect(seen).toEqual(['loading', 'loading', 'redirecting']);
    expect(assign).toHaveBeenCalledWith('/auth/login');
  });

  it('goes loading -> error on a failure, with no redirect', async () => {
    const { c, seen, assign } = controller(async () => new Response('{}', { status: 500 }));
    await c.start();
    expect(seen).toEqual(['loading', 'loading', 'error']);
    expect(c.state).toEqual({ kind: 'error', message: 'The session could not be read (HTTP 500).' });
    expect(assign).not.toHaveBeenCalled();
  });

  it('goes signed-in -> signed-out on Sign out: posts the logout with the CSRF token, moves to #/signed-out', async () => {
    const { c, seen, fetchFn, setHash } = controller(async () => Response.json(session));
    await c.start();
    fetchFn.mockImplementation(async () => new Response(null, { status: 204 }));
    expect(await c.signOut()).toBe('signed-out');
    expect(seen.at(-1)).toBe('signed-out');
    const [url, init] = fetchFn.mock.calls.at(-1)!;
    expect(url).toBe('/auth/logout');
    expect(init?.method).toBe('POST');
    expect(init?.headers).toEqual({ 'X-CSRF-Token': 'tok' });
    expect(setHash).toHaveBeenCalledWith('#/signed-out');
  });

  it('clears every vh.view.* key on Sign out and leaves the keys that are not the app\'s (R16)', async () => {
    const storage = memoryStorage({
      'vh.view.v1.tasks': '{"v":1,"query":"status=done"}',
      'vh.view.v1.initiative': '{"v":1,"query":"sort=ref"}',
      'vh.view.v2.tasks': 'a later version',
      'other': 'kept',
    });
    const { c, fetchFn, b } = controller(async () => Response.json(session));
    b.storage = storage;
    await c.start();
    fetchFn.mockImplementation(async () => new Response(null, { status: 204 }));
    expect(await c.signOut()).toBe('signed-out');
    expect(Object.keys(storage.data)).toEqual(['other']);
  });

  it('clears every board.* and vh.* key of both storages on Sign out: view state, theme and entry state (R3)', async () => {
    const local = memoryStorage({
      'board.view.v1.tasks': '{"v":1,"query":"status=done"}',
      'board.view.v2.tasks': 'a later version',
      'board.theme': 'light',
      'vh.view.v1.tasks': '{"v":1,"query":"status=done"}',
      'vh.view.v1.initiative': '{"v":1,"query":"sort=ref"}',
      'vh.view.v2.tasks': 'a later version',
      'vh.theme': 'dark',
      'other': 'kept',
    });
    const tab = memoryStorage({ 'board.entry.test': '{"t":1}', 'vh.entry.test': '{"t":2}', 'other': 'kept' });
    const { c, fetchFn, b } = controller(async () => Response.json(session));
    b.storage = local;
    b.sessionStorage = tab;
    await c.start();
    fetchFn.mockImplementation(async () => new Response(null, { status: 204 }));
    expect(await c.signOut()).toBe('signed-out');
    expect(Object.keys(local.data)).toEqual(['other']);
    expect(Object.keys(tab.data)).toEqual(['other']);
  });

  it('keeps the saved view state, the theme and the entry state when the logout fails', async () => {
    const storage = memoryStorage({ 'vh.view.v1.tasks': '{"v":1,"query":"status=done"}', 'vh.theme': 'dark' });
    const tab = memoryStorage({ 'vh.entry.test': '{"t":2}' });
    const { c, fetchFn, b } = controller(async () => Response.json(session));
    b.storage = storage;
    b.sessionStorage = tab;
    await c.start();
    fetchFn.mockImplementation(async () => new Response('{}', { status: 403 }));
    expect(await c.signOut()).toBe('failed');
    expect(Object.keys(storage.data)).toEqual(['vh.view.v1.tasks', 'vh.theme']);
    expect(Object.keys(tab.data)).toEqual(['vh.entry.test']);
  });

  it('stays signed in when the logout fails', async () => {
    const { c, fetchFn, setHash } = controller(async () => Response.json(session));
    await c.start();
    fetchFn.mockImplementation(async () => new Response('{}', { status: 403 }));
    expect(await c.signOut()).toBe('failed');
    expect(c.state.kind).toBe('signed-in');
    expect(setHash).not.toHaveBeenCalled();
  });

  it('has nothing to sign out of when no session is shown', async () => {
    const { c, fetchFn } = controller(async () => new Response('{}', { status: 500 }));
    await c.start();
    fetchFn.mockClear();
    expect(await c.signOut()).toBe('failed');
    expect(fetchFn).not.toHaveBeenCalled();
  });

  it('retry on the error card loads the session again: error -> loading -> signed-in', async () => {
    const { c, seen, fetchFn } = controller(async () => new Response('{}', { status: 503 }));
    await c.start();
    seen.length = 0;
    fetchFn.mockImplementation(async () => Response.json(session));
    await c.retry();
    expect(seen).toEqual(['loading', 'signed-in']);
    expect(fetchFn).toHaveBeenCalledTimes(2);
  });

  it('retry that fails again returns to the error card, with the problem detail of the new answer', async () => {
    const { c, seen, fetchFn } = controller(async () => new Response('{}', { status: 503 }));
    await c.start();
    seen.length = 0;
    fetchFn.mockImplementation(async () => Response.json({ detail: 'Still starting up.' }, { status: 503 }));
    await c.retry();
    expect(seen).toEqual(['loading', 'error']);
    expect(c.state).toEqual({ kind: 'error', message: 'Still starting up.' });
  });

  it('retry does nothing when the state is not an error', async () => {
    const { c, fetchFn } = controller(async () => Response.json(session));
    await c.start();
    fetchFn.mockClear();
    await c.retry();
    expect(fetchFn).not.toHaveBeenCalled();
    expect(c.state.kind).toBe('signed-in');
  });

  it('a page that opens on #/signed-out shows Signed out and never calls the session or the login redirect', async () => {
    const { c, seen, fetchFn, assign } = controller(async () => new Response('{}', { status: 401 }), SIGNED_OUT_HASH);
    await c.start();
    await c.retry();
    expect(seen).toEqual(['signed-out']);
    expect(fetchFn).not.toHaveBeenCalled();
    expect(assign).not.toHaveBeenCalled();
    await c.hashChanged(SIGNED_OUT_HASH);
    expect(fetchFn).not.toHaveBeenCalled();
    expect(assign).not.toHaveBeenCalled();
  });

  it('after Sign out the signed-out view stays put: no session call and no login redirect', async () => {
    const { c, fetchFn, assign } = controller(async () => Response.json(session));
    await c.start();
    fetchFn.mockImplementation(async () => new Response(null, { status: 204 }));
    await c.signOut();
    fetchFn.mockClear();
    await c.hashChanged(SIGNED_OUT_HASH);
    expect(fetchFn).not.toHaveBeenCalled();
    expect(assign).not.toHaveBeenCalled();
    expect(c.state.kind).toBe('signed-out');
  });

  it('leaving the Signed out view loads the session again (signed-out -> loading -> redirecting)', async () => {
    const { c, seen, assign } = controller(async () => new Response('{}', { status: 401 }), SIGNED_OUT_HASH);
    await c.start();
    await c.hashChanged('#/');
    expect(seen).toEqual(['signed-out', 'loading', 'redirecting']);
    expect(assign).toHaveBeenCalledWith('/auth/login');
  });

  it('opening #/signed-out while signed in is not a sign out: the page goes home', async () => {
    const { c, setHash, fetchFn } = controller(async () => Response.json(session));
    await c.start();
    fetchFn.mockClear();
    await c.hashChanged(SIGNED_OUT_HASH);
    expect(setHash).toHaveBeenCalledWith('#/');
    expect(c.state.kind).toBe('signed-in');
    expect(fetchFn).not.toHaveBeenCalled();
  });

  it('an ordinary fragment change while signed in does nothing', async () => {
    const { c, setHash, fetchFn } = controller(async () => Response.json(session));
    await c.start();
    fetchFn.mockClear();
    await c.hashChanged('#/settings/keys');
    expect(setHash).not.toHaveBeenCalled();
    expect(fetchFn).not.toHaveBeenCalled();
  });
});
