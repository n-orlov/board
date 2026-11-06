import { describe, expect, it, vi } from 'vitest';
import { KeysApi, KeysError, KeysStore, type CreatedKey, type Key, type KeysState } from './keys';
import type { Browser } from './session';

const key = (name: string, extra: Partial<Key> = {}): Key => ({
  id: 'id-' + name,
  name,
  display_prefix: 'abcd1234',
  status: 'active',
  created_at: '2026-01-01T00:00:00Z',
  created_by: 'oidc:op@example.test',
  ...extra,
});

function api(respond: (url: string, init?: RequestInit) => Promise<Response>, pathname = '/') {
  const fetchFn = vi.fn((input: string, init?: RequestInit) => respond(input, init));
  const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname, setHash: vi.fn() };
  return { api: new KeysApi(browser, () => 'csrf-1'), fetchFn };
}

const problem = (status: number, body: object) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/problem+json' } });

describe('KeysApi.list', () => {
  it('reads the items with the session cookie and no CSRF header', async () => {
    const { api: a, fetchFn } = api(async () => Response.json({ items: [key('a'), key('b')] }));
    expect((await a.list()).map((k) => k.name)).toEqual(['a', 'b']);
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/api/v1/keys');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.method).toBeUndefined();
    expect(init?.headers).toEqual({ Accept: 'application/json' });
  });

  it('asks under the mount prefix', async () => {
    const { api: a, fetchFn } = api(async () => Response.json({ items: [] }), '/board/');
    await a.list();
    expect(fetchFn.mock.calls[0][0]).toBe('/board/api/v1/keys');
  });
});

describe('KeysApi.create', () => {
  const created: CreatedKey = { ...key('ci'), key: 'brd_secret' };

  it('posts the name and expiry with the CSRF token and the session cookie', async () => {
    const { api: a, fetchFn } = api(async () => Response.json(created, { status: 201 }));
    expect(await a.create('ci', '90d')).toEqual(created);
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/api/v1/keys');
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.headers).toEqual({ Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': 'csrf-1' });
    expect(JSON.parse(init?.body as string)).toEqual({ name: 'ci', expires_in: '90d' });
  });

  it('leaves expires_in out when the expiry is blank', async () => {
    const { api: a, fetchFn } = api(async () => Response.json(created, { status: 201 }));
    await a.create('ci', '');
    expect(JSON.parse(fetchFn.mock.calls[0][1]?.body as string)).toEqual({ name: 'ci' });
  });

  it('raises the problem detail the server gave', async () => {
    const { api: a } = api(async () => problem(409, { detail: 'a key named "ci" exists', title: 'Conflict' }));
    await expect(a.create('ci', '')).rejects.toMatchObject({ name: 'KeysError', status: 409, message: 'a key named "ci" exists' });
  });
});

describe('KeysApi.revoke', () => {
  it('posts to the key name, escaped, with the CSRF token', async () => {
    const { api: a, fetchFn } = api(async () => Response.json(key('a b', { status: 'revoked' })));
    expect((await a.revoke('a b')).status).toBe('revoked');
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/api/v1/keys/a%20b/revoke');
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.headers).toEqual({ Accept: 'application/json', 'X-CSRF-Token': 'csrf-1' });
    expect(init?.body).toBeUndefined();
  });
});

describe('errors', () => {
  it('falls back to the title when there is no detail', async () => {
    const { api: a } = api(async () => problem(403, { title: 'Forbidden' }));
    await expect(a.list()).rejects.toMatchObject({ status: 403, message: 'Forbidden' });
  });

  it('names the action and the status when the body is not a problem document', async () => {
    const { api: a } = api(async () => new Response('<html>', { status: 502 }));
    await expect(a.list()).rejects.toMatchObject({ status: 502, message: 'Listing the keys failed (HTTP 502).' });
    await expect(a.create('x', '')).rejects.toMatchObject({ message: 'Creating the key failed (HTTP 502).' });
    await expect(a.revoke('x')).rejects.toMatchObject({ message: 'Revoking the key failed (HTTP 502).' });
  });

  it('names the status when the problem document has neither detail nor title', async () => {
    const { api: a } = api(async () => problem(500, {}));
    await expect(a.list()).rejects.toMatchObject({ message: 'Listing the keys failed (HTTP 500).' });
  });

  it('reports an unreachable server as status 0', async () => {
    const { api: a } = api(async () => {
      throw new TypeError('network');
    });
    const err = await a.list().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(KeysError);
    expect(err).toMatchObject({ status: 0, message: 'The server is not reachable.' });
  });
});

function store(overrides: Partial<{ list: () => Promise<Key[]>; create: (n: string, e: string) => Promise<CreatedKey>; revoke: (n: string) => Promise<Key> }> = {}) {
  const fake = {
    list: vi.fn(overrides.list ?? (async () => [key('a')])),
    create: vi.fn(overrides.create ?? (async (name: string) => ({ ...key(name), key: 'brd_once' }))),
    revoke: vi.fn(overrides.revoke ?? (async (name: string) => key(name, { status: 'revoked' }))),
  };
  const seen: KeysState[] = [];
  const s = new KeysStore(fake, (state) => seen.push(state));
  return { s, fake, seen };
}

