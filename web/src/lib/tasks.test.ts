import { describe, expect, it, vi } from 'vitest';
import { InitiativesError, type InitiativeListItem, type TaskListItem } from './initiatives';
import { emptyNewTask } from './newTask';
import { DEFAULT_TASKS_FILTER } from './taskFilters';
import { NewTaskStore, openInitiatives, TasksApi, TasksStore, type NewTaskState, type TasksState } from './tasks';
import type { Browser } from './session';

const task = (ref: string) => ({ id: 'id-' + ref, ref, title: 'Title ' + ref }) as TaskListItem;
const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const problem = (status: number, body: object) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/problem+json' } });

function api(respond: (url: string, init?: RequestInit) => Promise<Response>, pathname = '/') {
  const fetchFn = vi.fn((input: string, init?: RequestInit) => respond(input, init));
  const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname, setHash: vi.fn() };
  return { api: new TasksApi(browser, () => 'csrf-1'), fetchFn };
}

describe('TasksApi', () => {
  it('lists the tasks of a filter with the API query, following next_after through the pages', async () => {
    const { api: a, fetchFn } = api(async (url) =>
      url.includes('after=') ? json({ items: [task('aa-2')] }) : json({ items: [task('aa-1')], next_after: 'aa-1' }),
    );
    const items = await a.list({ ...DEFAULT_TASKS_FILTER, kind: 'watch', blocked: 'yes', text: 'a b' });
    expect(items.map((t) => t.ref)).toEqual(['aa-1', 'aa-2']);
    expect(fetchFn.mock.calls.map((c) => c[0])).toEqual([
      '/api/v1/tasks?status=not_closed&kind=watch&blocked=true&q=a+b',
      '/api/v1/tasks?status=not_closed&kind=watch&blocked=true&q=a+b&after=aa-1',
    ]);
    expect(fetchFn.mock.calls[0]![1]).toMatchObject({ credentials: 'same-origin', headers: { Accept: 'application/json' } });
  });

  it('reads the open tasks of every initiative', async () => {
    const { api: a, fetchFn } = api(async () => json({ items: [task('aa-1')] }));
    expect((await a.openTasks()).map((t) => t.ref)).toEqual(['aa-1']);
    expect(fetchFn.mock.calls[0]![0]).toBe('/api/v1/tasks?status=open');
  });

  it('honours the mount prefix', async () => {
    const { api: a, fetchFn } = api(async () => json({ items: [] }), '/board/');
    await a.openTasks();
    expect(fetchFn.mock.calls[0]![0]).toBe('/board/api/v1/tasks?status=open');
  });

  it('posts a create with the JSON body and the CSRF token and returns the task', async () => {
    const { api: a, fetchFn } = api(async () => json({ ref: 'aa-1' }, 201));
    const made = await a.create({ initiative: 'aa', title: 'T' });
    expect(made.ref).toBe('aa-1');
    const [url, init] = fetchFn.mock.calls[0]!;
    expect(url).toBe('/api/v1/tasks');
    expect(init).toMatchObject({ method: 'POST', credentials: 'same-origin', body: '{"initiative":"aa","title":"T"}' });
    expect(init!.headers).toMatchObject({ 'Content-Type': 'application/json', 'X-CSRF-Token': 'csrf-1' });
  });

  it('turns a refusal into an error carrying the status and the problem detail', async () => {
    const { api: a } = api(async () => problem(409, { title: 'Conflict', detail: 'initiative is closed' }));
    await expect(a.create({ initiative: 'aa', title: 'T' })).rejects.toMatchObject({ status: 409, message: expect.stringContaining('initiative is closed') });
  });

  it('says the server is not reachable when the request fails', async () => {
    const { api: a } = api(async () => {
      throw new Error('offline');
    });
    await expect(a.openTasks()).rejects.toEqual(new InitiativesError(0, 'The server is not reachable.'));
  });
});

