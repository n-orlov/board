import { describe, expect, it, vi } from 'vitest';
import { InitiativesError } from './initiatives';
import { depOptions, eventRows, expiredRows, kindText, leaseRows, outcomeProblem, plannedText, RESOLUTIONS, summaryProblem, TaskPageStore, taskHref, type TaskPageState } from './taskPage';
import type { TaskListItem } from './initiatives';
import type { Lease, TaskDetail, TaskEvent } from './tasks';

const NOW = new Date('2026-01-10T12:00:00Z');

describe('plannedText', () => {
  it('names the window by its dates', () => {
    expect(plannedText({ planned_start: '2026-01-01T00:00:00Z', planned_end: '2026-02-01T00:00:00Z' })).toBe('2026-01-01 to 2026-02-01');
    expect(plannedText({ planned_start: '2026-01-01T00:00:00Z' })).toBe('from 2026-01-01');
    expect(plannedText({ planned_end: '2026-02-01T00:00:00Z' })).toBe('until 2026-02-01');
    expect(plannedText({})).toBe('');
  });
});

describe('kindText and taskHref', () => {
  it('adds the cadence to a watch task', () => {
    expect(kindText({ kind: 'work' })).toBe('work');
    expect(kindText({ kind: 'watch', cadence_seconds: 300 })).toBe('watch, every 5 minutes');
  });

  it('links a task page', () => {
    expect(taskHref('aa-1')).toBe('#/t/aa-1');
  });
});

describe('RESOLUTIONS', () => {
  it('offers the four resolutions, completed first', () => {
    expect(RESOLUTIONS.map((r) => r.value)).toEqual(['completed', 'dropped', 'superseded', 'duplicate']);
    expect(RESOLUTIONS.map((r) => r.label)).toEqual(['Completed', 'Dropped', 'Superseded', 'Duplicate']);
  });
});

describe('leaseRows', () => {
  it('shows holder, intent, resources and when the lease runs out', () => {
    const task = {
      live_leases: [
        { ref: 'L1', actor: 'ann', intent: 'edit', resources: ['b.go', 'a.go'], expires_at: '2026-01-10T12:41:00Z' },
        { ref: 'L2', actor: 'bob', intent: 'read', resources: [], expires_at: '2026-01-10T11:58:00Z' },
      ],
    } as unknown as TaskDetail;
    expect(leaseRows(task, NOW)).toEqual([
      { ref: 'L1', holder: 'ann', intent: 'edit', resources: 'b.go, a.go', expires: 'expires in 41m' },
      { ref: 'L2', holder: 'bob', intent: 'read', resources: '', expires: 'expired 2m ago' },
    ]);
    expect(leaseRows({ live_leases: [] }, NOW)).toEqual([]);
  });
});

const event = (n: number, extra: Partial<TaskEvent> = {}): TaskEvent =>
  ({ id: 'id' + n, ref: 'E' + n, at: `2026-01-10T11:${String(10 + n)}:00Z`, type: 'task_done', actor: 'ann', summary: 's' + n, ...extra }) as TaskEvent;

describe('eventRows', () => {
  it('lists the events newest first with a relative time and the absolute one for the title', () => {
    const rows = eventRows([event(1), event(2), event(3)], NOW);
    expect(rows.map((r) => r.ref)).toEqual(['E3', 'E2', 'E1']);
    expect(rows[0]).toMatchObject({ id: 'id3', type: 'task_done', summary: 's3', actor: 'ann', at: '2026-01-10T11:13:00Z', when: '47 minutes ago' });
    expect(rows[0]!.title).toMatch(/^2026-01-10 \d\d:13$/);
  });

  it('does not reorder the events it was given', () => {
    const events = [event(1), event(2)];
    eventRows(events, NOW);
    expect(events.map((e) => e.ref)).toEqual(['E1', 'E2']);
    expect(eventRows([], NOW)).toEqual([]);
  });
});

const detail = (status = 'open') => ({ ref: 'aa-1', status, kind: 'work', title: 'T' }) as TaskDetail;

