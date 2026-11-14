import { describe, expect, it, vi } from 'vitest';
import { DEFAULT_NOTES_FILTER, type Note, type NotesFilter } from './notes';
import { affectsNotes, dropUnknownReferences, forgetNotes, moreCount, NOTES_VIEW, notesLookup, NotesApi, NotesViewStore, openNotes, PAGE_SIZE, postScope, saveNotes, startNotesFilter, visibleNotes, type NotesLookup, type NotesViewState } from './notesView';
import { InitiativesError } from './initiatives';
import type { Browser } from './session';
import { viewKey, type ViewEnv, type ViewStorage } from './viewState';

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

function memory(
  initial: Record<string, string> = {},
): ViewStorage & { data: Map<string, string> } {
  const data = new Map(Object.entries(initial));
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
    removeItem: (k) => void data.delete(k),
    key: (i) => [...data.keys()][i] ?? null,
    get length() {
      return data.size;
    },
  };
}

function env(storage: ViewStorage | undefined): ViewEnv & { hashes: string[] } {
  const hashes: string[] = [];
  return { storage, replaceHash: (h) => void hashes.push(h), hashes };
}

describe('saved Notes filters', () => {
  const KEY = 'board.view.v1.notes';
  const FILTER = {
    ...DEFAULT_NOTES_FILTER,
    broadcastOnly: true,
    author: 'ann',
    resource: 'host:b-1',
  };

  it('is saved under board.view.v1.notes', () => {
    expect(viewKey(NOTES_VIEW)).toBe(KEY);
  });

  it('saves the filter as the fragment query and removes the key for the empty filter', () => {
    const storage = memory();
    saveNotes(storage, FILTER);
    expect(JSON.parse(storage.data.get(KEY)!)).toEqual({
      v: 1,
      query: 'broadcast=only&resource=host%3Ab-1&author=ann',
    });
    saveNotes(storage, DEFAULT_NOTES_FILTER);
    expect(storage.data.has(KEY)).toBe(false);
  });

  it('restores the saved filter on a bare route, rewriting the fragment in place once', () => {
    const storage = memory();
    saveNotes(storage, FILTER);
    const e = env(storage);
    expect(openNotes(e, '#/notes').filter).toEqual(FILTER);
    expect(e.hashes).toEqual([
      '#/notes?broadcast=only&resource=host%3Ab-1&author=ann',
    ]);
  });

  it('lets a fragment with its own query win, and saves it', () => {
    const storage = memory();
    saveNotes(storage, FILTER);
    const e = env(storage);
    expect(openNotes(e, '#/notes?mention=bob').filter).toEqual({
      ...DEFAULT_NOTES_FILTER,
      mention: 'bob',
    });
    expect(e.hashes).toEqual([]);
    expect(JSON.parse(storage.data.get(KEY)!)).toEqual({
      v: 1,
      query: 'mention=bob',
    });
  });

  it('restores the saved filter for a fragment with an empty query', () => {
    const storage = memory();
    saveNotes(storage, FILTER);
    expect(openNotes(env(storage), '#/notes?').filter).toEqual(FILTER);
  });

  it('reads the query after the first question mark wherever it is', () => {
    const storage = memory();
    saveNotes(storage, FILTER);
    expect(openNotes(env(storage), '?author=bob').filter.author).toBe('bob');
  });

  it('forgets the saved filter on Clear filters', () => {
    const storage = memory();
    saveNotes(storage, FILTER);
    forgetNotes(storage);
    expect(storage.data.has(KEY)).toBe(false);
    const e = env(storage);
    expect(openNotes(e, '#/notes').filter).toEqual(DEFAULT_NOTES_FILTER);
    expect(e.hashes).toEqual([]);
  });

  it('opens at the defaults for a malformed or foreign-version value, no storage or a storage that throws', () => {
    for (const bad of [
      'not json',
      JSON.stringify({ v: 2, query: 'author=ann' }),
      JSON.stringify({ v: 1, query: 7 }),
      JSON.stringify({ v: 1, query: '' }),
    ]) {
      const e = env(memory({ [KEY]: bad }));
      expect(openNotes(e, '#/notes').filter).toEqual(DEFAULT_NOTES_FILTER);
      expect(e.hashes).toEqual([]);
    }
    expect(openNotes(env(undefined), '#/notes').filter).toEqual(DEFAULT_NOTES_FILTER);
    const throwing = {
      ...memory(),
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('quota');
      },
    };
    expect(openNotes(env(throwing), '#/notes').filter).toEqual(DEFAULT_NOTES_FILTER);
    expect(openNotes(env(throwing), '#/notes?author=ann').filter.author).toBe('ann');
  });

  it('a restored filter is what the first read uses (one GET /notes under it)', async () => {
    const storage = memory();
    saveNotes(storage, FILTER);
    const e = env(storage);
    const api = {
      filtered: vi.fn(async () => notes(1)),
      note: vi.fn(),
      postNote: vi.fn(),
    };
    const store = new NotesViewStore(
      api,
      () => undefined,
      openNotes(e, '#/notes').filter,
    );
    await store.load();
    expect(api.filtered.mock.calls).toEqual([[FILTER]]);
  });
});