describe('TasksStore', () => {
  function store(list: () => Promise<TaskListItem[]>) {
    const seen: TasksState[] = [];
    return { s: new TasksStore({ list }, (st) => seen.push(st)), seen };
  }

  it('starts loading under the default filter, or the one it is given', () => {
    expect(store(async () => []).s.state).toEqual({ tasks: [], filter: DEFAULT_TASKS_FILTER, loading: true, error: undefined });
    const filter = { ...DEFAULT_TASKS_FILTER, kind: 'watch' as const };
    expect(new TasksStore({ list: async () => [] }, () => undefined, filter).state.filter).toBe(filter);
  });

  it('loads the tasks and stops loading', async () => {
    const { s } = store(async () => [task('aa-1')]);
    await s.load();
    expect(s.state).toMatchObject({ loading: false, error: undefined });
    expect(s.state.tasks.map((t) => t.ref)).toEqual(['aa-1']);
  });

  it('shows the error of a failed load and stops loading', async () => {
    const { s } = store(async () => {
      throw new InitiativesError(500, 'boom');
    });
    await s.load();
    expect(s.state).toMatchObject({ loading: false, error: 'boom', tasks: [] });
  });

  it('fetches under the new filter when it is set', async () => {
    const list = vi.fn(async () => [task('aa-1')]);
    const { s } = store(list);
    const filter = { ...DEFAULT_TASKS_FILTER, status: 'done' as const };
    await s.setFilter(filter);
    expect(list).toHaveBeenCalledWith(filter);
    expect(s.state.filter).toBe(filter);
  });

  it('shows the newest fetch when an older one lands after it', async () => {
    const resolvers: ((t: TaskListItem[]) => void)[] = [];
    const { s } = store(() => new Promise((resolve) => resolvers.push(resolve)));
    const first = s.load();
    const second = s.load();
    resolvers[1]!([task('new')]);
    await second;
    resolvers[0]!([task('old')]);
    await first;
    expect(s.state.tasks.map((t) => t.ref)).toEqual(['new']);
    expect(s.state.loading).toBe(false);
  });
});

describe('NewTaskStore', () => {
  const initiatives = [{ slug: 'aa' }] as InitiativeListItem[];
  function store(over: Partial<ConstructorParameters<typeof NewTaskStore>[0]> = {}) {
    const seen: NewTaskState[] = [];
    const create = vi.fn(async () => ({ ref: 'aa-7' }) as never);
    const s = new NewTaskStore({ initiatives: async () => initiatives, openTasks: async () => [task('aa-1')], create, ...over }, (st) => seen.push(st));
    return { s, seen, create };
  }

  it('starts loading with nothing to pick from', () => {
    expect(store().s.state).toEqual({ initiatives: [], openTasks: [], loading: true, error: undefined });
  });

  it('loads the open initiatives and the open tasks', async () => {
    const { s } = store();
    await s.load();
    expect(s.state).toMatchObject({ loading: false, error: undefined, initiatives });
    expect(s.state.openTasks.map((t) => t.ref)).toEqual(['aa-1']);
  });

  it('shows why the pickers could not be read', async () => {
    const { s } = store({
      openTasks: async () => {
        throw new InitiativesError(500, 'no tasks');
      },
    });
    await s.load();
    expect(s.state).toMatchObject({ loading: false, error: 'no tasks', initiatives: [], openTasks: [] });
  });

  it('creates the task from the form and gives back its ref', async () => {
    const { s, create } = store();
    const ref = await s.create({ ...emptyNewTask('aa'), title: ' T ' });
    expect(ref).toBe('aa-7');
    expect(create).toHaveBeenCalledWith({ initiative: 'aa', title: 'T', kind: 'work' });
    expect(s.state.error).toBeUndefined();
  });

  it('refuses a form the body cannot be built from without calling the API', async () => {
    const { s, create } = store();
    expect(await s.create({ ...emptyNewTask('aa'), kind: 'watch' })).toBeUndefined();
    expect(create).not.toHaveBeenCalled();
    expect(s.state.error).toContain('Cadence');
  });

  it('keeps the refusal of the server for the form and forgets it when asked', async () => {
    const { s } = store({
      create: async () => {
        throw new InitiativesError(409, 'initiative is closed');
      },
    });
    expect(await s.create(emptyNewTask('aa'))).toBeUndefined();
    expect(s.state.error).toBe('initiative is closed');
    s.clearError();
    expect(s.state.error).toBeUndefined();
  });

  it('clears an earlier refusal when a create works', async () => {
    const { s } = store();
    await s.create({ ...emptyNewTask('aa'), kind: 'watch' });
    expect(s.state.error).toBeDefined();
    await s.create(emptyNewTask('aa'));
    expect(s.state.error).toBeUndefined();
  });
});

