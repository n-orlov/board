import { describe, expect, it, vi } from 'vitest';
import {
  InitiativePageStore,
  concerns,
  leaseHolder,
  liveLeaseCount,
  pageNeedsHref,
  pageNeedsQuery,
  summaryText,
  targetsText,
  taskBadges,
  taskCounts,
  taskRows,
  type InitiativePageState,
} from './initiativePage';
import { InitiativesApi, InitiativesError, type Initiative, type TaskListItem } from './initiatives';
import type { Browser } from './session';

const lease = (actor: string) => ({ actor }) as TaskListItem['live_leases'][number];

const task = (ref: string, extra: Partial<TaskListItem> = {}): TaskListItem =>
  ({
    id: 'id-' + ref,
    ref,
    title: 'Title ' + ref,
    initiative: 'aa',
    kind: 'work',
    status: 'open',
    active: false,
    blocked: false,
    overdue: false,
    live_leases: [],
    attention: [],
    tag_marks: [],
    ...extra,
  }) as TaskListItem;

const initiative = { slug: 'aa', title: 'Aa' } as Initiative;

describe('taskCounts and summaryText', () => {
  it('counts the tasks by status and ignores a status it does not know', () => {
    expect(taskCounts([{ status: 'open' }, { status: 'open' }, { status: 'done' }, { status: 'closed' }, { status: 'weird' }])).toEqual({
      open: 2,
      done: 1,
      closed: 1,
    });
    expect(taskCounts([])).toEqual({ open: 0, done: 0, closed: 0 });
  });

  it('adds the live leases of every task', () => {
    expect(liveLeaseCount([{ live_leases: [lease('a'), lease('b')] }, { live_leases: [] }, { live_leases: [lease('c')] }])).toBe(3);
    expect(liveLeaseCount([])).toBe(0);
  });

  it('puts the counts and the leases on one line', () => {
    const tasks = [task('aa-1', { live_leases: [lease('x')] }), task('aa-2', { status: 'done' }), task('aa-3', { status: 'closed' })];
    expect(summaryText(tasks)).toBe('1 open · 1 done · 1 closed, 1 live lease');
    expect(summaryText([])).toBe('No tasks, no live leases');
  });
});

describe('targetsText', () => {
  it.each([
    [{ target_start: '2026-01-01', target_end: '2026-02-01' }, '2026-01-01 to 2026-02-01'],
    [{ target_start: '2026-01-01' }, 'from 2026-01-01'],
    [{ target_end: '2026-02-01' }, 'until 2026-02-01'],
    [{}, ''],
  ])('phrases %j as %j', (targets, want) => {
    expect(targetsText(targets)).toBe(want);
  });
});

describe('taskBadges and leaseHolder', () => {
  it('gives a badge for each flag that is on, in a fixed order, each with its own tone', () => {
    expect(taskBadges({ active: true, blocked: true, overdue: true })).toEqual([
      { key: 'active', label: 'active', tone: 'success' },
      { key: 'blocked', label: 'blocked', tone: 'warning' },
      { key: 'overdue', label: 'overdue', tone: 'danger' },
    ]);
    expect(taskBadges({ active: false, blocked: true, overdue: false }).map((b) => b.key)).toEqual(['blocked']);
    expect(taskBadges({ active: true, blocked: false, overdue: false }).map((b) => b.key)).toEqual(['active']);
    expect(taskBadges({ active: false, blocked: false, overdue: true }).map((b) => b.key)).toEqual(['overdue']);
    expect(taskBadges({ active: false, blocked: false, overdue: false })).toEqual([]);
  });

  it('names the oldest lease holder and counts the others', () => {
    expect(leaseHolder({ live_leases: [] })).toBe('');
    expect(leaseHolder({ live_leases: [lease('ann')] })).toBe('ann');
    expect(leaseHolder({ live_leases: [lease('ann'), lease('bob'), lease('cy')] })).toBe('ann +2');
  });
});

describe('taskRows', () => {
  const tasks = [
    task('aa-1', { live_leases: [lease('ann')], active: true }),
    task('aa-2', { status: 'done', kind: 'watch', overdue: true }),
    task('aa-3', { status: 'closed', blocked: true }),
  ];

  it('keeps the tasks the filter shows, each with its badges and holder', () => {
    expect(taskRows(tasks, 'not-closed')).toEqual([
      { id: 'id-aa-1', ref: 'aa-1', initiative: 'aa', title: 'Title aa-1', kind: 'work', status: 'open', badges: [{ key: 'active', label: 'active', tone: 'success' }], attention: [], holder: 'ann', marks: [] },
      { id: 'id-aa-2', ref: 'aa-2', initiative: 'aa', title: 'Title aa-2', kind: 'watch', status: 'done', badges: [{ key: 'overdue', label: 'overdue', tone: 'danger' }], attention: [], holder: '', marks: [] },
    ]);
    expect(taskRows(tasks, 'done').map((r) => r.ref)).toEqual(['aa-2']);
    expect(taskRows(tasks, 'closed').map((r) => r.ref)).toEqual(['aa-3']);
    expect(taskRows(tasks, 'all').map((r) => r.ref)).toEqual(['aa-1', 'aa-2', 'aa-3']);
  });
});

