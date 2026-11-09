import { describe, expect, it, vi } from 'vitest';
import { DEFAULT_NOTES_FILTER, type Note } from './notes';
import { affectsNotes, moreCount, NotesApi, NotesViewStore, PAGE_SIZE, postScope, visibleNotes, type NotesViewState } from './notesView';
import type { Browser } from './session';

const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const notes = (count: number): Note[] => Array.from({ length: count }, (_, i) => ({ ref: `N${i + 1}`, summary: `s${i + 1}` }) as Note);

describe('NotesApi', () => {
  it('reads every page of the filtered notes, summaries only', async () => {
    const respond = async (url: string) => (url.includes('after=') ? json({ items: [{ ref: 'N2' }] }) : json({ items: [{ ref: 'N1' }], next_after: 'N1' }));
    const fetchFn = vi.fn((input: string) => respond(input));
    const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname: '/', setHash: vi.fn() };
    const got = await new NotesApi(browser, () => 't').filtered({ ...DEFAULT_NOTES_FILTER, broadcastOnly: true, author: 'ann' });
    expect(got.map((n) => n.ref)).toEqual(['N1', 'N2']);
    expect(fetchFn.mock.calls.map((c) => c[0])).toEqual([
      '/api/v1/notes?broadcast=only&author=ann&fields=summary&limit=1000',
      '/api/v1/notes?broadcast=only&author=ann&fields=summary&limit=1000&after=N1',
    ]);
  });
});

function rig(list: Note[] = notes(45)) {
  const api = {
    filtered: vi.fn(async () => list),
    note: vi.fn(async (ref: string) => ({ ref, body: `body of ${ref}` }) as Note),
    postNote: vi.fn(async () => ({}) as Note),
  };
  const seen: NotesViewState[] = [];
  const store = new NotesViewStore(api, (s) => seen.push(s), DEFAULT_NOTES_FILTER);
  return { api, store, seen };
}

