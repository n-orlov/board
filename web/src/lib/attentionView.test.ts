import { describe, expect, it, vi } from 'vitest';
import { DEFAULT_ATTENTION_FILTER } from './attention';
import {
  AttentionApi,
  AttentionStore,
  affectsAttention,
  copyLabel,
  filterChoices,
  rowsFor,
  type Attention,
  type AttentionState,
} from './attentionView';
import type { Browser } from './session';

const NOW = new Date('2026-01-01T12:00:00Z');
const counts = { open_asks: 0, done_not_closed: 0, expired_leases: 0, overdue_watches: 0, conflicts: 0, total: 0 };
const bob = { handle: 'bob', way_back: 'ralphctl status bob' };
const sue = { handle: 'sue' };

const full: Attention = {
  counts,
  open_asks: [
    { ref: 'A1', question: 'Which one?', task: 'aa-1', asker: bob, mode: 'poll', asker_waiting: true, blocking: true, created_at: '2026-01-01T11:00:00Z', age_seconds: 3600 },
    { ref: 'A2', question: 'Free?', asker: sue, mode: 'stop', asker_waiting: false, blocking: false, created_at: '2026-01-01T11:59:50Z', age_seconds: 10 },
  ],
  done_not_closed: [
    {
      task: 'aa-2',
      title: 'Ship it',
      done_by: bob,
      done_at: '2026-01-01T10:00:00Z',
      last_lease: { lease: 'L3', holder: sue, outcome: 'shipped', ended_at: '2026-01-01T09:59:00Z' },
    },
    { task: 'aa-4', title: 'No lease', done_by: sue, done_at: '2026-01-01T11:59:30Z', last_lease: { lease: 'L9', holder: bob, ended_at: '2026-01-01T09:59:00Z' } },
    { task: 'aa-5', title: 'Plain', done_by: sue, done_at: '2026-01-01T11:59:30Z' },
  ],
  expired_leases: [{ lease: 'L4', holder: bob, task: 'aa-3', title: 'Slow', intent: 'ride', renewed_at: '2026-01-01T08:00:00Z', expired_at: '2026-01-01T09:00:00Z' }],
  overdue_watches: [
    { task: 'aa-6', title: 'Watch', owner: bob, next_check_at: '2026-01-01T10:00:00Z', last_check: { summary: 'all well', by: sue, at: '2026-01-01T09:00:00Z' } },
    { task: 'aa-7', title: 'Fresh', owner: sue, next_check_at: '2026-01-01T10:00:00Z' },
  ],
  conflicts: [
    {
      resource: 'db/main',
      holders: [
        { lease: 'L1', actor: bob, task: 'aa-1', intent: 'migrate', expires_at: '2026-01-01T13:00:00Z' },
        { lease: 'L2', actor: sue, task: 'aa-2', intent: 'vacuum', expires_at: '2026-01-01T13:00:00Z' },
      ],
      tasks: ['aa-1', 'aa-2'],
    },
    {
      resource: 'solo',
      holders: [{ lease: 'L5', actor: bob, task: 'aa-1', intent: 'x', expires_at: '2026-01-01T13:00:00Z' }],
      tasks: ['aa-1'],
    },
  ],
};