describe('openInitiatives', () => {
  it('lists the open initiatives', async () => {
    const list = vi.fn(async () => initiativesOf(['aa']));
    expect(await openInitiatives({ list })()).toEqual(initiativesOf(['aa']));
    expect(list).toHaveBeenCalledWith('open');
  });
});

function initiativesOf(slugs: string[]) {
  return slugs.map((slug) => ({ slug }) as InitiativeListItem);
}

describe('TasksApi task page calls', () => {
  it('reads a task by its encoded ref', async () => {
    const { api: a, fetchFn } = api(async () => json({ ref: 'aa-1' }));
    expect((await a.get('aa-1')).ref).toBe('aa-1');
    expect(fetchFn.mock.calls[0]![0]).toBe('/api/v1/tasks/aa-1');
    expect(fetchFn.mock.calls[0]![1]).toMatchObject({ credentials: 'same-origin', headers: { Accept: 'application/json' } });
    await a.get('a/b');
    expect(fetchFn.mock.calls[1]![0]).toBe('/api/v1/tasks/a%2Fb');
  });

  it('reads every event, following next_after through the pages', async () => {
    const { api: a, fetchFn } = api(async (url) =>
      url.includes('after=') ? json({ items: [{ ref: 'E2' }] }) : json({ items: [{ ref: 'E1' }], next_after: 'E1' }),
    );
    expect((await a.events('aa-1')).map((e) => e.ref)).toEqual(['E1', 'E2']);
    expect(fetchFn.mock.calls.map((c) => c[0])).toEqual(['/api/v1/tasks/aa-1/events?limit=1000', '/api/v1/tasks/aa-1/events?limit=1000&after=E1']);
  });

  it('patches with the JSON body and the CSRF token', async () => {
    const { api: a, fetchFn } = api(async () => json({ ref: 'aa-1' }));
    await a.patch('aa-1', { title: 'N', planned_end: null });
    const [url, init] = fetchFn.mock.calls[0]!;
    expect(url).toBe('/api/v1/tasks/aa-1');
    expect(init).toMatchObject({ method: 'PATCH', credentials: 'same-origin', body: '{"title":"N","planned_end":null}' });
    expect(init!.headers).toMatchObject({ 'Content-Type': 'application/json', 'X-CSRF-Token': 'csrf-1' });
  });

  it('posts done and reopen without a body, and close with its resolution', async () => {
    const { api: a, fetchFn } = api(async () => json({ ref: 'aa-1' }));
    await a.done('aa-1');
    await a.reopen('aa-1');
    await a.close('aa-1', 'superseded');
    const calls = fetchFn.mock.calls.map((c) => [c[0], (c[1] as RequestInit).method, (c[1] as RequestInit).body]);
    expect(calls).toEqual([
      ['/api/v1/tasks/aa-1/done', 'POST', undefined],
      ['/api/v1/tasks/aa-1/reopen', 'POST', undefined],
      ['/api/v1/tasks/aa-1/close', 'POST', '{"resolution":"superseded"}'],
    ]);
    expect(fetchFn.mock.calls[0]![1]!.headers).toEqual({ Accept: 'application/json', 'X-CSRF-Token': 'csrf-1' });
    expect(fetchFn.mock.calls[2]![1]!.headers).toMatchObject({ 'Content-Type': 'application/json' });
  });

  it('names the failed action when a write is refused without a detail', async () => {
    const { api: a } = api(async () => new Response('', { status: 500 }));
    await expect(a.done('aa-1')).rejects.toMatchObject({ status: 500, message: 'Marking the task done failed (HTTP 500).' });
  });
});

