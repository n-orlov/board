import { describe, expect, it, vi } from 'vitest';
import { InitiativesApi, InitiativesError, InitiativesStore, toCreate, type Initiative, type InitiativesState } from './initiatives';
import type { Browser } from './session';

const initiative = (slug: string, extra: Partial<Initiative> = {}): Initiative => ({
  id: 'id-' + slug,
  ref: slug,
  slug,
  title: 'Title ' + slug,
  description: '',
  status: 'open',
  tags: [],
  created_by: 'operator',
  created_at: '2026-01-01T00:00:00Z',
  ...extra,
});

function api(respond: (url: string, init?: RequestInit) => Promise<Response>, pathname = '/') {
  const fetchFn = vi.fn((input: string, init?: RequestInit) => respond(input, init));
  const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname };
  return { api: new InitiativesApi(browser, () => 'csrf-1'), fetchFn };
}

const problem = (status: number, body: object) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/problem+json' } });

const form = { slug: ' alpha ', title: ' Alpha ', description: '', tags: '' };

describe('toCreate', () => {
  it('trims the slug and title and leaves blank optional fields out', () => {
    expect(toCreate(form)).toEqual({ slug: 'alpha', title: 'Alpha' });
  });

  it('keeps a trimmed description and splits the tags on commas, dropping blanks', () => {
    expect(toCreate({ ...form, description: ' why ', tags: ' a, b ,, c ,' })).toEqual({
      slug: 'alpha',
      title: 'Alpha',
      description: 'why',
      tags: ['a', 'b', 'c'],
    });
  });

  it('leaves tags out when only separators were typed', () => {
    expect(toCreate({ ...form, tags: ' , ,' })).toEqual({ slug: 'alpha', title: 'Alpha' });
  });
});

describe('InitiativesApi.list', () => {
  it('reads the items with the session cookie and no CSRF header', async () => {
    const { api: a, fetchFn } = api(async () => Response.json({ items: [initiative('a'), initiative('b')] }));
    expect((await a.list()).map((i) => i.slug)).toEqual(['a', 'b']);
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/api/v1/initiatives');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.method).toBeUndefined();
    expect(init?.headers).toEqual({ Accept: 'application/json' });
  });

  it('asks under the mount prefix', async () => {
    const { api: a, fetchFn } = api(async () => Response.json({ items: [] }), '/board/');
    await a.list();
    expect(fetchFn.mock.calls[0][0]).toBe('/board/api/v1/initiatives');
  });

  it('follows next_after until the last page', async () => {
    const { api: a, fetchFn } = api(async (url) =>
      url.includes('after=a%20b')
        ? Response.json({ items: [initiative('c')] })
        : Response.json({ items: [initiative('a'), initiative('b')], next_after: 'a b' }),
    );
    expect((await a.list()).map((i) => i.slug)).toEqual(['a', 'b', 'c']);
    expect(fetchFn.mock.calls.map((c) => c[0])).toEqual(['/api/v1/initiatives', '/api/v1/initiatives?after=a%20b']);
  });

  it('reports an unreachable server with status 0', async () => {
    const { api: a } = api(async () => {
      throw new TypeError('network');
    });
    await expect(a.list()).rejects.toMatchObject({ name: 'InitiativesError', status: 0, message: 'The server is not reachable.' });
  });
});

describe('InitiativesApi.create', () => {
  it('posts the body with the CSRF token and the session cookie', async () => {
    const made = initiative('alpha');
    const { api: a, fetchFn } = api(async () => Response.json(made, { status: 201 }));
    expect(await a.create({ slug: 'alpha', title: 'Alpha' })).toEqual(made);
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/api/v1/initiatives');
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.headers).toEqual({ Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': 'csrf-1' });
    expect(JSON.parse(init?.body as string)).toEqual({ slug: 'alpha', title: 'Alpha' });
  });

  it('throws the problem document detail with the HTTP status', async () => {
    const { api: a } = api(async () => problem(400, { title: 'Bad Request', detail: 'slug must be 2 to 40 characters' }));
    const error = await a.create({ slug: 'x', title: 'X' }).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(InitiativesError);
    expect(error).toMatchObject({ status: 400, message: 'slug must be 2 to 40 characters' });
  });

  it('falls back to the title, then to a line naming the status', async () => {
    const titled = api(async () => problem(409, { title: 'Conflict' }));
    await expect(titled.api.create({ slug: 'xx', title: 'X' })).rejects.toMatchObject({ status: 409, message: 'Conflict' });
    const plain = api(async () => new Response('boom', { status: 500 }));
    await expect(plain.api.create({ slug: 'xx', title: 'X' })).rejects.toMatchObject({
      status: 500,
      message: 'Creating the initiative failed (HTTP 500).',
    });
  });
});

describe('InitiativesStore', () => {
  function store(fake: { list: () => Promise<Initiative[]>; create: (b: unknown) => Promise<Initiative> }) {
    const seen: InitiativesState[] = [];
    const s = new InitiativesStore(fake as never, (st) => seen.push(st));
    return { s, seen };
  }

  it('starts loading with nothing to show', () => {
    const { s } = store({ list: async () => [], create: async () => initiative('a') });
    expect(s.state).toEqual({ initiatives: [], loading: true, error: undefined });
  });

  it('load shows the list and stops loading', async () => {
    const { s, seen } = store({ list: async () => [initiative('a')], create: async () => initiative('a') });
    await s.load();
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['a']);
    expect(s.state.loading).toBe(false);
    expect(seen).toHaveLength(1);
  });

  it('load keeps the error text when the list fails, and a non-Error is stringified', async () => {
    const a = store({ list: async () => Promise.reject(new InitiativesError(503, 'down')), create: async () => initiative('a') });
    await a.s.load();
    expect(a.s.state).toMatchObject({ loading: false, error: 'down' });
    const b = store({ list: async () => Promise.reject('plain'), create: async () => initiative('a') });
    await b.s.load();
    expect(b.s.state.error).toBe('plain');
  });

  it('create sends the cleaned body, reloads the list and clears an earlier error', async () => {
    const create = vi.fn(async (_body: unknown) => initiative('alpha'));
    let items: Initiative[] = [];
    const { s } = store({ list: async () => items, create });
    await s.load();
    items = [initiative('alpha')];
    expect(await s.create({ ...form, tags: 'x, y' })).toBe(true);
    expect(create).toHaveBeenCalledWith({ slug: 'alpha', title: 'Alpha', tags: ['x', 'y'] });
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['alpha']);
    expect(s.state.error).toBeUndefined();
  });

  it('a refused create shows the server text on the form and does not reload', async () => {
    const list = vi.fn(async () => [initiative('keep')]);
    const { s } = store({ list, create: async () => Promise.reject(new InitiativesError(400, 'slug must be 2 to 40 characters')) });
    await s.load();
    expect(await s.create({ ...form, slug: 'x' })).toBe(false);
    expect(s.state.error).toBe('slug must be 2 to 40 characters');
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['keep']);
    expect(list).toHaveBeenCalledTimes(1);
  });
});