function harness(overrides: Record<string, ReturnType<typeof vi.fn>> = {}) {
  const api = {
    get: vi.fn(async () => detail()),
    events: vi.fn(async () => [event(1)]),
    expiredLeases: vi.fn(async () => [] as Lease[]),
    liveLeases: vi.fn(async () => [] as Lease[]),
    openTasks: vi.fn(async () => [] as TaskListItem[]),
    addDep: vi.fn(async () => detail()),
    removeDep: vi.fn(async () => detail()),
    milestone: vi.fn(async () => detail()),
    recordCheck: vi.fn(async () => detail()),
    endLease: vi.fn(async () => ({}) as Lease),
    ackLease: vi.fn(async () => ({}) as Lease),
    patch: vi.fn(async () => detail()),
    done: vi.fn(async () => detail('done')),
    reopen: vi.fn(async () => detail()),
    close: vi.fn(async () => detail('closed')),
    ...overrides,
  };
  const states: TaskPageState[] = [];
  const store = new TaskPageStore(api as never, (s) => states.push(s));
  return { api, store, states };
}

const initial = { title: 'T', description: '', plannedStart: '', plannedEnd: '', cadence: '' };

describe('TaskPageStore', () => {
  it('starts loading, then holds the task and its events', async () => {
    const { store, states } = harness();
    expect(store.state).toMatchObject({ task: undefined, events: [], expired: [], live: [], candidates: [], loading: true, error: undefined, actionError: undefined });
    await store.load('aa-1');
    expect(states[0]).toMatchObject({ loading: true });
    expect(store.state.task?.ref).toBe('aa-1');
    expect(store.state.events.map((e) => e.ref)).toEqual(['E1']);
    expect(store.state.loading).toBe(false);
  });

  it('shows one error and no half page when either read fails', async () => {
    const { store } = harness({ events: vi.fn(async () => Promise.reject(new InitiativesError(404, 'task not found'))) });
    await store.load('aa-1');
    expect(store.state).toMatchObject({ task: undefined, events: [], loading: false, error: 'task not found' });
  });

  it('forgets an earlier refusal when the page is loaded again', async () => {
    const { store } = harness({ done: vi.fn(async () => Promise.reject(new Error('nope'))) });
    await store.load('aa-1');
    await store.done('aa-1');
    expect(store.state.actionError).toBe('nope');
    await store.load('aa-1');
    expect(store.state.actionError).toBeUndefined();
  });

  it('runs done, reopen and close, then reads the page again without the skeleton', async () => {
    const { api, store, states } = harness();
    await store.load('aa-1');
    states.length = 0;
    expect(await store.done('aa-1')).toBe(true);
    expect(await store.reopen('aa-1')).toBe(true);
    expect(await store.close('aa-1', 'duplicate')).toBe(true);
    expect(api.done).toHaveBeenCalledWith('aa-1');
    expect(api.reopen).toHaveBeenCalledWith('aa-1');
    expect(api.close).toHaveBeenCalledWith('aa-1', 'duplicate');
    expect(api.get).toHaveBeenCalledTimes(4);
    expect(states.every((s) => !s.loading)).toBe(true);
  });

  it('keeps the page and says why when a write is refused', async () => {
    const { api, store } = harness({ close: vi.fn(async () => Promise.reject(new InitiativesError(409, 'task is closed'))) });
    await store.load('aa-1');
    expect(await store.close('aa-1', 'dropped')).toBe(false);
    expect(store.state.actionError).toBe('task is closed');
    expect(store.state.task?.ref).toBe('aa-1');
    expect(api.get).toHaveBeenCalledTimes(1);
    store.clearActionError();
    expect(store.state.actionError).toBeUndefined();
  });

  it('saves only the fields of the edit form that changed', async () => {
    const { api, store } = harness();
    await store.load('aa-1');
    expect(await store.edit('aa-1', { ...initial, title: 'New' }, initial, 'work')).toBe(true);
    expect(api.patch).toHaveBeenCalledWith('aa-1', { title: 'New' });
    expect(api.get).toHaveBeenCalledTimes(2);
  });

  it('refuses an edit form that cannot be sent without a request', async () => {
    const { api, store } = harness();
    await store.load('aa-1');
    expect(await store.edit('aa-1', { ...initial, cadence: 'soon' }, { ...initial, cadence: '5m' }, 'watch')).toBe(false);
    expect(store.state.actionError).toContain('15m');
    expect(api.patch).not.toHaveBeenCalled();
  });
});

