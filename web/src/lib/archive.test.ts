import { describe, expect, it, vi } from 'vitest';
import {
  affectsArchive,
  ARCHIVE_WINDOWS,
  ArchiveApi,
  archiveInitiativesQuery,
  archiveQuery,
  initiativesIn,
  archiveRows,
  ArchiveStore,
  DEFAULT_ARCHIVE_WINDOW,
  parseWindow,
  reopenBody,
  reopenedToast,
  reopenLabel,
  windowFromQuery,
  windowToQuery,
  type ArchiveState,
  type ArchiveWindow,
} from './archive';
import type { components } from './api/schema';
import { NO_CHIPS } from './chipFilter';
import type { Browser } from './session';

type Task = components['schemas']['TaskListItem'];
type Initiative = components['schemas']['InitiativeListItem'];

const NOW = new Date('2026-03-10T12:00:00Z');
const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

function task(ref: string, closedAt: string | undefined, over: Partial<Task> = {}): Task {
  return { ref, title: `title ${ref}`, status: 'closed', resolution: 'completed', closed_at: closedAt, closed_by: 'ann', ...over } as Task;
}

function initiative(slug: string, closedAt: string | undefined, over: Partial<Initiative> = {}): Initiative {
  return { slug, title: `title ${slug}`, status: 'closed', closed_at: closedAt, closed_by: 'bob', ...over } as Initiative;
}

describe('the window <-> URL encoding', () => {
  it('names the four windows and defaults to 30 days', () => {
    expect(ARCHIVE_WINDOWS.map((w) => w.value)).toEqual(['7', '30', '90', 'all']);
    expect(DEFAULT_ARCHIVE_WINDOW).toBe('30');
  });

  it('keeps the default out of the URL and every other window in it', () => {
    expect(windowToQuery('30')).toBe('');
    expect(windowToQuery('7')).toBe('window=7');
    expect(windowToQuery('90')).toBe('window=90');
    expect(windowToQuery('all')).toBe('window=all');
  });

  it('reads the window back from a query, with or without its question mark', () => {
    expect(windowFromQuery('window=7')).toBe('7');
    expect(windowFromQuery('?window=90')).toBe('90');
    expect(windowFromQuery('window=all')).toBe('all');
    expect(windowFromQuery('')).toBe('30');
  });

  it('reads an unknown or malformed window as the default', () => {
    for (const raw of ['window=14', 'window=', 'window=ALL', 'window=7d', 'other=7', 'window=%']) {
      expect(windowFromQuery(raw)).toBe('30');
    }
    expect(parseWindow(null)).toBe('30');
    expect(parseWindow(undefined)).toBe('30');
  });

  it('asks the API for closed items closed within the days, and for every closed item with all', () => {
    expect(archiveQuery('7').toString()).toBe('status=closed&closed_within=7d');
    expect(archiveQuery('30').toString()).toBe('status=closed&closed_within=30d');
    expect(archiveQuery('90').toString()).toBe('status=closed&closed_within=90d');
    expect(archiveQuery('all').toString()).toBe('status=closed');
  });

  it('adds the chips to the task query as repeatable initiative (any) then tag (all), after the window', () => {
    const chips = { tags: ['x', 'y'], initiatives: ['b', 'a'] };
    expect(archiveQuery('7', chips).toString()).toBe('status=closed&closed_within=7d&initiative=b&initiative=a&tag=x&tag=y');
    expect(archiveQuery('all', { tags: ['x'], initiatives: [] }).toString()).toBe('status=closed&tag=x');
  });

  it('sends only the tags to the initiatives query, and picks the chosen initiatives out of the answer', () => {
    const chips = { tags: ['x'], initiatives: ['b', 'a'] };
    expect(archiveInitiativesQuery('30', chips).toString()).toBe('status=closed&closed_within=30d&tag=x');
    const list = [{ slug: 'a' }, { slug: 'c' }, { slug: 'b' }];
    expect(initiativesIn(list, ['b', 'a'])).toEqual([{ slug: 'a' }, { slug: 'b' }]);
    expect(initiativesIn(list, [])).toEqual(list);
  });
});