describe('KeysStore', () => {
  it('starts loading with nothing to show', () => {
    expect(store().s.state).toEqual({ keys: [], loading: true, created: undefined, error: undefined, formError: undefined });
  });

  it('load fills the list and stops loading', async () => {
    const { s, seen } = store();
    await s.load();
    expect(s.state.keys.map((k) => k.name)).toEqual(['a']);
    expect(s.state.loading).toBe(false);
    expect(seen).toHaveLength(1);
    expect(seen[0]).toBe(s.state);
  });

  it('load shows the error of a failed call and keeps the list it had', async () => {
    let fail = false;
    const { s } = store({
      list: async () => {
        if (fail) throw new KeysError(500, 'boom');
        return [key('a')];
      },
    });
    await s.load();
    fail = true;
    await s.load();
    expect(s.state.error).toBe('boom');
    expect(s.state.loading).toBe(false);
    expect(s.state.keys).toHaveLength(1);
  });

  it('shows a non-Error rejection as text', async () => {
    const { s } = store({
      list: () => Promise.reject('plain'),
    });
    await s.load();
    expect(s.state.error).toBe('plain');
  });

  it('create trims the inputs, keeps the new key to show once, and reloads the list', async () => {
    const { s, fake } = store();
    expect(await s.create('  ci  ', ' 90d ')).toBe(true);
    expect(fake.create).toHaveBeenCalledWith('ci', '90d');
    expect(s.state.created?.key).toBe('brd_once');
    expect(fake.list).toHaveBeenCalledTimes(1);
    expect(s.state.error).toBeUndefined();
  });

  it('create that is refused keeps no key, shows the reason in the form and does not reload', async () => {
    const { s, fake } = store({
      create: async () => {
        throw new KeysError(409, 'exists');
      },
    });
    expect(await s.create('ci', '')).toBe(false);
    expect(s.state.created).toBeUndefined();
    expect(s.state.formError).toBe('exists');
    expect(s.state.error).toBeUndefined();
    expect(fake.list).not.toHaveBeenCalled();
  });

  it('a refused create keeps the key already on show and a good create clears the refusal', async () => {
    let refuse = false;
    const { s } = store({
      create: async (name: string) => {
        if (refuse) throw new KeysError(409, 'exists');
        return { ...key(name), key: 'brd_once' };
      },
    });
    await s.create('a', '');
    refuse = true;
    await s.create('a', '');
    expect(s.state.created?.key).toBe('brd_once');
    expect(s.state.formError).toBe('exists');
    refuse = false;
    await s.create('b', '');
    expect(s.state.formError).toBeUndefined();
  });

  it('clearFormError forgets a refusal and only that', async () => {
    const { s } = store({
      create: async () => {
        throw new KeysError(409, 'exists');
      },
      list: async () => {
        throw new KeysError(500, 'boom');
      },
    });
    await s.load();
    await s.create('a', '');
    s.clearFormError();
    expect(s.state.formError).toBeUndefined();
    expect(s.state.error).toBe('boom');
  });

  it('dismiss drops the one-time key', async () => {
    const { s } = store();
    await s.create('ci', '');
    s.dismiss();
    expect(s.state.created).toBeUndefined();
  });

  it('the list reload after a create keeps the key on show', async () => {
    const { s } = store();
    await s.create('ci', '');
    await s.load();
    expect(s.state.created?.key).toBe('brd_once');
  });

  it('revoke calls the API, drops a shown key and reloads', async () => {
    const { s, fake } = store();
    await s.create('ci', '');
    expect(await s.revoke('ci')).toBe(true);
    expect(fake.revoke).toHaveBeenCalledWith('ci');
    expect(s.state.created).toBeUndefined();
    expect(fake.list).toHaveBeenCalledTimes(2);
  });

  it('revoke that fails shows the error, keeps the shown key and does not reload', async () => {
    const { s, fake } = store({
      revoke: async () => {
        throw new KeysError(404, 'no such key');
      },
    });
    await s.create('ci', '');
    fake.list.mockClear();
    expect(await s.revoke('nope')).toBe(false);
    expect(s.state.error).toBe('no such key');
    expect(s.state.created?.key).toBe('brd_once');
    expect(fake.list).not.toHaveBeenCalled();
  });

  it('a later action clears the error of an earlier one', async () => {
    let fail = true;
    const { s } = store({
      list: async () => {
        if (fail) throw new KeysError(500, 'boom');
        return [];
      },
    });
    await s.load();
    expect(s.state.error).toBe('boom');
    fail = false;
    await s.load();
    expect(s.state.error).toBeUndefined();
  });
});
