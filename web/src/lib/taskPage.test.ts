import { describe, expect, it, vi } from 'vitest';
import { InitiativesError } from './initiatives';
import { eventRows, kindText, leaseRows, plannedText, RESOLUTIONS, TaskPageStore, taskHref, type TaskPageState } from './taskPage';
import type { TaskDetail, TaskEvent } from './tasks';

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
    expect(store.state).toMatchObject({ task: undefined, events: [], loading: true, error: undefined, actionError: undefined });
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
