import { describe, expect, it, vi } from 'vitest';
import { InitiativesApi, InitiativesError, InitiativesStore, affectsInitiatives, parseTags, toCreate, toEditForm, toPatch, type Initiative, type InitiativesState } from './initiatives';
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
  const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname, setHash: vi.fn() };
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

describe('parseTags', () => {
  it('splits on commas, trims and drops blanks', () => {
    expect(parseTags(' a, b ,, c ,')).toEqual(['a', 'b', 'c']);
    expect(parseTags('')).toEqual([]);
  });
});

describe('toEditForm and toPatch', () => {
  const edit = { title: ' T ', description: ' d ', tags: 'x, y', targetStart: ' 2026-01-02 ', targetEnd: '' };

  it('fills the form from the initiative: tags on one line, an unset target blank', () => {
    expect(toEditForm(initiative('aa', { title: 'T', description: 'd', tags: ['x', 'y'], target_start: '2026-01-02' }))).toEqual({
      title: 'T',
      description: 'd',
      tags: 'x, y',
      targetStart: '2026-01-02',
      targetEnd: '',
    });
    expect(toEditForm(initiative('aa', { target_end: '2026-02-03' })).targetEnd).toBe('2026-02-03');
  });

  it('sends every field: text trimmed, the tags as a list, a blank target as null so it is cleared', () => {
    expect(toPatch(edit)).toEqual({ title: 'T', description: 'd', tags: ['x', 'y'], target_start: '2026-01-02', target_end: null });
    expect(toPatch({ ...edit, targetStart: '  ', targetEnd: '2026-03-04', tags: '', description: '' })).toEqual({
      title: 'T',
      description: '',
      tags: [],
      target_start: null,
      target_end: '2026-03-04',
    });
  });
});

