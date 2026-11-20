import { describe, expect, it } from 'vitest';
import { buildTimeline, type Timeline } from './timeline';
import { ACTIVE_CHOICES, ACTIVE_COLUMNS, activeFirst, needsActiveSet, refsOf, windowVisible } from './timelineActive';

type Task = Timeline['tasks'][number];

function task(ref: string, initiative: string): Task {
  return { ref, initiative, title: ref, status: 'open', kind: 'work', created_at: '2026-03-01T00:00:00Z', attention: [], deps: [], tags: [], tag_marks: [] };
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
  const NOW = new Date('2026-03-16T12:00:00Z');
  const tasks = [task('a-1', 'a'), task('a-2', 'a'), task('a-3', 'a'), task('b-1', 'b'), task('b-2', 'b'), task('c-1', 'c')];
  const active = new Set(['a-2', 'b-1', 'b-2']);

  /** The rows as the chart draws them, top to bottom (vis-timeline orders groups by `order`). */
  const drawn = (t: Timeline) => [...buildTimeline(t, NOW).groups].sort((x, y) => x.order - y.order);
  /** The task rows, top to bottom. */
  const rows = (t: Timeline) => drawn(t).flatMap((g) => (g.nestedGroups === undefined ? [g.ref] : []));
  /** Each task row with the initiative row it is drawn under: the closest initiative row above it, which lists it. */
  const parents = (t: Timeline) => {
    const out: [string, string][] = [];
    let parent: { id: string; ref: string; nestedGroups?: string[] } | undefined;
    for (const g of drawn(t)) {
      if (g.nestedGroups !== undefined) parent = g;
      else {
        expect(parent?.nestedGroups).toContain(g.id);
        expect(g.nestedInGroup).toBe(parent?.id);
        out.push([g.ref, parent!.ref]);
      }
    }
    return out;
  };

  it('draws every active row above every idle one, each part in the current order, across interleaved initiatives', () => {
    const got = activeFirst(board(tasks, ['a', 'b', 'c']), active);
    expect(rows(got)).toEqual(['a-2', 'b-1', 'b-2', 'a-1', 'a-3', 'c-1']);
    const firstIdle = rows(got).findIndex((ref) => !active.has(ref));
    expect(rows(got).slice(firstIdle).some((ref) => active.has(ref))).toBe(false);
  });

  it('draws an active task of a later initiative above an idle task of an earlier one (A-active, A-idle, B-active)', () => {
    const t = board([task('a-1', 'a'), task('a-2', 'a'), task('b-1', 'b')], ['a', 'b']);
    expect(rows(t)).toEqual(['a-1', 'a-2', 'b-1']);
    const got = activeFirst(t, new Set(['a-1', 'b-1']));
    expect(rows(got)).toEqual(['a-1', 'b-1', 'a-2']);
    expect(parents(got)).toEqual([
      ['a-1', 'a'],
      ['b-1', 'b'],
      ['a-2', 'a'],
    ]);
    expect(drawn(got).map((g) => g.id)).toEqual(['i:a', 't:a-1', 'i:b', 't:b-1', 'i:a:idle', 't:a-2']);
  });

  it('keeps the initiatives in their answer order within each part, and an initiative without a task among the idle', () => {
    const got = activeFirst(board(tasks, ['c', 'd', 'b', 'a']), active);
    expect(rows(got)).toEqual(['b-1', 'b-2', 'a-2', 'c-1', 'a-1', 'a-3']);
    expect(drawn(got).flatMap((g) => (g.nestedGroups === undefined ? [] : [g.id]))).toEqual(['i:b', 'i:a', 'i:c', 'i:d', 'i:a:idle']);
    parents(got);
  });

  it('draws the current order when every task or none is active, and does not change its input', () => {
    const input = board(tasks, ['a', 'b', 'c']);
    const current = drawn(input).map((g) => g.id);
    expect(drawn(activeFirst(input, new Set(tasks.map((t) => t.ref)))).map((g) => g.id)).toEqual(current);
    expect(drawn(activeFirst(input, new Set())).map((g) => g.id)).toEqual(current);
    activeFirst(input, active);
    expect(refs(input)).toEqual(['a-1', 'a-2', 'a-3', 'b-1', 'b-2', 'c-1']);
    expect(input).not.toHaveProperty('activeFirst');
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
