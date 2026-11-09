import { describe, expect, it, vi } from 'vitest';
import type { Lease } from './conflicts';
import { InitiativesError } from './initiatives';
import { affectsNow, NowStore, type NowState } from './nowPanel';

const lease = (ref: string) => ({ ref }) as Lease;

function harness(liveLeases: () => Promise<Lease[]>) {
  const states: NowState[] = [];
  const store = new NowStore({ liveLeases }, (s) => states.push(s));
  return { store, states };
}

describe('NowStore', () => {
  it('starts loading, then holds the live leases', async () => {
    const { store, states } = harness(async () => [lease('L1'), lease('L2')]);
    expect(store.state).toEqual({ leases: [], loading: true, error: undefined });
    await store.load();
    expect(states[0]).toMatchObject({ loading: true });
    expect(store.state.leases.map((l) => l.ref)).toEqual(['L1', 'L2']);
    expect(store.state).toMatchObject({ loading: false, error: undefined });
  });

  it('says why a read failed and keeps the leases it had', async () => {
    const reads = [vi.fn(async () => [lease('L1')]), vi.fn(async () => Promise.reject(new InitiativesError(500, 'down')))];
    const { store } = harness(() => reads.shift()!());
    await store.load();
    await store.load();
    expect(store.state).toMatchObject({ error: 'down', loading: false });
    expect(store.state.leases.map((l) => l.ref)).toEqual(['L1']);
  });

  it('clears the error when a later read works', async () => {
    const answers = [() => Promise.reject(new InitiativesError(500, 'down')), async () => [lease('L3')]];
    const { store } = harness(() => answers.shift()!());
    await store.load();
    expect(store.state.error).toBe('down');
    await store.load();
    expect(store.state).toMatchObject({ error: undefined });
    expect(store.state.leases).toHaveLength(1);
  });

  it('shows the newest outcome when reads overlap and land out of order', async () => {
    const resolvers: ((l: Lease[]) => void)[] = [];
    const { store } = harness(() => new Promise<Lease[]>((resolve) => resolvers.push(resolve)));
    const first = store.load();
    const second = store.load();
    resolvers[1]!([lease('L2')]);
    await second;
    resolvers[0]!([lease('L1')]);
    await first;
    expect(store.state.leases.map((l) => l.ref)).toEqual(['L2']);
    expect(store.state.loading).toBe(false);
  });
});

describe('NowStore refresh (a live event)', () => {
  it('swaps the leases without ever making the panel loading', async () => {
    const answers = [async () => [lease('L1')], async () => [lease('L1'), lease('L2')]];
    const { store, states } = harness(() => answers.shift()!());
    await store.load();
    states.length = 0;
    await store.refresh();
    expect(store.state.leases.map((l) => l.ref)).toEqual(['L1', 'L2']);
    expect(states.some((s) => s.loading)).toBe(false);
  });

  it('keeps the leases it had when the read fails and says why', async () => {
    const answers = [async () => [lease('L1')], () => Promise.reject(new InitiativesError(500, 'down'))];
    const { store } = harness(() => answers.shift()!());
    await store.load();
    await store.refresh();
    expect(store.state).toMatchObject({ error: 'down', loading: false });
    expect(store.state.leases.map((l) => l.ref)).toEqual(['L1']);
  });

  it('drops an older read that lands after a newer one', async () => {
    const resolvers: ((l: Lease[]) => void)[] = [];
    const { store } = harness(() => new Promise((resolve) => resolvers.push(resolve)));
    const first = store.refresh();
    const second = store.refresh();
    resolvers[1]!([lease('new')]);
    await second;
    resolvers[0]!([lease('old')]);
    await first;
    expect(store.state.leases.map((l) => l.ref)).toEqual(['new']);
  });

  it('a first load still pending keeps the panel loading after a refresh lands', async () => {
    const resolvers: ((l: Lease[]) => void)[] = [];
    const { store } = harness(() => new Promise((resolve) => resolvers.push(resolve)));
    const first = store.load();
    const again = store.refresh();
    resolvers[1]!([lease('x')]);
    await again;
    expect(store.state.loading).toBe(true);
    resolvers[0]!([lease('old')]);
    await first;
    expect(store.state.loading).toBe(false);
    expect(store.state.leases.map((l) => l.ref)).toEqual(['x']);
  });
});

describe('affectsNow', () => {
  it('is true for lease events only', () => {
    for (const type of ['lease_started', 'lease_renewed', 'lease_ended', 'lease_acked', 'lease_expired']) expect(affectsNow({ type })).toBe(true);
    expect(affectsNow({ type: 'task_done' })).toBe(false);
    expect(affectsNow({ type: 'note_posted' })).toBe(false);
  });
});