describe('archiveRows', () => {
  it('lists tasks and initiatives together, newest closed first', () => {
    const rows = archiveRows(
      [task('E1-1', '2026-03-01T00:00:00Z'), task('E1-2', '2026-03-09T00:00:00Z')],
      [initiative('old', '2026-02-01T00:00:00Z'), initiative('mid', '2026-03-05T00:00:00Z')],
      NOW,
    );
    expect(rows.map((r) => r.key)).toEqual(['task:E1-2', 'initiative:mid', 'task:E1-1', 'initiative:old']);
  });

  it('breaks a tie by key', () => {
    const at = '2026-03-09T00:00:00Z';
    expect(archiveRows([task('E1-2', at), task('E1-1', at)], [initiative('a', at)], NOW).map((r) => r.key)).toEqual(['initiative:a', 'task:E1-1', 'task:E1-2']);
  });

  it('draws a task with its resolution, its page, who closed it and when', () => {
    const [row] = archiveRows([task('E1-3', '2026-03-09T12:00:00Z', { resolution: 'dropped' })], [], NOW);
    expect(row).toMatchObject({ kind: 'task', ref: 'E1-3', title: 'title E1-3', href: '#/t/E1-3', resolution: 'dropped', closedBy: 'ann', closed: 'yesterday' });
    expect(row!.closedTitle).toMatch(/^\d{4}-\d\d-\d\d \d\d:\d\d$/);
  });

  it('draws an initiative with no resolution of its own', () => {
    const [row] = archiveRows([], [initiative('big thing', '2026-03-10T11:00:00Z')], NOW);
    expect(row).toMatchObject({ kind: 'initiative', ref: 'big thing', href: '#/i/big%20thing', resolution: 'initiative closed', closedBy: 'bob', closed: '1 hour ago' });
  });

  it('puts a row with no closed_at last and draws it without a time', () => {
    const rows = archiveRows([task('E1-1', undefined, { resolution: undefined, closed_by: undefined })], [initiative('a', '2026-03-09T00:00:00Z')], NOW);
    expect(rows.map((r) => r.key)).toEqual(['initiative:a', 'task:E1-1']);
    expect(rows[1]).toMatchObject({ resolution: 'closed', closed: '', closedTitle: '', closedAt: '', closedBy: '' });
  });

  it('draws an initiative with no closed_at without a time or a closer', () => {
    const [row] = archiveRows([], [initiative('a', undefined, { closed_by: undefined })], NOW);
    expect(row).toMatchObject({ kind: 'initiative', closed: '', closedTitle: '', closedAt: '', closedBy: '' });
  });

  it('draws the closer and the exact time of both kinds', () => {
    const rows = archiveRows([task('E1-1', '2026-03-09T12:00:00Z')], [initiative('a', '2026-03-08T12:00:00Z')], NOW);
    expect(rows.map((r) => [r.closedAt, r.closedBy, r.closed])).toEqual([
      ['2026-03-09T12:00:00Z', 'ann', 'yesterday'],
      ['2026-03-08T12:00:00Z', 'bob', '2 days ago'],
    ]);
    for (const row of rows) expect(row.closedTitle).toMatch(/^\d{4}-\d\d-\d\d \d\d:\d\d$/);
  });

  it('is empty for nothing closed', () => {
    expect(archiveRows([], [], NOW)).toEqual([]);
  });

  it('leaves its inputs alone', () => {
    const tasks = [task('E1-1', '2026-03-01T00:00:00Z'), task('E1-2', '2026-03-09T00:00:00Z')];
    archiveRows(tasks, [], NOW);
    expect(tasks.map((t) => t.ref)).toEqual(['E1-1', 'E1-2']);
  });
});

describe('the Reopen words', () => {
  it('names the kind and the item', () => {
    expect(reopenLabel({ kind: 'task' })).toBe('Reopen task');
    expect(reopenLabel({ kind: 'initiative' })).toBe('Reopen initiative');
    expect(reopenBody({ kind: 'task', ref: 'E1-3' })).toBe('Task E1-3 goes back to open and shows in the Tasks view again.');
    expect(reopenBody({ kind: 'initiative', ref: 'x' })).toBe('Initiative x goes back to open and takes tasks again. Its closed tasks stay closed.');
    expect(reopenedToast({ kind: 'task', ref: 'E1-3' })).toBe('Task E1-3 reopened');
    expect(reopenedToast({ kind: 'initiative', ref: 'x' })).toBe('Initiative x reopened');
  });
});

describe('affectsArchive', () => {
  it('refetches on a task or initiative closing or reopening, and on nothing else', () => {
    for (const type of ['task_closed', 'task_reopened', 'initiative_closed', 'initiative_reopened']) expect(affectsArchive({ type })).toBe(true);
    for (const type of ['task_done', 'task_created', 'lease_started', 'note_posted', 'initiative_created']) expect(affectsArchive({ type })).toBe(false);
  });
});

