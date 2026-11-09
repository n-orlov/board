import { describe, expect, it, vi } from 'vitest';
import { affectsKeys, KeysApi, KeysError, KeysStore, type CreatedKey, type Key, type KeysState } from './keys';
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
    expect(seen).toHaveLength(2);
    expect(seen[0]?.loading).toBe(true);
    expect(seen[1]).toBe(s.state);
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

describe('KeysStore loading', () => {
  function gated() {
    const gates: { resolve: (k: Key[]) => void; reject: (e: unknown) => void }[] = [];
    const list = () => new Promise<Key[]>((resolve, reject) => gates.push({ resolve, reject }));
    return { gates, list };
  }
  const tick = () => new Promise((r) => setTimeout(r, 0));

  it('the first fetch is loading while pending and stops on success', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const done = s.load();
    await tick();
    expect(s.state.loading).toBe(true);
    gates[0]!.resolve([key('a')]);
    await done;
    expect(s.state).toMatchObject({ loading: false, keys: [key('a')] });
  });

  it('the refresh after a create is loading while pending, keeps the key on show, and stops on success', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const first = s.load();
    gates[0]!.resolve([]);
    await first;
    expect(s.state.loading).toBe(false);
    const done = s.create('ci', '');
    await tick();
    expect(s.state.loading).toBe(true);
    expect(s.state.created?.key).toBe('brd_once');
    gates[1]!.resolve([key('ci')]);
    expect(await done).toBe(true);
    expect(s.state.loading).toBe(false);
    expect(s.state.keys).toEqual([key('ci')]);
    expect(s.state.created?.key).toBe('brd_once');
  });

  it('the refresh after a revoke is loading while pending and stops when it fails', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const first = s.load();
    gates[0]!.resolve([key('a')]);
    await first;
    const done = s.revoke('a');
    await tick();
    expect(s.state.loading).toBe(true);
    gates[1]!.reject(new KeysError(503, 'down'));
    expect(await done).toBe(true);
    expect(s.state).toMatchObject({ loading: false, error: 'down' });
  });

  it('a revoke or create that rejects with plain text shows that text', async () => {
    const { s } = store({ revoke: () => Promise.reject('plain') });
    expect(await s.revoke('a')).toBe(false);
    expect(s.state.error).toBe('plain');
  });

  it('a manual reload is loading while pending and a refused create or revoke never starts one', async () => {
    const { gates, list } = gated();
    const { s } = store({
      list,
      create: async () => {
        throw new KeysError(409, 'exists');
      },
      revoke: async () => {
        throw new KeysError(404, 'gone');
      },
    });
    const first = s.load();
    gates[0]!.resolve([key('a')]);
    await first;
    await s.create('a', '');
    await s.revoke('a');
    expect(s.state.loading).toBe(false);
    expect(gates).toHaveLength(1);
    const again = s.load();
    await tick();
    expect(s.state.loading).toBe(true);
    gates[1]!.resolve([key('a')]);
    await again;
    expect(s.state.loading).toBe(false);
  });

  it('a create submitted while the first fetch is pending: its refresh landing first leaves the list loading until the first lands too', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const first = s.load();
    await tick();
    const done = s.create('ci', '');
    await tick();
    expect(gates).toHaveLength(2);
    gates[1]!.resolve([key('ci')]);
    expect(await done).toBe(true);
    expect(s.state.loading).toBe(true);
    expect(s.state.created?.key).toBe('brd_once');
    gates[0]!.resolve([]);
    await first;
    // The first fetch's older list is not shown over the refresh's; the key stays on show.
    expect(s.state).toMatchObject({ loading: false, error: undefined, keys: [key('ci')] });
    expect(s.state.created?.key).toBe('brd_once');
  });

  it('a revoke refresh and a create refresh in flight together keep the list loading until both land', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const first = s.load();
    gates[0]!.resolve([key('a')]);
    await first;
    const revoked = s.revoke('a');
    await tick();
    const created = s.create('ci', '');
    await tick();
    expect(gates).toHaveLength(3);
    gates[1]!.resolve([{ ...key('a'), status: 'revoked' }]);
    expect(await revoked).toBe(true);
    expect(s.state.loading).toBe(true);
    gates[2]!.reject(new KeysError(503, 'down'));
    expect(await created).toBe(true);
    expect(s.state).toMatchObject({ loading: false, error: 'down', keys: [{ ...key('a'), status: 'revoked' }] });
  });

  it('a first fetch that fails while a create refresh is pending leaves the list loading, and the refresh clears the error', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const first = s.load();
    await tick();
    const done = s.create('ci', '');
    await tick();
    gates[0]!.reject(new KeysError(500, 'boom'));
    await first;
    expect(s.state).toMatchObject({ loading: true, error: 'boom' });
    gates[1]!.resolve([key('ci')]);
    expect(await done).toBe(true);
    expect(s.state).toMatchObject({ loading: false, error: undefined, keys: [key('ci')] });
  });

  it('three overlapping reloads landing middle, last, first leave the list loading until the third lands and show the newest list', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const a = s.load();
    const b = s.load();
    const c = s.load();
    await tick();
    gates[1]!.resolve([key('b')]);
    await b;
    expect(s.state).toMatchObject({ loading: true });
    gates[2]!.resolve([key('c')]);
    await c;
    expect(s.state).toMatchObject({ loading: true, keys: [key('c')] });
    gates[0]!.resolve([key('a')]);
    await a;
    expect(s.state).toMatchObject({ loading: false, error: undefined, keys: [key('c')] });
  });

  it('an older fetch that succeeds after a newer one failed neither hides the error nor replaces the list', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const first = s.load();
    gates[0]!.resolve([key('keep')]);
    await first;
    const a = s.load();
    const b = s.load();
    await tick();
    gates[2]!.reject(new KeysError(500, 'newer'));
    await b;
    gates[1]!.resolve([key('stale')]);
    await a;
    expect(s.state).toMatchObject({ loading: false, error: 'newer', keys: [key('keep')] });
  });
});

