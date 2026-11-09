import { describe, expect, it, vi } from 'vitest';
import type { Ask } from './asks';
import { InitiativesError } from './initiatives';
import type { Note } from './notes';
import type { Browser } from './session';
import { ThreadApi, ThreadStore, type ThreadState } from './threads';

const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

function api(respond: (url: string, init?: RequestInit) => Promise<Response>, pathname = '/') {
  const fetchFn = vi.fn((input: string, init?: RequestInit) => respond(input, init));
  const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname, setHash: vi.fn() };
  return { api: new ThreadApi(browser, () => 'tok'), fetchFn };
}

describe('ThreadApi', () => {
  it('reads every open ask and every note of an initiative, following next_after through the pages', async () => {
    const pages = async (url: string) => (url.includes('after=') ? json({ items: [{ ref: 'X2' }] }) : json({ items: [{ ref: 'X1' }], next_after: 'X1' }));
    const { api: a, fetchFn } = api(pages);
    expect((await a.openAsks({ initiative: 'aa' })).map((x) => x.ref)).toEqual(['X1', 'X2']);
    expect((await a.notes({ initiative: 'aa' })).map((x) => x.ref)).toEqual(['X1', 'X2']);
    expect(fetchFn.mock.calls.map((c) => c[0])).toEqual([
      '/api/v1/asks?initiative=aa&state=open&limit=1000',
      '/api/v1/asks?initiative=aa&state=open&limit=1000&after=X1',
      '/api/v1/notes?initiative=aa&fields=summary&limit=1000',
      '/api/v1/notes?initiative=aa&fields=summary&limit=1000&after=X1',
    ]);
    expect(fetchFn.mock.calls[0]![1]).toMatchObject({ credentials: 'same-origin', headers: { Accept: 'application/json' } });
  });

  it('goes through the mount prefix', async () => {
    const { api: a, fetchFn } = api(async () => json({ items: [] }), '/board/');
    await a.notes({ initiative: 'aa' });
    expect(fetchFn.mock.calls[0]![0]).toBe('/board/api/v1/notes?initiative=aa&fields=summary&limit=1000');
  });

  it('reads one note, answers and withdraws an ask and posts a note with the CSRF token', async () => {
    const { api: a, fetchFn } = api(async () => json({ ref: 'N1', body: 'b' }));
    expect((await a.note('N1')).body).toBe('b');
    await a.answerAsk('A1', 'yes');
    await a.withdrawAsk('A1', 'why');
    await a.withdrawAsk('A1', '');
    await a.postNote({ summary: 's', initiative: 'aa' });
    expect(fetchFn.mock.calls.map((c) => [c[0], c[1]?.method, c[1]?.body])).toEqual([
      ['/api/v1/notes/N1', undefined, undefined],
      ['/api/v1/asks/A1/answer', 'POST', '{"answer":"yes"}'],
      ['/api/v1/asks/A1/withdraw', 'POST', '{"reason":"why"}'],
      ['/api/v1/asks/A1/withdraw', 'POST', '{}'],
      ['/api/v1/notes', 'POST', '{"summary":"s","initiative":"aa"}'],
    ]);
    expect(fetchFn.mock.calls[1]![1]).toMatchObject({ headers: { 'X-CSRF-Token': 'tok', 'Content-Type': 'application/json', Accept: 'application/json' } });
  });

  it('names the failed action when a call is refused without a detail, and an unreachable server', async () => {
    const { api: a } = api(async () => new Response('', { status: 500 }));
    await expect(a.openAsks({ initiative: 'aa' })).rejects.toMatchObject({ message: 'Reading the asks failed (HTTP 500).' });
    await expect(a.notes({ initiative: 'aa' })).rejects.toMatchObject({ message: 'Reading the notes failed (HTTP 500).' });
    await expect(a.note('N1')).rejects.toMatchObject({ message: 'Reading the note failed (HTTP 500).' });
    await expect(a.answerAsk('A1', 'x')).rejects.toMatchObject({ message: 'Answering the ask failed (HTTP 500).' });
    await expect(a.withdrawAsk('A1', '')).rejects.toMatchObject({ message: 'Withdrawing the ask failed (HTTP 500).' });
    await expect(a.postNote({ summary: 's' })).rejects.toMatchObject({ message: 'Posting the note failed (HTTP 500).' });
    const down = api(async () => Promise.reject(new TypeError('offline')));
    await expect(down.api.notes({ initiative: 'aa' })).rejects.toMatchObject({ status: 0, message: 'The server is not reachable.' });
  });
});

const scope = { initiative: 'aa' };