describe('InitiativesApi.patch, close and reopen', () => {
  it('patch sends the body with the CSRF token on the encoded slug', async () => {
    const { api: a, fetchFn } = api(async () => Response.json(initiative('a/b')));
    await a.patch('a/b', { title: 'N' });
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/api/v1/initiatives/a%2Fb');
    expect(init?.method).toBe('PATCH');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.headers).toEqual({ Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': 'csrf-1' });
    expect(JSON.parse(init?.body as string)).toEqual({ title: 'N' });
  });

  it('close posts an empty object, or force true when forcing', async () => {
    const { api: a, fetchFn } = api(async () => Response.json(initiative('aa')));
    await a.close('aa', false);
    await a.close('aa', true);
    expect(fetchFn.mock.calls.map((c) => c[0])).toEqual(['/api/v1/initiatives/aa/close', '/api/v1/initiatives/aa/close']);
    expect(fetchFn.mock.calls.map((c) => c[1]?.method)).toEqual(['POST', 'POST']);
    expect(fetchFn.mock.calls.map((c) => JSON.parse(c[1]?.body as string))).toEqual([{}, { force: true }]);
    expect(fetchFn.mock.calls[0][1]?.headers).toEqual({ Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': 'csrf-1' });
  });

  it('reopen posts without a body or a content type', async () => {
    const { api: a, fetchFn } = api(async () => Response.json(initiative('aa')));
    expect((await a.reopen('aa')).slug).toBe('aa');
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/api/v1/initiatives/aa/reopen');
    expect(init?.method).toBe('POST');
    expect(init?.body).toBeUndefined();
    expect(init?.headers).toEqual({ Accept: 'application/json', 'X-CSRF-Token': 'csrf-1' });
  });

  it('a refusal throws the problem detail with its status, each with its own fallback', async () => {
    const refuse = api(async () => problem(409, { title: 'Conflict', detail: 'initiative aa has open tasks' }));
    await expect(refuse.api.close('aa', false)).rejects.toMatchObject({ status: 409, message: 'initiative aa has open tasks' });
    const plain = api(async () => new Response('x', { status: 500 }));
    await expect(plain.api.patch('aa', {})).rejects.toMatchObject({ message: 'Editing the initiative failed (HTTP 500).' });
    await expect(plain.api.close('aa', false)).rejects.toMatchObject({ message: 'Closing the initiative failed (HTTP 500).' });
    await expect(plain.api.reopen('aa')).rejects.toMatchObject({ message: 'Reopening the initiative failed (HTTP 500).' });
  });
});

describe('InitiativesApi.list', () => {
  it('reads the items with the session cookie and no CSRF header', async () => {
    const { api: a, fetchFn } = api(async () => Response.json({ items: [initiative('a'), initiative('b')] }));
    expect((await a.list()).map((i) => i.slug)).toEqual(['a', 'b']);
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe('/api/v1/initiatives?status=open');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.method).toBeUndefined();
    expect(init?.headers).toEqual({ Accept: 'application/json' });
  });

  it('asks under the mount prefix', async () => {
    const { api: a, fetchFn } = api(async () => Response.json({ items: [] }), '/board/');
    await a.list();
    expect(fetchFn.mock.calls[0][0]).toBe('/board/api/v1/initiatives?status=open');
  });

  it('follows next_after until the last page', async () => {
    const { api: a, fetchFn } = api(async (url) =>
      url.includes('after=a+b')
        ? Response.json({ items: [initiative('c')] })
        : Response.json({ items: [initiative('a'), initiative('b')], next_after: 'a b' }),
    );
    expect((await a.list()).map((i) => i.slug)).toEqual(['a', 'b', 'c']);
    expect(fetchFn.mock.calls.map((c) => c[0])).toEqual(['/api/v1/initiatives?status=open', '/api/v1/initiatives?status=open&after=a+b']);
  });

  it.each(['open', 'closed', 'all'] as const)('sends the %s filter as the status query', async (filter) => {
    const { api: a, fetchFn } = api(async () => Response.json({ items: [] }));
    await a.list(filter);
    expect(fetchFn.mock.calls[0][0]).toBe(`/api/v1/initiatives?status=${filter}`);
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

describe('InitiativesStore filter', () => {
  it('setFilter keeps the filter, fetches under it and shows what that fetch returned', async () => {
    const list = vi.fn(async (filter: string) => (filter === 'closed' ? [initiative('old', { status: 'closed' })] : [initiative('new')]));
    const s = new InitiativesStore({ list, create: async () => initiative('a') } as never, () => undefined);
    await s.setFilter('closed');
    expect(list).toHaveBeenLastCalledWith('closed');
    expect(s.state.filter).toBe('closed');
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['old']);
    await s.setFilter('all');
    expect(list).toHaveBeenLastCalledWith('all');
    expect(s.state.filter).toBe('all');
  });

  it('a reload after a create uses the filter in force', async () => {
    const list = vi.fn(async (_filter: string) => []);
    const s = new InitiativesStore({ list, create: async () => initiative('a') } as never, () => undefined);
    await s.setFilter('all');
    await s.create({ slug: 'aa', title: 'A', description: '', tags: '' });
    expect(list.mock.calls.map((c) => c[0])).toEqual(['all', 'all']);
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
    expect(s.state).toEqual({ initiatives: [], filter: 'open', loading: true, error: undefined, formError: undefined });
  });

  it('load shows the list and stops loading', async () => {
    const { s, seen } = store({ list: async () => [initiative('a')], create: async () => initiative('a') });
    await s.load();
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['a']);
    expect(s.state.loading).toBe(false);
    expect(seen.map((st) => st.loading)).toEqual([true, false]);
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
    expect(s.state.formError).toBeUndefined();
  });

  it('a refused create shows the server text on the form and does not reload', async () => {
    const list = vi.fn(async () => [initiative('keep')]);
    const { s } = store({ list, create: async () => Promise.reject(new InitiativesError(400, 'slug must be 2 to 40 characters')) });
    await s.load();
    expect(await s.create({ ...form, slug: 'x' })).toBe(false);
    expect(s.state.formError).toBe('slug must be 2 to 40 characters');
    expect(s.state.error).toBeUndefined();
    expect(s.state.loading).toBe(false);
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['keep']);
    expect(list).toHaveBeenCalledTimes(1);
  });

  it('a refusal is dropped by clearFormError and by the next successful create, and a non-Error is stringified', async () => {
    let refuse = true;
    const { s } = store({
      list: async () => [],
      create: async () => (refuse ? Promise.reject('plain') : initiative('a')),
    });
    expect(await s.create(form)).toBe(false);
    expect(s.state.formError).toBe('plain');
    s.clearFormError();
    expect(s.state.formError).toBeUndefined();
    expect(await s.create(form)).toBe(false);
    refuse = false;
    expect(await s.create(form)).toBe(true);
    expect(s.state.formError).toBeUndefined();
  });

  it('a refusal while the first load is still running keeps the list loading', async () => {
    const { s } = store({ list: async () => [], create: async () => Promise.reject(new InitiativesError(409, 'slug taken')) });
    expect(await s.create(form)).toBe(false);
    expect(s.state.loading).toBe(true);
  });
});

describe('InitiativesStore loading', () => {
  function gated() {
    const gates: { resolve: (i: Initiative[]) => void; reject: (e: unknown) => void }[] = [];
    const list = () => new Promise<Initiative[]>((resolve, reject) => gates.push({ resolve, reject }));
    return { gates, list };
  }
  const tick = () => new Promise((r) => setTimeout(r, 0));
  const make = (list: () => Promise<Initiative[]>, create: () => Promise<Initiative> = async () => initiative('a')) => new InitiativesStore({ list, create } as never, () => undefined);

  it('the first fetch is loading while pending and stops on success', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const done = s.load();
    await tick();
    expect(s.state.loading).toBe(true);
    gates[0]!.resolve([initiative('a')]);
    await done;
    expect(s.state).toMatchObject({ loading: false, error: undefined });
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['a']);
  });

  it('the first fetch stops loading when it fails', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const done = s.load();
    await tick();
    gates[0]!.reject(new InitiativesError(500, 'boom'));
    await done;
    expect(s.state).toMatchObject({ loading: false, error: 'boom' });
  });

  it('the refresh after a create is loading while pending and stops on success', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const first = s.load();
    gates[0]!.resolve([]);
    await first;
    const done = s.create(form);
    await tick();
    expect(s.state.loading).toBe(true);
    expect(s.state.formError).toBeUndefined();
    gates[1]!.resolve([initiative('alpha')]);
    expect(await done).toBe(true);
    expect(s.state).toMatchObject({ loading: false });
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['alpha']);
  });

  it('the refresh after a create stops loading when it fails, and a refused create starts no fetch', async () => {
    const { gates, list } = gated();
    let refuse = true;
    const s = make(list, async () => (refuse ? Promise.reject(new InitiativesError(409, 'taken')) : initiative('a')));
    const first = s.load();
    gates[0]!.resolve([initiative('keep')]);
    await first;
    expect(await s.create(form)).toBe(false);
    expect(s.state.loading).toBe(false);
    expect(gates).toHaveLength(1);
    refuse = false;
    const done = s.create(form);
    await tick();
    expect(s.state.loading).toBe(true);
    gates[1]!.reject(new InitiativesError(503, 'down'));
    expect(await done).toBe(true);
    expect(s.state).toMatchObject({ loading: false, error: 'down' });
  });

  it('a manual reload is loading while pending', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const first = s.load();
    gates[0]!.resolve([]);
    await first;
    const again = s.load();
    await tick();
    expect(s.state.loading).toBe(true);
    gates[1]!.resolve([]);
    await again;
    expect(s.state.loading).toBe(false);
  });

  it('a create submitted while the first fetch is pending: its refresh landing first leaves the list loading until the first lands too', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const first = s.load();
    await tick();
    const done = s.create(form);
    await tick();
    expect(gates).toHaveLength(2);
    gates[1]!.resolve([initiative('alpha')]);
    expect(await done).toBe(true);
    expect(s.state.loading).toBe(true);
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['alpha']);
    gates[0]!.resolve([]);
    await first;
    // The first fetch's older list is not shown over the refresh's.
    expect(s.state).toMatchObject({ loading: false, error: undefined });
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['alpha']);
  });

  it('a first fetch that fails while a refresh is pending leaves the list loading, and the refresh clears the error', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const first = s.load();
    await tick();
    const done = s.create(form);
    await tick();
    gates[0]!.reject(new InitiativesError(503, 'down'));
    await first;
    expect(s.state).toMatchObject({ loading: true, error: 'down' });
    gates[1]!.resolve([initiative('alpha')]);
    expect(await done).toBe(true);
    expect(s.state).toMatchObject({ loading: false, error: undefined });
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['alpha']);
  });

  it('overlapping reloads that both fail leave the list loading until the last lands, then show the newest error', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const a = s.load();
    const b = s.load();
    await tick();
    gates[1]!.reject(new InitiativesError(500, 'newer'));
    await b;
    expect(s.state).toMatchObject({ loading: true, error: 'newer' });
    gates[0]!.reject(new InitiativesError(500, 'older'));
    await a;
    expect(s.state).toMatchObject({ loading: false, error: 'newer' });
  });

  it('three overlapping reloads landing middle, last, first leave the list loading until the third lands and show the newest list', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const a = s.load();
    const b = s.load();
    const c = s.load();
    await tick();
    gates[1]!.resolve([initiative('b')]);
    await b;
    expect(s.state).toMatchObject({ loading: true });
    gates[2]!.resolve([initiative('c')]);
    await c;
    expect(s.state).toMatchObject({ loading: true });
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['c']);
    gates[0]!.resolve([initiative('a')]);
    await a;
    expect(s.state).toMatchObject({ loading: false, error: undefined });
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['c']);
  });

  it('an older fetch that succeeds after a newer one failed neither hides the error nor replaces the list', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const first = s.load();
    gates[0]!.resolve([initiative('keep')]);
    await first;
    const a = s.load();
    const b = s.load();
    await tick();
    gates[2]!.reject(new InitiativesError(500, 'newer'));
    await b;
    gates[1]!.resolve([initiative('stale')]);
    await a;
    expect(s.state).toMatchObject({ loading: false, error: 'newer' });
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['keep']);
  });
});