describe('ArchiveApi', () => {
  function setup(respond: (url: string, init?: RequestInit) => Response = () => json({ items: [] })) {
    const fetchFn = vi.fn(async (input: string, init?: RequestInit) => respond(input, init));
    const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname: '/board/', setHash: vi.fn() };
    return { fetchFn, api: new ArchiveApi(browser, () => 'tok') };
  }

  it('lists the closed tasks and initiatives of a window, same origin, under the mount prefix', async () => {
    const { fetchFn, api } = setup((url) => json({ items: [url.includes('/tasks') ? task('E1-1', '2026-03-09T00:00:00Z') : initiative('a', '2026-03-09T00:00:00Z')] }));
    expect((await api.tasks('7')).map((t) => t.ref)).toEqual(['E1-1']);
    expect((await api.initiatives('all')).map((i) => i.slug)).toEqual(['a']);
    expect(fetchFn.mock.calls[0]![0]).toBe('/board/api/v1/tasks?status=closed&closed_within=7d');
    expect(fetchFn.mock.calls[1]![0]).toBe('/board/api/v1/initiatives?status=closed');
    expect(fetchFn.mock.calls[0]![1]?.credentials).toBe('same-origin');
  });

  it('asks the server for the chips: tag and initiative repeated on /tasks with status=closed, only tags on /initiatives', async () => {
    const { fetchFn, api } = setup((url) => json({ items: url.includes('/tasks') ? [] : [initiative('a', '2026-03-09T00:00:00Z'), initiative('b', '2026-03-09T00:00:00Z')] }));
    const chips = { tags: ['x', 'y'], initiatives: ['b', 'q'] };
    await api.tasks('7', chips);
    const initiatives = await api.initiatives('7', chips);
    expect(fetchFn.mock.calls[0]![0]).toBe('/board/api/v1/tasks?status=closed&closed_within=7d&initiative=b&initiative=q&tag=x&tag=y');
    expect(fetchFn.mock.calls[1]![0]).toBe('/board/api/v1/initiatives?status=closed&closed_within=7d&tag=x&tag=y');
    expect(initiatives.map((i) => i.slug)).toEqual(['b']);
  });

  it('follows next_after through the pages', async () => {
    const { fetchFn, api } = setup((url) => (url.includes('after=E1-1') ? json({ items: [task('E1-2', undefined)] }) : json({ items: [task('E1-1', undefined)], next_after: 'E1-1' })));
    expect((await api.tasks('all')).map((t) => t.ref)).toEqual(['E1-1', 'E1-2']);
    expect(fetchFn.mock.calls[1]![0]).toContain('after=E1-1');
  });

  it('reopens a task and an initiative with a POST carrying the CSRF token', async () => {
    const { fetchFn, api } = setup();
    await api.reopenTask('E1-3');
    await api.reopenInitiative('my slug');
    expect(fetchFn.mock.calls[0]![0]).toBe('/board/api/v1/tasks/E1-3/reopen');
    expect(fetchFn.mock.calls[1]![0]).toBe('/board/api/v1/initiatives/my%20slug/reopen');
    for (const call of fetchFn.mock.calls) {
      expect(call[1]?.method).toBe('POST');
      expect(call[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'tok' });
    }
  });

  it('names what it was doing when a read or a reopen is refused', async () => {
    const { api } = setup(() => new Response('', { status: 500 }));
    await expect(api.tasks('7')).rejects.toThrow(/Listing the closed tasks/);
    await expect(api.initiatives('7')).rejects.toThrow(/Listing the closed initiatives/);
    await expect(api.reopenTask('E1-1')).rejects.toThrow(/Reopening the task/);
    await expect(api.reopenInitiative('a')).rejects.toThrow(/Reopening the initiative/);
  });
});