function harness(over: Partial<ConstructorParameters<typeof ThreadStore>[0]> = {}) {
  const apiStub = {
    openAsks: vi.fn(async () => [{ ref: 'A1', state: 'open' }] as unknown as Ask[]),
    notes: vi.fn(async () => [{ ref: 'N1' }, { ref: 'N2' }] as unknown as Note[]),
    note: vi.fn(async () => ({ ref: 'N2', body: 'the body' }) as Note),
    answerAsk: vi.fn(async () => ({}) as Ask),
    withdrawAsk: vi.fn(async () => ({}) as Ask),
    postNote: vi.fn(async () => ({}) as Note),
    ...over,
  };
  const seen: ThreadState[] = [];
  return { api: apiStub, store: new ThreadStore(apiStub, (s) => seen.push(s)), seen };
}

describe('ThreadStore', () => {
  it('starts empty', () => {
    expect(harness().store.state).toEqual({ asks: [], notes: [], bodies: {}, error: undefined, actionError: undefined });
  });

  it('reads the open asks and the notes, and nothing of them survives a failed read', async () => {
    const { store, seen } = harness();
    await store.load(scope);
    expect(store.state.asks.map((a) => a.ref)).toEqual(['A1']);
    expect(store.state.notes.map((n) => n.ref)).toEqual(['N1', 'N2']);
    expect(seen.at(-1)).toBe(store.state);
    const failing = harness({ notes: vi.fn(async () => Promise.reject(new Error('no notes'))) });
    await failing.store.load(scope);
    expect(failing.store.state).toMatchObject({ error: 'no notes', asks: [], notes: [] });
  });

  it('answers and withdraws with the trimmed text, then reads the lists again', async () => {
    const { api, store } = harness();
    await store.load(scope);
    expect(await store.answer(scope, 'A1', ' yes ')).toBe(true);
    expect(await store.withdraw(scope, 'A1', ' no longer ')).toBe(true);
    expect(api.answerAsk).toHaveBeenCalledWith('A1', 'yes');
    expect(api.withdrawAsk).toHaveBeenCalledWith('A1', 'no longer');
    expect(api.openAsks).toHaveBeenCalledTimes(3);
    expect(store.state.actionError).toBeUndefined();
  });

  it('refuses an empty answer and an over-long reason without a request', async () => {
    const { api, store } = harness();
    expect(await store.answer(scope, 'A1', '  ')).toBe(false);
    expect(store.state.actionError).toBe('Write an answer or pick one of the options.');
    expect(await store.withdraw(scope, 'A1', 'x'.repeat(201))).toBe(false);
    expect(store.state.actionError).toMatch(/at most 200/);
    expect(api.answerAsk).not.toHaveBeenCalled();
    expect(api.withdrawAsk).not.toHaveBeenCalled();
  });

  it('says why when the API refuses an answer, and forgets the refusal on request', async () => {
    const { api, store } = harness({ answerAsk: vi.fn(async () => Promise.reject(new InitiativesError(409, 'ask is not open'))) });
    expect(await store.answer(scope, 'A1', 'yes')).toBe(false);
    expect(store.state.actionError).toBe('ask is not open');
    expect(api.openAsks).not.toHaveBeenCalled();
    store.clearActionError();
    expect(store.state.actionError).toBeUndefined();
  });

  it('posts a note on the initiative, and refuses a form that cannot be sent', async () => {
    const { api, store } = harness();
    const form = { summary: ' hello ', body: '', resource: '', mentions: 'bob' };
    expect(await store.postNote(scope, form)).toBe(true);
    expect(api.postNote).toHaveBeenCalledWith({ summary: 'hello', initiative: 'aa', mentions: ['bob'] });
    expect(api.notes).toHaveBeenCalledTimes(1);
    expect(await store.postNote(scope, { ...form, summary: ' ' })).toBe(false);
    expect(store.state.actionError).toBe('Write a summary.');
    expect(api.postNote).toHaveBeenCalledTimes(1);
  });

  it('reads a note body once, when it is expanded, and says why when it cannot', async () => {
    const { api, store } = harness();
    await store.loadBody('N2');
    await store.loadBody('N2');
    expect(store.state.bodies).toEqual({ N2: 'the body' });
    expect(api.note).toHaveBeenCalledTimes(1);
    const bare = harness({ note: vi.fn(async () => ({ ref: 'N1' }) as Note) });
    await bare.store.loadBody('N1');
    expect(bare.store.state.bodies).toEqual({ N1: '' });
    const failing = harness({ note: vi.fn(async () => Promise.reject(new Error('gone'))) });
    await failing.store.loadBody('N2');
    expect(failing.store.state).toMatchObject({ actionError: 'gone', bodies: {} });
  });
});