describe('restored board references the server no longer knows', () => {
  const KEY = 'board.view.v1.notes';
  const lookup = (slugs: string[], tasks: string[]): NotesLookup => ({
    initiatives: vi.fn(async () => slugs),
    taskExists: vi.fn(async (ref: string) => tasks.includes(ref)),
  });
  const withRefs: NotesFilter = { ...DEFAULT_NOTES_FILTER, initiative: 'gone', task: 'gone-1', author: 'ann', broadcastOnly: true };

  it('drops a removed initiative and keeps the unrelated filters', async () => {
    const kept = await dropUnknownReferences({ ...withRefs, task: '' }, lookup(['board'], []));
    expect(kept).toEqual({ ...DEFAULT_NOTES_FILTER, author: 'ann', broadcastOnly: true });
  });

  it('drops a removed task and keeps a known initiative', async () => {
    const kept = await dropUnknownReferences({ ...withRefs, initiative: 'board' }, lookup(['board'], []));
    expect(kept).toEqual({ ...DEFAULT_NOTES_FILTER, initiative: 'board', author: 'ann', broadcastOnly: true });
  });

  it('drops both when neither is known, and treats an empty initiative list as an answer', async () => {
    expect(await dropUnknownReferences(withRefs, lookup([], []))).toEqual({ ...DEFAULT_NOTES_FILTER, author: 'ann', broadcastOnly: true });
  });

  it('compares the slug without regard to case and returns the same object when nothing is dropped', async () => {
    const filter = { ...DEFAULT_NOTES_FILTER, initiative: 'BOARD', task: 'board-1' };
    expect(await dropUnknownReferences(filter, lookup(['board'], ['board-1']))).toBe(filter);
  });

  it('asks nothing for a filter that names no reference', async () => {
    const l = lookup([], []);
    const filter = { ...DEFAULT_NOTES_FILTER, author: 'ann' };
    expect(await dropUnknownReferences(filter, l)).toBe(filter);
    expect(l.initiatives).not.toHaveBeenCalled();
    expect(l.taskExists).not.toHaveBeenCalled();
  });

  it('keeps what a failed lookup could not check', async () => {
    const failing: NotesLookup = {
      initiatives: async () => {
        throw new Error('down');
      },
      taskExists: async () => {
        throw new Error('down');
      },
    };
    expect(await dropUnknownReferences(withRefs, failing)).toBe(withRefs);
  });

  it('drops a removed initiative but keeps a task that is still known', async () => {
    const kept = await dropUnknownReferences({ ...withRefs, task: 'board-1' }, lookup(['board'], ['board-1']));
    expect(kept).toEqual({ ...DEFAULT_NOTES_FILTER, task: 'board-1', author: 'ann', broadcastOnly: true });
  });

  it('checks each reference on its own: a failed task lookup keeps the task while a removed initiative is still dropped', async () => {
    const l: NotesLookup = { initiatives: async () => ['board'], taskExists: async () => { throw new Error('down'); } };
    expect(await dropUnknownReferences(withRefs, l)).toEqual({ ...withRefs, initiative: '' });
  });

  it('checks each reference on its own: a failed initiative lookup keeps the initiative while a removed task is still dropped', async () => {
    const l: NotesLookup = { initiatives: async () => { throw new Error('down'); }, taskExists: async () => false };
    expect(await dropUnknownReferences(withRefs, l)).toEqual({ ...withRefs, task: '' });
  });

  it('asks about the task as written and leaves the filter it was given untouched', async () => {
    const l = lookup(['board'], []);
    const filter = { ...withRefs, initiative: 'board' };
    const before = { ...filter };
    await dropUnknownReferences(filter, l);
    expect(l.taskExists).toHaveBeenCalledWith('gone-1');
    expect(l.taskExists).toHaveBeenCalledTimes(1);
    expect(l.initiatives).toHaveBeenCalledTimes(1);
    expect(filter).toEqual(before);
  });

  it('a saved filter that names only references the server no longer knows opens at the defaults and saves nothing', async () => {
    const storage = memory();
    saveNotes(storage, { ...DEFAULT_NOTES_FILTER, initiative: 'gone', task: 'gone-1' });
    const opened = openNotes(env(storage), '#/notes');
    expect(opened.restored).toBe(true);
    const kept = await startNotesFilter(opened, lookup([], []));
    expect(kept).toEqual(DEFAULT_NOTES_FILTER);
    saveNotes(storage, kept);
    expect(storage.data.has(KEY)).toBe(false);
  });

  it('a task that exists is kept even when the initiative list is empty and no initiative is named', async () => {
    const filter = { ...DEFAULT_NOTES_FILTER, task: 'board-1' };
    expect(await dropUnknownReferences(filter, lookup([], ['board-1']))).toBe(filter);
  });

  it('checks only a restored filter: a link names its references and keeps them', async () => {
    const l = lookup([], []);
    const storage = memory();
    saveNotes(storage, DEFAULT_NOTES_FILTER);
    const linked = openNotes(env(storage), '#/notes?initiative=gone&task=gone-1');
    expect(linked.restored).toBe(false);
    expect(await startNotesFilter(linked, l)).toBe(linked.filter);
    expect(l.initiatives).not.toHaveBeenCalled();
    expect(JSON.parse(storage.data.get(KEY)!).query).toBe('initiative=gone&task=gone-1');
  });

  it('a saved query restores as restored, and the first read uses the filter without the unknown references', async () => {
    const storage = memory();
    saveNotes(storage, withRefs);
    const opened = openNotes(env(storage), '#/notes');
    expect(opened.restored).toBe(true);
    const kept = await startNotesFilter(opened, lookup(['board'], []));
    const api = { filtered: vi.fn(async () => notes(1)), note: vi.fn(), postNote: vi.fn() };
    await new NotesViewStore(api, () => undefined, kept).load();
    expect(api.filtered.mock.calls).toEqual([[{ ...DEFAULT_NOTES_FILTER, author: 'ann', broadcastOnly: true }]]);
    saveNotes(storage, kept);
    expect(JSON.parse(storage.data.get(KEY)!).query).toBe('broadcast=only&author=ann');
  });
});

describe('notesLookup', () => {
  const problem = (status: number) => new InitiativesError(status, 'no');
  it('lists the slugs and treats 404 as no task, any other failure as unknown', async () => {
    const initiatives = { list: vi.fn(async () => [{ slug: 'a' }, { slug: 'b' }] as never) };
    const get = vi.fn(async (ref: string) => {
      if (ref === 'a-1') return {} as never;
      throw problem(ref === 'boom' ? 500 : 404);
    });
    const l = notesLookup(initiatives, { get });
    expect(await l.initiatives()).toEqual(['a', 'b']);
    expect(initiatives.list).toHaveBeenCalledWith('all');
    expect(await l.taskExists('a-1')).toBe(true);
    expect(await l.taskExists('a-9')).toBe(false);
    await expect(l.taskExists('boom')).rejects.toBeInstanceOf(InitiativesError);
  });
});