describe('rowsFor', () => {
  it('shows an ask with its question, task link, age, mode and the asker with the way back', () => {
    const [a1, a2] = rowsFor('open_asks', full, NOW);
    expect(a1).toEqual({
      id: 'A1',
      title: 'A1: Which one?',
      href: '#/t/aa-1',
      details: ['asked 1 hour ago', 'asker waiting', 'blocking'],
      actors: [{ role: 'asked by', handle: 'bob', wayBack: 'ralphctl status bob' }],
    });
    expect(a2).toEqual({
      id: 'A2',
      title: 'A2: Free?',
      href: undefined,
      details: ['asked just now', 'asker stopped'],
      actors: [{ role: 'asked by', handle: 'sue', wayBack: undefined }],
    });
  });

  it('shows a done task with who marked it done, when, and its last lease outcome', () => {
    const [done, noOutcome, plain] = rowsFor('done_not_closed', full, NOW);
    expect(done).toEqual({
      id: 'aa-2',
      title: 'aa-2 Ship it',
      href: '#/t/aa-2',
      details: ['done 2 hours ago', 'last lease L3: shipped'],
      actors: [
        { role: 'done by', handle: 'bob', wayBack: 'ralphctl status bob' },
        { role: 'last lease', handle: 'sue', wayBack: undefined },
      ],
    });
    expect(noOutcome!.details).toEqual(['done just now']);
    expect(noOutcome!.actors.map((a) => a.role)).toEqual(['done by', 'last lease']);
    expect(plain!.actors.map((a) => a.role)).toEqual(['done by']);
    expect(plain!.details).toEqual(['done just now']);
  });

  it('shows an expired lease with its holder, intent and expiry', () => {
    expect(rowsFor('expired_leases', full, NOW)).toEqual([
      {
        id: 'L4',
        title: 'L4 on aa-3 Slow',
        href: '#/t/aa-3',
        details: ['ride', 'expired 3 hours ago'],
        actors: [{ role: 'held by', handle: 'bob', wayBack: 'ralphctl status bob' }],
      },
    ]);
  });

  it('shows an overdue watch with its owner and the last check when there is one', () => {
    const [checked, fresh] = rowsFor('overdue_watches', full, NOW);
    expect(checked!.details).toEqual(['check was due 2 hours ago', 'last check: all well']);
    expect(checked!.actors.map((a) => [a.role, a.handle])).toEqual([
      ['owner', 'bob'],
      ['last check by', 'sue'],
    ]);
    expect(fresh!.details).toEqual(['check was due 2 hours ago']);
    expect(fresh!.actors.map((a) => a.role)).toEqual(['owner']);
  });

  it('shows a conflict with every holder; it links the task only when there is one', () => {
    const [many, one] = rowsFor('conflicts', full, NOW);
    expect(many).toEqual({
      id: 'db/main',
      title: 'db/main',
      href: undefined,
      details: ['L1 on aa-1: migrate', 'L2 on aa-2: vacuum'],
      actors: [
        { role: 'L1', handle: 'bob', wayBack: 'ralphctl status bob' },
        { role: 'L2', handle: 'sue', wayBack: undefined },
      ],
    });
    expect(one!.href).toBe('#/t/aa-1');
  });

  it('has no rows for a section the answer lacks', () => {
    for (const key of ['open_asks', 'done_not_closed', 'expired_leases', 'overdue_watches', 'conflicts'] as const) {
      expect(rowsFor(key, { counts }, NOW)).toEqual([]);
    }
  });

  it('treats a blank way back as none', () => {
    const odd: Attention = { counts, expired_leases: [{ ...full.expired_leases![0]!, holder: { handle: 'x', way_back: '' } }] };
    expect(rowsFor('expired_leases', odd, NOW)[0]!.actors[0]!.wayBack).toBeUndefined();
  });

  it('encodes a task ref in its link', () => {
    const odd: Attention = { counts, overdue_watches: [{ ...full.overdue_watches![1]!, task: 'a b' }] };
    expect(rowsFor('overdue_watches', odd, NOW)[0]!.href).toBe('#/t/a%20b');
  });
});

describe('affectsAttention', () => {
  it.each([['ask_raised'], ['task_done'], ['lease_expired'], ['check_recorded'], ['initiative_updated']])('keeps %s', (type) => {
    expect(affectsAttention({ type })).toBe(true);
  });
  it.each([['note_posted'], ['key_created'], ['key_revoked']])('ignores %s', (type) => {
    expect(affectsAttention({ type })).toBe(false);
  });
});

describe('copyLabel', () => {
  it('names the actor', () => {
    expect(copyLabel({ role: 'r', handle: 'bob', wayBack: 'x' })).toBe('Copy way back of bob');
  });
});

describe('filterChoices', () => {
  it('offers the initiatives and tags, sorted and once, plus what the filter names', () => {
    const list = [
      { slug: 'b', tags: ['work', 'a'] },
      { slug: 'a', tags: ['work'] },
    ];
    expect(filterChoices(list, { initiatives: ['z'], tags: ['q', 'a'] })).toEqual({ initiatives: ['a', 'b', 'z'], tags: ['a', 'q', 'work'] });
    expect(filterChoices([], DEFAULT_ATTENTION_FILTER)).toEqual({ initiatives: [], tags: [] });
  });
});

function browser(respond: (url: string) => Promise<Response>, pathname = '/'): { browser: Browser; fetchFn: ReturnType<typeof vi.fn> } {
  const fetchFn = vi.fn((url: string) => respond(url));
  return { browser: { fetch: fetchFn, assign: vi.fn(), pathname, setHash: vi.fn() }, fetchFn };
}