describe('InitiativePageStore', () => {
  function store(api: { get: () => Promise<Initiative>; tasks: () => Promise<TaskListItem[]> }) {
    const seen: InitiativePageState[] = [];
    return { s: new InitiativePageStore(api as never, (st) => seen.push(st)), seen };
  }

  it('starts loading with the not-closed filter', () => {
    const { s } = store({ get: async () => initiative, tasks: async () => [] });
    expect(s.state).toEqual({ initiative: undefined, tasks: [], filter: 'not-closed', loading: true, error: undefined, dialogError: undefined });
  });

  it('load shows the initiative and its tasks and stops loading', async () => {
    const { s, seen } = store({ get: async () => initiative, tasks: async () => [task('aa-1')] });
    await s.load('aa');
    expect(s.state.initiative).toBe(initiative);
    expect(s.state.tasks.map((t) => t.ref)).toEqual(['aa-1']);
    expect(s.state.loading).toBe(false);
    expect(seen.map((st) => st.loading)).toEqual([true, false]);
  });

  it('a failure shows the error text, no half page and no loading', async () => {
    const a = store({ get: async () => Promise.reject(new InitiativesError(404, 'initiative not found')), tasks: async () => [task('aa-1')] });
    await a.s.load('zz');
    expect(a.s.state).toMatchObject({ initiative: undefined, tasks: [], loading: false, error: 'initiative not found' });
    const b = store({ get: async () => initiative, tasks: async () => Promise.reject('plain') });
    await b.s.load('aa');
    expect(b.s.state).toMatchObject({ initiative: undefined, tasks: [], error: 'plain' });
  });

  it('a load after a failure clears the error', async () => {
    let fail = true;
    const { s } = store({ get: async () => (fail ? Promise.reject(new Error('down')) : initiative), tasks: async () => [] });
    await s.load('aa');
    fail = false;
    await s.load('aa');
    expect(s.state.error).toBeUndefined();
    expect(s.state.initiative).toBe(initiative);
  });

  it('setFilter keeps the filter and the tasks without fetching', async () => {
    const get = vi.fn(async () => initiative);
    const { s } = store({ get, tasks: async () => [task('aa-1')] });
    await s.load('aa');
    s.setFilter('closed');
    expect(s.state.filter).toBe('closed');
    expect(s.state.tasks).toHaveLength(1);
    expect(get).toHaveBeenCalledTimes(1);
  });
});

describe('InitiativePageStore reload', () => {
  it('reads the page again without the skeleton', async () => {
    let tasks = [task('aa-1')];
    const seen: InitiativePageState[] = [];
    const s = new InitiativePageStore({ get: async () => initiative, tasks: async () => tasks } as never, (st) => seen.push(st));
    await s.load('aa');
    tasks = [task('aa-1'), task('aa-2')];
    seen.length = 0;
    await s.reload('aa');
    expect(seen.every((st) => !st.loading)).toBe(true);
    expect(s.state.tasks.map((t) => t.ref)).toEqual(['aa-1', 'aa-2']);
  });
});

describe('InitiativePageStore writes', () => {
  const later = { slug: 'aa', title: 'Later' } as Initiative;
  function writer(over: { patch?: () => Promise<unknown>; close?: () => Promise<unknown>; reopen?: () => Promise<unknown> } = {}) {
    let current = initiative;
    const api = {
      get: vi.fn(async () => current),
      tasks: vi.fn(async () => [task('aa-1')]),
      patch: vi.fn(over.patch ?? (async () => (current = later))),
      close: vi.fn(over.close ?? (async () => (current = later))),
      reopen: vi.fn(over.reopen ?? (async () => (current = later))),
    };
    const seen: InitiativePageState[] = [];
    return { api, s: new InitiativePageStore(api as never, (st) => seen.push(st)), seen };
  }
  const form = { title: ' New ', description: '', tags: 'a, b', targetStart: '', targetEnd: '2026-05-01' };

  it('edit sends the patch of the form, then reads the page again without showing the skeleton', async () => {
    const { api, s, seen } = writer();
    await s.load('aa');
    seen.length = 0;
    expect(await s.edit('aa', form)).toBe(true);
    expect(api.patch).toHaveBeenCalledWith('aa', { title: 'New', description: '', tags: ['a', 'b'], target_start: null, target_end: '2026-05-01' });
    expect(api.get).toHaveBeenCalledTimes(2);
    expect(s.state.initiative).toBe(later);
    expect(seen.every((st) => !st.loading)).toBe(true);
  });

  it('a refused edit keeps the page, says why in dialogError and reads nothing again', async () => {
    const { api, s } = writer({ patch: async () => Promise.reject(new InitiativesError(400, 'target_end is before target_start')) });
    await s.load('aa');
    expect(await s.edit('aa', form)).toBe(false);
    expect(s.state.dialogError).toBe('target_end is before target_start');
    expect(s.state.initiative).toBe(initiative);
    expect(s.state.error).toBeUndefined();
    expect(api.get).toHaveBeenCalledTimes(1);
  });

  it('the next write and clearDialogError forget a refusal', async () => {
    let fail = true;
    const { s } = writer({ close: async () => (fail ? Promise.reject(new Error('busy')) : {}) });
    await s.load('aa');
    expect(await s.close('aa', false)).toBe(false);
    s.clearDialogError();
    expect(s.state.dialogError).toBeUndefined();
    expect(await s.close('aa', false)).toBe(false);
    fail = false;
    expect(await s.close('aa', false)).toBe(true);
    expect(s.state.dialogError).toBeUndefined();
  });

  it('close passes force through, reopen takes only the slug, and both read the page again', async () => {
    const { api, s } = writer();
    await s.load('aa');
    await s.close('aa', true);
    await s.close('aa', false);
    await s.reopen('aa');
    expect(api.close.mock.calls).toEqual([['aa', true], ['aa', false]]);
    expect(api.reopen.mock.calls).toEqual([['aa']]);
    expect(api.get).toHaveBeenCalledTimes(4);
  });

  it('a load clears the refusal of an earlier write', async () => {
    const { s } = writer({ close: async () => Promise.reject(new Error('no')) });
    await s.load('aa');
    await s.close('aa', false);
    await s.load('aa');
    expect(s.state.dialogError).toBeUndefined();
  });
});