describe('ArchiveStore', () => {
  function setup(over: Partial<Record<'tasks' | 'initiatives' | 'reopenTask' | 'reopenInitiative', (arg: string) => Promise<unknown>>> = {}) {
    const states: ArchiveState[] = [];
    const api = {
      tasks: vi.fn(async (_w: ArchiveWindow, _c?: unknown) => [task('E1-1', '2026-03-09T00:00:00Z')]),
      initiatives: vi.fn(async (_w: ArchiveWindow, _c?: unknown) => [initiative('a', '2026-03-08T00:00:00Z')]),
      reopenTask: vi.fn(async (_ref: string) => undefined),
      reopenInitiative: vi.fn(async (_slug: string) => undefined),
      ...over,
    } as unknown as ConstructorParameters<typeof ArchiveStore>[0];
    const store = new ArchiveStore(api, (s) => states.push(s));
    return { store, states, api };
  }

  it('starts loading the default window, with nothing loaded', () => {
    expect(setup().store.state).toEqual({ window: '30', chips: NO_CHIPS, tasks: [], initiatives: [], loaded: false, loading: true, error: '', dialogError: '' });
  });

  it('shows both lists once the read lands', async () => {
    const { store } = setup();
    await store.load();
    expect(store.state).toMatchObject({ loaded: true, loading: false, error: '' });
    expect(store.state.tasks.map((t) => t.ref)).toEqual(['E1-1']);
    expect(store.state.initiatives.map((i) => i.slug)).toEqual(['a']);
  });

  it('reads under the chips it holds, and a new chip set once, not the same one again', async () => {
    const chips = { tags: ['x'], initiatives: ['a'] };
    const { store, api } = setup();
    await store.setChips(NO_CHIPS);
    expect(api.tasks).not.toHaveBeenCalled();
    await store.setChips(chips);
    expect(store.state.chips).toEqual(chips);
    expect(api.tasks).toHaveBeenCalledWith('30', chips);
    expect(api.initiatives).toHaveBeenCalledWith('30', chips);
    await store.setChips({ tags: ['x'], initiatives: ['a'] });
    expect(api.tasks).toHaveBeenCalledTimes(1);
    await store.setScope('7', chips);
    expect(api.tasks).toHaveBeenLastCalledWith('7', chips);
    await store.setScope('7', { tags: [], initiatives: ['a'] });
    expect(api.tasks).toHaveBeenCalledTimes(3);
  });

  it('reads the window it holds, and a new window once, not the same one again', async () => {
    const { store, api } = setup();
    await store.setWindow('30');
    expect(api.tasks).not.toHaveBeenCalled();
    await store.setWindow('7');
    expect(store.state.window).toBe('7');
    expect(api.tasks).toHaveBeenCalledWith('7', NO_CHIPS);
    expect(api.initiatives).toHaveBeenCalledWith('7', NO_CHIPS);
  });

  it('keeps what it shows and says why when a refresh fails', async () => {
    let fail = false;
    const { store } = setup({
      tasks: async () => {
        if (fail) throw new Error('The server is not reachable.');
        return [task('E1-1', '2026-03-09T00:00:00Z')];
      },
    });
    await store.load();
    fail = true;
    await store.load();
    expect(store.state).toMatchObject({ loaded: true, loading: false, error: 'The server is not reachable.' });
    expect(store.state.tasks).toHaveLength(1);
  });

  it('drops an answer that lands after a newer one', async () => {
    const resolvers: ((t: Task[]) => void)[] = [];
    const { store } = setup({ tasks: () => new Promise<Task[]>((r) => resolvers.push(r)) });
    const first = store.load();
    const second = store.load();
    resolvers[1]!([task('E1-9', '2026-03-09T00:00:00Z')]);
    await second;
    resolvers[0]!([task('E1-1', '2026-03-09T00:00:00Z')]);
    await first;
    expect(store.state.tasks.map((t) => t.ref)).toEqual(['E1-9']);
    expect(store.state.loading).toBe(false);
  });

  it('reopens a task, then reads the archive again', async () => {
    const { store, api } = setup();
    await store.load();
    expect(await store.reopen({ kind: 'task', ref: 'E1-1' })).toBe(true);
    expect(api.reopenTask).toHaveBeenCalledWith('E1-1');
    expect(api.tasks).toHaveBeenCalledTimes(2);
  });

  it('reopens an initiative through the initiative call', async () => {
    const { store, api } = setup();
    expect(await store.reopen({ kind: 'initiative', ref: 'a' })).toBe(true);
    expect(api.reopenInitiative).toHaveBeenCalledWith('a');
    expect(api.reopenTask).not.toHaveBeenCalled();
  });

  it('keeps the dialog open with the refusal when the reopen is refused, and reads nothing again', async () => {
    const { store, api } = setup({
      reopenTask: async () => {
        throw new Error('The task is in a closed initiative.');
      },
    });
    await store.load();
    expect(await store.reopen({ kind: 'task', ref: 'E1-1' })).toBe(false);
    expect(store.state.dialogError).toBe('The task is in a closed initiative.');
    expect(api.tasks).toHaveBeenCalledTimes(1);
    const before = store.state;
    store.clearDialogError();
    expect(store.state.dialogError).toBe('');
    store.clearDialogError();
    expect(store.state).not.toBe(before);
    const cleared = store.state;
    store.clearDialogError();
    expect(store.state).toBe(cleared);
  });
});