describe('InitiativesStore refresh (a live event)', () => {
  const tick = () => new Promise((r) => setTimeout(r, 0));
  function gated() {
    const gates: { resolve: (i: Initiative[]) => void; reject: (e: unknown) => void }[] = [];
    const list = () => new Promise<Initiative[]>((resolve, reject) => gates.push({ resolve, reject }));
    return { gates, list };
  }
  const make = (list: () => Promise<Initiative[]>) => new InitiativesStore({ list, create: async () => initiative('a') } as never, () => undefined);

  it('swaps the rows without ever making the list loading, so the rows stay on screen', async () => {
    const { gates, list } = gated();
    const seen: boolean[] = [];
    const s = new InitiativesStore({ list, create: async () => initiative('a') } as never, (st) => seen.push(st.loading));
    const first = s.load();
    gates[0]!.resolve([initiative('a')]);
    await first;
    seen.length = 0;
    const again = s.refresh();
    await tick();
    expect(s.state.loading).toBe(false);
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['a']);
    gates[1]!.resolve([initiative('a'), initiative('b')]);
    await again;
    expect(s.state.loading).toBe(false);
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['a', 'b']);
    expect(seen).not.toContain(true);
  });

  it('keeps the rows it had when the read fails and says why', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const first = s.load();
    gates[0]!.resolve([initiative('a')]);
    await first;
    const again = s.refresh();
    gates[1]!.reject(new InitiativesError(503, 'down'));
    await again;
    expect(s.state).toMatchObject({ loading: false, error: 'down' });
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['a']);
  });

  it('drops an older read that lands after a newer one', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const first = s.refresh();
    const second = s.refresh();
    gates[1]!.resolve([initiative('new')]);
    await second;
    gates[0]!.resolve([initiative('old')]);
    await first;
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['new']);
  });

  it('a first load still pending keeps the list loading after a refresh lands', async () => {
    const { gates, list } = gated();
    const s = make(list);
    const first = s.load();
    await tick();
    const again = s.refresh();
    gates[1]!.resolve([initiative('x')]);
    await again;
    expect(s.state.loading).toBe(true);
    gates[0]!.resolve([initiative('old')]);
    await first;
    expect(s.state).toMatchObject({ loading: false });
    expect(s.state.initiatives.map((i) => i.slug)).toEqual(['x']);
  });
});

describe('affectsInitiatives', () => {
  it('is true for every event that changes a row and false for notes and keys', () => {
    for (const type of ['initiative_created', 'initiative_closed', 'task_created', 'task_done', 'lease_started', 'lease_expired', 'ask_raised']) {
      expect(affectsInitiatives({ type }), type).toBe(true);
    }
    for (const type of ['note_posted', 'key_created', 'key_revoked']) expect(affectsInitiatives({ type }), type).toBe(false);
  });
});