describe('InitiativesApi.get and tasks', () => {
  function api(respond: (url: string) => Response) {
    const fetchFn = vi.fn(async (url: string) => respond(url));
    const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname: '/', setHash: vi.fn() };
    return { api: new InitiativesApi(browser, () => 'csrf'), fetchFn };
  }

  it('get reads the initiative by its encoded slug with the session cookie', async () => {
    const { api: a, fetchFn } = api(() => Response.json(initiative));
    expect(await a.get('aa')).toEqual(initiative);
    expect(fetchFn.mock.calls[0][0]).toBe('/api/v1/initiatives/aa');
    expect((fetchFn.mock.calls[0] as unknown[])[1]).toMatchObject({ credentials: 'same-origin', headers: { Accept: 'application/json' } });
  });

  it('get turns a 404 problem into its detail', async () => {
    const { api: a } = api(() => new Response(JSON.stringify({ title: 'Not Found', detail: 'no initiative zz' }), { status: 404, headers: { 'Content-Type': 'application/problem+json' } }));
    await expect(a.get('zz')).rejects.toMatchObject({ status: 404, message: 'no initiative zz' });
  });

  it('tasks asks for every status of the initiative and follows next_after', async () => {
    const { api: a, fetchFn } = api((url) =>
      url.includes('after=aa-2') ? Response.json({ items: [task('aa-3')] }) : Response.json({ items: [task('aa-1'), task('aa-2')], next_after: 'aa-2' }),
    );
    expect((await a.tasks('aa')).map((t) => t.ref)).toEqual(['aa-1', 'aa-2', 'aa-3']);
    expect(fetchFn.mock.calls.map((c) => c[0])).toEqual(['/api/v1/tasks?initiative=aa&status=all', '/api/v1/tasks?initiative=aa&status=all&after=aa-2']);
  });
});

describe('concerns', () => {
  it('matches the events that name the initiative and no other', () => {
    const mine = concerns('aa');
    expect(mine({ initiative: 'aa' })).toBe(true);
    expect(mine({ initiative: 'bb' })).toBe(false);
    expect(mine({})).toBe(false);
  });
});

describe('the Needs attention section of the page', () => {
  it('reads the tasks of the initiative under the task filter with attention=true', () => {
    expect(pageNeedsQuery('aa', 'not-closed').toString()).toBe('status=not_closed&initiative=aa&attention=true');
    expect(pageNeedsQuery('aa', 'done').toString()).toBe('status=done&initiative=aa&attention=true');
  });
  it('rows carry the tag marks of their task, every source', () => {
    const marks = [
      { tag: 'ui', source: 'task' as const },
      { tag: 'web', source: 'initiative' as const },
    ];
    expect(taskRows([task('aa-1', { tag_marks: marks })], 'all')[0]?.marks).toEqual(marks);
  });
  it('adds each tag chosen, so the section narrows with the table', () => {
    expect(pageNeedsQuery('aa', 'not-closed', ['ui', 'api']).toString()).toBe('status=not_closed&initiative=aa&tag=ui&tag=api&attention=true');
    expect(pageNeedsHref('aa', 'not-closed', ['ui'])).toBe('#/tasks?initiative=aa&tag=ui&attention=true');
  });
  it('links `+K more` to the Tasks view for that initiative and filter', () => {
    expect(pageNeedsHref('aa', 'not-closed')).toBe('#/tasks?initiative=aa&attention=true');
    expect(pageNeedsHref('aa', 'all')).toBe('#/tasks?status=all&initiative=aa&attention=true');
  });
  it('rows carry the attention badges of their task', () => {
    const rows = taskRows([task('aa-1', { attention: [{ reason: 'done' }] })], 'all');
    expect(rows[0]!.attention.map((b) => b.label)).toEqual(['done, not closed']);
  });
});
