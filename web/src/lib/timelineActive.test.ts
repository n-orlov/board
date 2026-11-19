import { describe, expect, it } from 'vitest';
import type { Timeline } from './timeline';
import { ACTIVE_CHOICES, ACTIVE_COLUMNS, activeFirst, needsActiveSet, refsOf, windowVisible } from './timelineActive';

type Task = Timeline['tasks'][number];

function task(ref: string, initiative: string): Task {
  return { ref, initiative, title: ref, status: 'open', kind: 'work', created_at: '2026-03-01T00:00:00Z', attention: [], deps: [] };
}

function board(tasks: Task[], slugs: string[]): Timeline {
  return {
    from: '2026-02-24T12:00:00Z',
    to: '2026-03-17T12:00:00Z',
    initiatives: slugs.map((slug) => ({ slug, title: slug, tags: [], closed: false })),
    tasks,
    leases: [],
    markers: [],
  };
}

const refs = (t: Timeline) => t.tasks.map((x) => x.ref);

describe('the window control shows only while Only active is ticked or the sort is Active first', () => {
  it.each([
    [false, 'default', false],
    [true, 'default', true],
    [false, 'active', true],
    [true, 'active', true],
  ] as const)('Only active %s with the %s order: control shown %s', (activeOnly, order, shown) => {
    expect(windowVisible(activeOnly, order)).toBe(shown);
  });
});

describe('the active set is read only for Active first on the whole board', () => {
  it.each([
    [false, 'default', false],
    [true, 'default', false],
    [false, 'active', true],
    [true, 'active', false],
  ] as const)('Only active %s with the %s order: second read %s', (activeOnly, order, second) => {
    expect(needsActiveSet(activeOnly, order)).toBe(second);
  });
});

describe('Active first', () => {
  const tasks = [task('a-1', 'a'), task('a-2', 'a'), task('a-3', 'a'), task('b-1', 'b'), task('b-2', 'b'), task('c-1', 'c')];
  const active = new Set(['a-2', 'b-1', 'b-2']);

  it('puts every active task before every idle one and keeps each group in the current order', () => {
    const got = activeFirst(board(tasks, ['a', 'b', 'c']), active);
    expect(refs(got)).toEqual(['a-2', 'b-1', 'b-2', 'a-1', 'a-3', 'c-1']);
    const firstIdle = got.tasks.findIndex((t) => !active.has(t.ref));
    expect(got.tasks.slice(firstIdle).some((t) => active.has(t.ref))).toBe(false);
  });

  it('puts the initiatives that hold an active task first, in their own order, so no idle row is above an active one', () => {
    const got = activeFirst(board(tasks, ['c', 'a', 'b']), active);
    expect(got.initiatives.map((i) => i.slug)).toEqual(['a', 'b', 'c']);
    expect(activeFirst(board(tasks, ['c', 'b', 'a']), active).initiatives.map((i) => i.slug)).toEqual(['b', 'a', 'c']);
  });

  it('leaves the order alone when every task or none is active, and does not change its input', () => {
    const input = board(tasks, ['a', 'b', 'c']);
    expect(refs(activeFirst(input, new Set(tasks.map((t) => t.ref))))).toEqual(refs(input));
    expect(refs(activeFirst(input, new Set()))).toEqual(refs(input));
    activeFirst(input, active);
    expect(refs(input)).toEqual(['a-1', 'a-2', 'a-3', 'b-1', 'b-2', 'c-1']);
  });

  it('reads the active set from an answer filtered with active=true', () => {
    expect([...refsOf(board([task('a-2', 'a'), task('b-1', 'b')], ['a', 'b']))]).toEqual(['a-2', 'b-1']);
  });

  it('offers the current order and Active first on the shared control', () => {
    expect(ACTIVE_COLUMNS.map((c) => c.column)).toEqual(['active']);
    expect(ACTIVE_CHOICES).toEqual([
      { value: '', label: 'Default order' },
      { value: 'active:desc', label: 'Active first' },
    ]);
  });
});