describe('NotesViewStore', () => {
  it('draws the newest notes first, one page at a time, and Load more adds the next page', async () => {
    const { store } = rig();
    await store.load();
    expect(store.state.loading).toBe(false);
    expect(visibleNotes(store.state).map((n) => n.ref)).toEqual(Array.from({ length: PAGE_SIZE }, (_, i) => `N${45 - i}`));
    expect(moreCount(store.state)).toBe(PAGE_SIZE);
    store.loadMore();
    store.loadMore();
    expect(visibleNotes(store.state)).toHaveLength(45);
    expect(moreCount(store.state)).toBe(0);
    store.loadMore();
    expect(store.state.shown).toBe(45);
  });

  it('counts a last page smaller than a page', async () => {
    const { store } = rig(notes(25));
    await store.load();
    expect(moreCount(store.state)).toBe(5);
  });

  it('starts again from the first page under a new filter', async () => {
    const { store, api } = rig();
    await store.load();
    store.loadMore();
    await store.setFilter({ ...DEFAULT_NOTES_FILTER, task: 'a-1' });
    expect(store.state.shown).toBe(PAGE_SIZE);
    expect(store.state.filter.task).toBe('a-1');
    expect(api.filtered).toHaveBeenLastCalledWith(expect.objectContaining({ task: 'a-1' }));
  });

  it('keeps how much is drawn over a refresh', async () => {
    const { store } = rig();
    await store.load();
    store.loadMore();
    await store.load();
    expect(store.state.shown).toBe(2 * PAGE_SIZE);
  });

  it('shows the error and no notes when the read fails', async () => {
    const { store, api } = rig();
    api.filtered.mockRejectedValueOnce(new Error('boom'));
    await store.load();
    expect(store.state).toMatchObject({ error: 'boom', notes: [], loading: false });
  });

  it('drops an answer for a filter that is no longer set', async () => {
    const { store, api } = rig();
    let release: (n: Note[]) => void = () => undefined;
    api.filtered.mockImplementationOnce(() => new Promise<Note[]>((r) => (release = r)));
    const first = store.load();
    await store.setFilter({ ...DEFAULT_NOTES_FILTER, author: 'x' });
    release(notes(3));
    await first;
    expect(store.state.notes).toHaveLength(45);
  });

  it('posts a note with its scope and reads the list again', async () => {
    const { store, api } = rig();
    const form = { summary: ' hi ', body: '', resource: '', mentions: 'bob' };
    expect(await store.post(form, { task: ' a-1 ', initiative: '' })).toBe(true);
    expect(api.postNote).toHaveBeenCalledWith({ summary: 'hi', task: 'a-1', mentions: ['bob'] });
    expect(api.filtered).toHaveBeenCalledTimes(1);
    expect(store.state.actionError).toBeUndefined();
  });

  it('posts a broadcast when no scope is given and refuses a form that cannot be sent without calling the API', async () => {
    const { store, api } = rig();
    expect(await store.post({ summary: 'all', body: '', resource: '', mentions: '' }, { task: '', initiative: '' })).toBe(true);
    expect(api.postNote).toHaveBeenCalledWith({ summary: 'all' });
    expect(await store.post({ summary: ' ', body: '', resource: '', mentions: '' }, { task: '', initiative: '' })).toBe(false);
    expect(store.state.actionError).toBe('Write a summary.');
    expect(api.postNote).toHaveBeenCalledTimes(1);
  });

  it('shows why the server refused a note and clears it', async () => {
    const { store, api } = rig();
    api.postNote.mockRejectedValueOnce(new Error('No such task.'));
    expect(await store.post({ summary: 's', body: '', resource: '', mentions: '' }, { task: 'zz-9', initiative: '' })).toBe(false);
    expect(store.state.actionError).toBe('No such task.');
    store.clearActionError();
    expect(store.state.actionError).toBeUndefined();
  });

  it('forgets an earlier refusal once a post succeeds', async () => {
    const { store, api } = rig();
    api.postNote.mockRejectedValueOnce(new Error('No such task.'));
    await store.post({ summary: 's', body: '', resource: '', mentions: '' }, { task: 'zz-9', initiative: '' });
    expect(await store.post({ summary: 's', body: '', resource: '', mentions: '' }, { task: '', initiative: '' })).toBe(true);
    expect(store.state.actionError).toBeUndefined();
  });

  it('reads a body once and shows a failed read', async () => {
    const { store, api } = rig();
    await store.loadBody('N3');
    await store.loadBody('N3');
    expect(api.note).toHaveBeenCalledTimes(1);
    expect(store.state.bodies).toEqual({ N3: 'body of N3' });
    api.note.mockRejectedValueOnce(new Error('gone'));
    await store.loadBody('N4');
    expect(store.state.error).toBe('gone');
  });

  it('holds an empty body for a note whose body is absent', async () => {
    const { store, api } = rig();
    api.note.mockResolvedValueOnce({ ref: 'N5' } as Note);
    await store.loadBody('N5');
    expect(store.state.bodies).toEqual({ N5: '' });
  });
});

describe('postScope', () => {
  it('keeps the filled fields only', () => {
    expect(postScope({ task: '', initiative: '' })).toEqual({});
    expect(postScope({ task: ' a-1 ', initiative: ' a ' })).toEqual({ task: 'a-1', initiative: 'a' });
    expect(postScope({ task: '', initiative: 'a' })).toEqual({ initiative: 'a' });
  });
});