describe('summaryProblem and outcomeProblem', () => {
  it('wants a summary of 1 to 200 characters, counted after trimming', () => {
    expect(summaryProblem('')).toBe('Write a summary.');
    expect(summaryProblem('   ')).toBe('Write a summary.');
    expect(summaryProblem('a')).toBeUndefined();
    expect(summaryProblem(' ' + 'a'.repeat(200) + ' ')).toBeUndefined();
    expect(summaryProblem('a'.repeat(201))).toBe('A summary is at most 200 characters; this one has 201.');
  });

  it('counts characters, not bytes, and lets an outcome be empty', () => {
    expect(summaryProblem('é'.repeat(200))).toBeUndefined();
    expect(outcomeProblem('')).toBeUndefined();
    expect(outcomeProblem('a'.repeat(200))).toBeUndefined();
    expect(outcomeProblem('a'.repeat(201))).toBe('An outcome is at most 200 characters; this one has 201.');
  });
});

describe('depOptions', () => {
  const item = (ref: string, title: string) => ({ id: 'id-' + ref, ref, title }) as TaskListItem;
  const candidates = [item('aa-1', 'Self'), item('aa-2', 'Already a blocker'), item('aa-3', 'Write docs'), item('bb-1', 'Docs review')];
  const task = { ref: 'aa-1', deps: [{ ref: 'aa-2', title: '', status: 'open' }] } as TaskDetail;

  it('leaves out the task itself and the tasks it already waits for', () => {
    expect(depOptions(candidates, task, '').map((t) => t.ref)).toEqual(['aa-3', 'bb-1']);
  });

  it('narrows by the typed text over ref and title', () => {
    expect(depOptions(candidates, task, 'REVIEW').map((t) => t.ref)).toEqual(['bb-1']);
    expect(depOptions(candidates, task, 'aa-').map((t) => t.ref)).toEqual(['aa-3']);
  });
});

describe('expiredRows', () => {
  const lease = (ref: string, extra: Partial<Lease> = {}) =>
    ({ ref, actor: 'ann', intent: 'edit', resources: ['b', 'a'], expires_at: '2026-01-10T11:50:00Z', ...extra }) as Lease;

  it('lists the expired leases nobody acknowledged, with how long ago they ran out', () => {
    const rows = expiredRows([lease('L1'), lease('L2', { acked_at: '2026-01-10T11:55:00Z', acked_by: 'bob' }), lease('L3', { resources: [] })], NOW);
    expect(rows).toEqual([
      { ref: 'L1', holder: 'ann', intent: 'edit', resources: 'b, a', expired: 'expired 10m ago' },
      { ref: 'L3', holder: 'ann', intent: 'edit', resources: '', expired: 'expired 10m ago' },
    ]);
    expect(expiredRows([], NOW)).toEqual([]);
  });
});