describe('KeysStore refresh (a live event)', () => {
  function gated() {
    const gates: { resolve: (k: Key[]) => void; reject: (e: unknown) => void }[] = [];
    const list = () => new Promise<Key[]>((resolve, reject) => gates.push({ resolve, reject }));
    return { gates, list };
  }
  const tick = () => new Promise((r) => setTimeout(r, 0));

  it('swaps the list in without making it loading, and keeps the one-time key on show', async () => {
    const { gates, list } = gated();
    const { s, seen } = store({ list });
    const first = s.create('ci', '');
    await tick();
    gates[0]!.resolve([key('a')]);
    await first;
    expect(s.state.created?.key).toBe('brd_once');
    seen.length = 0;
    const again = s.refresh();
    await tick();
    expect(s.state.keys.map((k) => k.name)).toEqual(['a']);
    gates[1]!.resolve([key('a'), key('b')]);
    await again;
    expect(s.state.keys.map((k) => k.name)).toEqual(['a', 'b']);
    expect(s.state).toMatchObject({ loading: false, error: undefined });
    expect(s.state.created?.key).toBe('brd_once');
    expect(seen.map((x) => x.loading)).not.toContain(true);
  });

  it('keeps the keys it had when the read fails and says why', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const first = s.load();
    gates[0]!.resolve([key('a')]);
    await first;
    const again = s.refresh();
    gates[1]!.reject(new KeysError(503, 'down'));
    await again;
    expect(s.state).toMatchObject({ loading: false, error: 'down' });
    expect(s.state.keys.map((k) => k.name)).toEqual(['a']);
  });

  it('drops an older read that lands after a newer one', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const first = s.refresh();
    const second = s.refresh();
    gates[1]!.resolve([key('new')]);
    await second;
    gates[0]!.resolve([key('old')]);
    await first;
    expect(s.state.keys.map((k) => k.name)).toEqual(['new']);
  });

  it('a first load still pending keeps the list loading after a refresh lands', async () => {
    const { gates, list } = gated();
    const { s } = store({ list });
    const first = s.load();
    await tick();
    const again = s.refresh();
    gates[1]!.resolve([key('x')]);
    await again;
    expect(s.state.loading).toBe(true);
    gates[0]!.resolve([key('old')]);
    await first;
    expect(s.state.loading).toBe(false);
    expect(s.state.keys.map((k) => k.name)).toEqual(['x']);
  });
});

describe('affectsKeys', () => {
  it('is true for the key events, false for the rest', () => {
    expect(affectsKeys({ type: 'key_created' })).toBe(true);
    expect(affectsKeys({ type: 'key_revoked' })).toBe(true);
    expect(affectsKeys({ type: 'note_posted' })).toBe(false);
    expect(affectsKeys({ type: 'task_done' })).toBe(false);
  });
});