describe('AttentionApi', () => {
  it('reads /attention with the filter as repeated keys and the session cookie', async () => {
    const { browser: b, fetchFn } = browser(async () => new Response(JSON.stringify(full)));
    const got = await new AttentionApi(b).get({ tags: ['a', 'b'], initiatives: ['x'] });
    expect(got).toEqual(full);
    expect(fetchFn.mock.calls[0]![0]).toBe('/api/v1/attention?tag=a&tag=b&initiative=x');
    expect(fetchFn.mock.calls[0]![1]).toMatchObject({ credentials: 'same-origin', headers: { Accept: 'application/json' } });
  });

  it('asks for no query without a filter and follows the mount prefix', async () => {
    const { browser: b, fetchFn } = browser(async () => new Response(JSON.stringify({ counts })), '/board/');
    await new AttentionApi(b).get(DEFAULT_ATTENTION_FILTER);
    expect(fetchFn.mock.calls[0]![0]).toBe('/board/api/v1/attention');
  });

  it('says the server is not reachable when the fetch throws', async () => {
    const { browser: b } = browser(async () => Promise.reject(new TypeError('down')));
    await expect(new AttentionApi(b).get(DEFAULT_ATTENTION_FILTER)).rejects.toThrow('The server is not reachable.');
  });

  it('reports a refusal by its problem detail, else by its status', async () => {
    const problem = browser(async () => new Response(JSON.stringify({ detail: 'no such initiative' }), { status: 404, headers: { 'Content-Type': 'application/problem+json' } }));
    await expect(new AttentionApi(problem.browser).get(DEFAULT_ATTENTION_FILTER)).rejects.toThrow('no such initiative');
    const bare = browser(async () => new Response('x', { status: 500 }));
    await expect(new AttentionApi(bare.browser).get(DEFAULT_ATTENTION_FILTER)).rejects.toThrow('Reading what needs attention failed (HTTP 500).');
  });
});

describe('AttentionStore', () => {
  function rig(get: (f: unknown) => Promise<Attention>) {
    const states: AttentionState[] = [];
    const store = new AttentionStore({ get } as never, (s) => states.push(s));
    return { store, states, last: () => states[states.length - 1]! };
  }

  it('loads the sections under the filter and stops loading', async () => {
    const { store, last } = rig(async () => full);
    await store.load();
    expect(last()).toMatchObject({ attention: full, loading: false, error: undefined });
  });

  it('keeps what is shown while a refresh is out, and shows the error when it fails', async () => {
    let fail = false;
    const { store, states, last } = rig(async () => {
      if (fail) throw new Error('boom');
      return full;
    });
    await store.load();
    fail = true;
    await store.load();
    expect(states.some((s) => s.loading && s.attention === full)).toBe(true);
    expect(last()).toMatchObject({ attention: full, error: 'boom', loading: false });
  });

  it('clears the error once a load works again', async () => {
    let fail = true;
    const { store, last } = rig(async () => {
      if (fail) throw new Error('boom');
      return full;
    });
    await store.load();
    expect(last().error).toBe('boom');
    fail = false;
    await store.load();
    expect(last().error).toBeUndefined();
  });

  it('fetches under the new filter, and drops an answer for an older one that lands late', async () => {
    const waits: ((a: Attention) => void)[] = [];
    const seen: unknown[] = [];
    const { store, last } = rig((f) => {
      seen.push(f);
      return new Promise<Attention>((resolve) => waits.push(resolve));
    });
    const first = store.load();
    const second = store.setFilter({ tags: ['a'], initiatives: [] });
    waits[1]!({ counts, conflicts: [] });
    await second;
    waits[0]!(full);
    await first;
    expect(last().filter).toEqual({ tags: ['a'], initiatives: [] });
    expect(last().attention).toEqual({ counts, conflicts: [] });
    expect(seen[1]).toEqual({ tags: ['a'], initiatives: [] });
    expect(last().loading).toBe(false);
  });

  it('stays loading while a fetch is out', async () => {
    const waits: ((a: Attention) => void)[] = [];
    const { store, last } = rig(() => new Promise<Attention>((resolve) => waits.push(resolve)));
    const p = store.load();
    expect(last().loading).toBe(true);
    waits[0]!(full);
    await p;
    expect(last().loading).toBe(false);
  });
});