describe('TaskPageStore: dependencies, milestones, checks and leases', () => {
  it('reads the expired leases with the page', async () => {
    const { store } = harness({ expiredLeases: vi.fn(async () => [{ ref: 'L1' } as Lease]) });
    await store.load('aa-1');
    expect(store.state.expired.map((l) => l.ref)).toEqual(['L1']);
  });

  it('reads the live leases of the board with the page', async () => {
    const { store } = harness({ liveLeases: vi.fn(async () => [{ ref: 'L2' } as Lease]) });
    await store.load('aa-1');
    expect(store.state.live.map((l) => l.ref)).toEqual(['L2']);
  });

  it('shows one error and no live leases when that read fails', async () => {
    const { store } = harness({ liveLeases: vi.fn(async () => Promise.reject(new InitiativesError(500, 'down'))) });
    await store.load('aa-1');
    expect(store.state).toMatchObject({ task: undefined, live: [], error: 'down' });
  });

  it('shows one error and no expired leases when that read fails', async () => {
    const { store } = harness({ expiredLeases: vi.fn(async () => Promise.reject(new InitiativesError(500, 'down'))) });
    await store.load('aa-1');
    expect(store.state).toMatchObject({ task: undefined, expired: [], error: 'down' });
  });

  it('adds and removes a dependency, then reads the page again', async () => {
    const { api, store } = harness();
    await store.load('aa-1');
    expect(await store.addDep('aa-1', 'bb-2')).toBe(true);
    expect(await store.removeDep('aa-1', 'bb-2')).toBe(true);
    expect(api.addDep).toHaveBeenCalledWith('aa-1', 'bb-2');
    expect(api.removeDep).toHaveBeenCalledWith('aa-1', 'bb-2');
    expect(api.get).toHaveBeenCalledTimes(3);
  });

  it('keeps the refusal of a dependency (a cycle, say) for the dialog', async () => {
    const { store } = harness({ addDep: vi.fn(async () => Promise.reject(new InitiativesError(409, 'would close a cycle'))) });
    await store.load('aa-1');
    expect(await store.addDep('aa-1', 'bb-2')).toBe(false);
    expect(store.state.actionError).toBe('would close a cycle');
  });

  it('sends the trimmed summary of a milestone and of a check', async () => {
    const { api, store } = harness();
    await store.load('aa-1');
    expect(await store.milestone('aa-1', '  shipped  ')).toBe(true);
    expect(await store.recordCheck('aa-1', ' still green ')).toBe(true);
    expect(api.milestone).toHaveBeenCalledWith('aa-1', 'shipped');
    expect(api.recordCheck).toHaveBeenCalledWith('aa-1', 'still green');
  });

  it('refuses an empty or over-long summary without a request', async () => {
    const { api, store } = harness();
    await store.load('aa-1');
    expect(await store.milestone('aa-1', ' ')).toBe(false);
    expect(store.state.actionError).toBe('Write a summary.');
    expect(await store.recordCheck('aa-1', 'x'.repeat(201))).toBe(false);
    expect(store.state.actionError).toContain('at most 200');
    expect(api.milestone).not.toHaveBeenCalled();
    expect(api.recordCheck).not.toHaveBeenCalled();
  });

  it('keeps the refusal of a check on a task that is not a watch task', async () => {
    const { store } = harness({ recordCheck: vi.fn(async () => Promise.reject(new InitiativesError(409, 'not a watch task'))) });
    await store.load('aa-1');
    expect(await store.recordCheck('aa-1', 'ok')).toBe(false);
    expect(store.state.actionError).toBe('not a watch task');
  });

  it('ends a lease with its trimmed outcome and acknowledges an expired one', async () => {
    const { api, store } = harness();
    await store.load('aa-1');
    expect(await store.endLease('aa-1', 'L1', ' done ')).toBe(true);
    expect(await store.endLease('aa-1', 'L2', '')).toBe(true);
    expect(await store.ackLease('aa-1', 'L3')).toBe(true);
    expect(api.endLease.mock.calls).toEqual([['L1', 'done'], ['L2', '']]);
    expect(api.ackLease).toHaveBeenCalledWith('L3');
  });

  it('refuses an over-long outcome without a request and keeps the refusal of a stale lease', async () => {
    const { api, store } = harness({ ackLease: vi.fn(async () => Promise.reject(new InitiativesError(409, 'lease has not expired'))) });
    await store.load('aa-1');
    expect(await store.endLease('aa-1', 'L1', 'x'.repeat(201))).toBe(false);
    expect(api.endLease).not.toHaveBeenCalled();
    expect(await store.ackLease('aa-1', 'L1')).toBe(false);
    expect(store.state.actionError).toBe('lease has not expired');
  });

  it('reads the open tasks a dependency can be added on, or says why it could not', async () => {
    const open = [{ ref: 'bb-1' } as TaskListItem];
    const { store } = harness({ openTasks: vi.fn(async () => open) });
    await store.loadCandidates();
    expect(store.state.candidates).toEqual(open);
    const failing = harness({ openTasks: vi.fn(async () => Promise.reject(new InitiativesError(500, 'down'))) });
    await failing.store.loadCandidates();
    expect(failing.store.state).toMatchObject({ candidates: [], actionError: 'down' });
  });
});
