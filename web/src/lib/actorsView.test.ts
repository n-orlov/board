import { describe, expect, it, vi } from 'vitest';
import {
  actorRows,
  ActorsApi,
  ActorsStore,
  affectsActors,
  copyToast,
  copyWayBackLabel,
  orderActors,
  type ActorsState,
  type ActorView,
} from './actorsView';
import type { Browser } from './session';

const NOW = new Date('2026-03-10T12:00:00Z');
const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

function actor(handle: string, lastSeen: string, over: Partial<ActorView> = {}): ActorView {
  return { handle, kind: 'agent', last_seen: lastSeen, leases: [], asks: [], ...over };
}

const LEASE = { ref: 'L7', task: 'aa-3', intent: 'edit the parser', expires_at: '2026-03-10T12:41:00Z', resources: ['repo:x'] };

describe('orderActors', () => {
  it('puts the actors with a live lease first, each group newest last_seen first, ties by handle', () => {
    const list = [
      actor('idle-new', '2026-03-10T11:59:00Z'),
      actor('busy-old', '2026-03-01T00:00:00Z', { leases: [LEASE] }),
      actor('idle-b', '2026-03-09T00:00:00Z'),
      actor('idle-a', '2026-03-09T00:00:00Z'),
      actor('busy-new', '2026-03-10T11:00:00Z', { leases: [LEASE] }),
    ];
    expect(orderActors(list).map((a) => a.handle)).toEqual(['busy-new', 'busy-old', 'idle-new', 'idle-a', 'idle-b']);
  });

  it('leaves its input alone', () => {
    const list = [actor('b', '2026-03-09T00:00:00Z'), actor('a', '2026-03-09T00:00:00Z', { leases: [LEASE] })];
    orderActors(list);
    expect(list.map((a) => a.handle)).toEqual(['b', 'a']);
  });

});

describe('actorRows', () => {
  it('draws a live lease as its ref, the task it links to, the intent and when it runs out', () => {
    const [row] = actorRows([actor('ann', '2026-03-10T11:57:00Z', { leases: [LEASE], way_back: 'tmux attach -t ann', asks: [{ ref: 'A1', question: 'q', created_at: '2026-03-10T00:00:00Z' } as never] })], NOW);
    expect(row).toMatchObject({
      handle: 'ann',
      kind: 'agent',
      alive: true,
      wayBack: 'tmux attach -t ann',
      lastSeen: '3 minutes ago',
      openAsks: 1,
      leases: [{ ref: 'L7', task: 'aa-3', href: '#/t/aa-3', intent: 'edit the parser', expires: 'expires in 41m', resources: ['repo:x'] }],
    });
    expect(row!.lastSeenTitle).toMatch(/^\d{4}-\d\d-\d\d \d\d:\d\d$/);
  });

  it('draws an idle actor as not alive, with no leases', () => {
    const [row] = actorRows([actor('bob', '2026-03-10T11:00:00Z')], NOW);
    expect(row).toMatchObject({ alive: false, leases: [], openAsks: 0, lastSeen: '1 hour ago' });
  });

  it('has no way back for an actor that has none, an empty one included', () => {
    expect(actorRows([actor('a', '2026-03-10T11:00:00Z')], NOW)[0]!.wayBack).toBeUndefined();
    expect(actorRows([actor('a', '2026-03-10T11:00:00Z', { way_back: '' })], NOW)[0]!.wayBack).toBeUndefined();
  });

  it('is empty for no actors', () => {
    expect(actorRows([], NOW)).toEqual([]);
  });
});

describe('the copy words', () => {
  it('names the actor on the button and in the toast, and tells how to copy by hand when it failed', () => {
    expect(copyWayBackLabel('ann')).toBe('Copy way back of ann');
    expect(copyToast('ann', true)).toBe('Copied the way back of ann');
    expect(copyToast('ann', false)).toBe('Copying failed. Select the way back and copy it by hand.');
  });
});

describe('affectsActors', () => {
  it('refetches on any board write but an API key event', () => {
    expect(affectsActors({ type: 'lease_started' })).toBe(true);
    expect(affectsActors({ type: 'note_posted' })).toBe(true);
    expect(affectsActors({ type: 'key_created' })).toBe(false);
  });
});

describe('ActorsApi', () => {
  it('lists GET /actors, same origin, under the mount prefix', async () => {
    const fetchFn = vi.fn(async (_input: string, _init?: RequestInit) => json({ items: [actor('ann', '2026-03-10T11:00:00Z')] }));
    const browser: Browser = { fetch: fetchFn, assign: vi.fn(), pathname: '/board/', setHash: vi.fn() };
    expect((await new ActorsApi(browser, () => 't').list()).map((a) => a.handle)).toEqual(['ann']);
    expect(fetchFn.mock.calls[0]![0]).toBe('/board/api/v1/actors');
    expect(fetchFn.mock.calls[0]![1]?.credentials).toBe('same-origin');
    expect(fetchFn.mock.calls[0]![1]?.headers).toEqual({ Accept: 'application/json' });
  });

  it('names what it was doing when the read is refused', async () => {
    const browser: Browser = { fetch: vi.fn(async () => new Response('', { status: 500 })), assign: vi.fn(), pathname: '/', setHash: vi.fn() };
    await expect(new ActorsApi(browser, () => 't').list()).rejects.toThrow(/Listing the actors/);
  });
});

describe('ActorsStore', () => {
  function setup(list: () => Promise<ActorView[]>) {
    const states: ActorsState[] = [];
    const store = new ActorsStore({ list }, (s) => states.push(s));
    return { store, states };
  }

  it('starts loading, with nothing loaded', () => {
    expect(setup(async () => []).store.state).toEqual({ actors: [], loaded: false, loading: true, error: '' });
  });

  it('shows the actors once the read lands', async () => {
    const { store, states } = setup(async () => [actor('ann', '2026-03-10T11:00:00Z')]);
    await store.load();
    expect(states.at(-1)).toMatchObject({ loaded: true, loading: false, error: '' });
    expect(states.at(-1)!.actors.map((a) => a.handle)).toEqual(['ann']);
    expect(store.state).toBe(states.at(-1));
  });

  it('keeps what it shows and says why when a refresh fails', async () => {
    let fail = false;
    const { store } = setup(async () => {
      if (fail) throw new Error('The server is not reachable.');
      return [actor('ann', '2026-03-10T11:00:00Z')];
    });
    await store.load();
    fail = true;
    await store.load();
    expect(store.state).toMatchObject({ loaded: true, loading: false, error: 'The server is not reachable.' });
    expect(store.state.actors).toHaveLength(1);
  });

  it('drops an answer that lands after a newer one', async () => {
    const resolvers: ((a: ActorView[]) => void)[] = [];
    const { store } = setup(() => new Promise((resolve) => resolvers.push(resolve)));
    const first = store.load();
    const second = store.load();
    resolvers[1]!([actor('new', '2026-03-10T11:00:00Z')]);
    await second;
    resolvers[0]!([actor('stale', '2026-03-09T11:00:00Z')]);
    await first;
    expect(store.state.actors.map((a) => a.handle)).toEqual(['new']);
    expect(store.state.loading).toBe(false);
  });
});