describe('NotesViewStore refresh (a live event)', () => {
  const tick = () => new Promise((r) => setTimeout(r, 0));
  function gated() {
    const gates: { resolve: (n: Note[]) => void; reject: (e: unknown) => void }[] = [];
    const filtered = vi.fn(() => new Promise<Note[]>((resolve, reject) => gates.push({ resolve, reject })));
    const api = { filtered, note: vi.fn(async (ref: string) => ({ ref, body: `body of ${ref}` }) as Note), postNote: vi.fn(async () => ({}) as Note) };
    const seen: NotesViewState[] = [];
    return { gates, api, seen, store: new NotesViewStore(api, (s) => seen.push(s), DEFAULT_NOTES_FILTER) };
  }

  it('adds the new note on top, keeping the page drawn and the bodies read, without making the view loading', async () => {
    const { gates, store, seen } = gated();
    const first = store.load();
    gates[0]!.resolve(notes(45));
    await first;
    store.loadMore();
    await store.loadBody('N45');
    seen.length = 0;
    const again = store.refresh();
    await tick();
    expect(store.state.notes).toHaveLength(45);
    gates[1]!.resolve(notes(46));
    await again;
    expect(store.state.notes[0]?.ref).toBe('N46');
    expect(store.state).toMatchObject({ shown: 40, loading: false, error: undefined, bodies: { N45: 'body of N45' } });
    expect(seen.map((s) => s.loading)).not.toContain(true);
  });

  it('keeps the notes it had when the read fails and says why', async () => {
    const { gates, store } = gated();
    const first = store.load();
    gates[0]!.resolve(notes(3));
    await first;
    const again = store.refresh();
    gates[1]!.reject(new Error('down'));
    await again;
    expect(store.state).toMatchObject({ loading: false, error: 'down' });
    expect(store.state.notes).toHaveLength(3);
  });

  it('keeps the notes and the bodies read through a failed refresh and clears the error on the next good one', async () => {
    const { gates, store } = gated();
    const first = store.load();
    gates[0]!.resolve(notes(3));
    await first;
    await store.loadBody('N3');
    const failed = store.refresh();
    gates[1]!.reject(new Error('down'));
    await failed;
    expect(store.state).toMatchObject({ error: 'down', bodies: { N3: 'body of N3' } });
    expect(store.state.notes).toHaveLength(3);
    const good = store.refresh();
    gates[2]!.resolve(notes(4));
    await good;
    expect(store.state).toMatchObject({ error: undefined, bodies: { N3: 'body of N3' } });
    expect(store.state.notes).toHaveLength(4);
  });

  it('a failed body read keeps the notes already drawn', async () => {
    const { gates, api, store } = gated();
    const first = store.load();
    gates[0]!.resolve(notes(3));
    await first;
    api.note.mockRejectedValueOnce(new Error('gone'));
    await store.loadBody('N2');
    expect(store.state.error).toBe('gone');
    expect(store.state.notes).toHaveLength(3);
  });

  it('drops an older read that lands after a newer one, and one for a filter since replaced', async () => {
    const { gates, store } = gated();
    const first = store.refresh();
    const second = store.refresh();
    gates[1]!.resolve(notes(2));
    await second;
    gates[0]!.resolve(notes(1));
    await first;
    expect(store.state.notes).toHaveLength(2);

    const stale = store.refresh();
    const next = store.setFilter({ ...DEFAULT_NOTES_FILTER, author: 'ann' });
    gates[3]!.resolve(notes(5));
    await next;
    gates[2]!.resolve(notes(9));
    await stale;
    expect(store.state.notes).toHaveLength(5);
  });

  it('a first load still pending keeps the view loading after a refresh lands', async () => {
    const { gates, store } = gated();
    const first = store.load();
    await tick();
    const again = store.refresh();
    gates[1]!.resolve(notes(2));
    await again;
    expect(store.state.loading).toBe(true);
    gates[0]!.resolve(notes(1));
    await first;
    expect(store.state.loading).toBe(false);
    expect(store.state.notes).toHaveLength(2);
  });
});

describe('affectsNotes', () => {
  it('is true for the note events, false for the rest', () => {
    expect(affectsNotes({ type: 'note_posted' })).toBe(true);
    expect(affectsNotes({ type: 'task_done' })).toBe(false);
    expect(affectsNotes({ type: 'key_created' })).toBe(false);
  });
});