describe('TasksApi dependency, note and lease calls', () => {
  const calls = (fetchFn: { mock: { calls: unknown[][] } }) => fetchFn.mock.calls.map((c) => [c[0], (c[1] as RequestInit).method, (c[1] as RequestInit).body]);

  it('adds and removes a dependency on the encoded refs, without a body', async () => {
    const { api: a, fetchFn } = api(async () => json({ ref: 'aa-1' }));
    await a.addDep('aa-1', 'bb-2');
    await a.removeDep('aa-1', 'b/b');
    expect(calls(fetchFn)).toEqual([
      ['/api/v1/tasks/aa-1/deps/bb-2', 'POST', undefined],
      ['/api/v1/tasks/aa-1/deps/b%2Fb', 'DELETE', undefined],
    ]);
    expect(fetchFn.mock.calls[1]![1]!.headers).toEqual({ Accept: 'application/json', 'X-CSRF-Token': 'csrf-1' });
  });

  it('posts a milestone and a check with their summary', async () => {
    const { api: a, fetchFn } = api(async () => json({ ref: 'aa-1' }));
    await a.milestone('aa-1', 'shipped');
    await a.recordCheck('aa-1', 'green');
    expect(calls(fetchFn)).toEqual([
      ['/api/v1/tasks/aa-1/milestones', 'POST', '{"summary":"shipped"}'],
      ['/api/v1/tasks/aa-1/checks', 'POST', '{"summary":"green"}'],
    ]);
  });

  it('reads the expired leases of a task, following next_after through the pages', async () => {
    const { api: a, fetchFn } = api(async (url) => (url.includes('after=') ? json({ items: [{ ref: 'L2' }] }) : json({ items: [{ ref: 'L1' }], next_after: 'L1' })));
    expect((await a.expiredLeases('aa-1')).map((l) => l.ref)).toEqual(['L1', 'L2']);
    expect(fetchFn.mock.calls.map((c) => c[0])).toEqual([
      '/api/v1/leases?task=aa-1&state=expired&limit=1000',
      '/api/v1/leases?task=aa-1&state=expired&limit=1000&after=L1',
    ]);
  });

  it('reads every live lease of the board, following next_after through the pages', async () => {
    const { api: a, fetchFn } = api(async (url) => (url.includes('after=') ? json({ items: [{ ref: 'L2' }] }) : json({ items: [{ ref: 'L1' }], next_after: 'L1' })));
    expect((await a.liveLeases()).map((l) => l.ref)).toEqual(['L1', 'L2']);
    expect(fetchFn.mock.calls.map((c) => c[0])).toEqual(['/api/v1/leases?state=live&limit=1000', '/api/v1/leases?state=live&limit=1000&after=L1']);
  });

  it('ends a lease with its outcome only when there is one, and acknowledges an expired lease', async () => {
    const { api: a, fetchFn } = api(async () => json({ ref: 'L1' }));
    expect((await a.endLease('L1', 'done')).ref).toBe('L1');
    await a.endLease('L1', '');
    await a.ackLease('L1');
    expect(calls(fetchFn)).toEqual([
      ['/api/v1/leases/L1/end', 'POST', '{"outcome":"done"}'],
      ['/api/v1/leases/L1/end', 'POST', undefined],
      ['/api/v1/leases/L1/ack', 'POST', undefined],
    ]);
  });

  it('names the failed action when a lease write is refused without a detail', async () => {
    const { api: a } = api(async () => new Response('', { status: 500 }));
    await expect(a.endLease('L1', '')).rejects.toMatchObject({ message: 'Ending the lease failed (HTTP 500).' });
    await expect(a.ackLease('L1')).rejects.toMatchObject({ message: 'Acknowledging the lease failed (HTTP 500).' });
    await expect(a.addDep('aa-1', 'bb-1')).rejects.toMatchObject({ message: 'Adding the dependency failed (HTTP 500).' });
    await expect(a.removeDep('aa-1', 'bb-1')).rejects.toMatchObject({ message: 'Removing the dependency failed (HTTP 500).' });
    await expect(a.milestone('aa-1', 's')).rejects.toMatchObject({ message: 'Recording the milestone failed (HTTP 500).' });
    await expect(a.recordCheck('aa-1', 's')).rejects.toMatchObject({ message: 'Recording the check failed (HTTP 500).' });
    await expect(a.expiredLeases('aa-1')).rejects.toMatchObject({ message: 'Reading the expired leases failed (HTTP 500).' });
    await expect(a.liveLeases()).rejects.toMatchObject({ message: 'Reading the live leases failed (HTTP 500).' });
  });
});
