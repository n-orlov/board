import { describe, expect, it, vi } from 'vitest';
import {
  NEEDS_LIMIT,
  NO_NEEDS,
  NeedsStore,
  moreText,
  needsHeading,
  needsQuery,
  readNeeds,
  withoutNeeds,
  type NeedsAttention,
  type NeedsState,
  type TaskList,
} from './needsAttention';
import type { TaskListItem } from './initiatives';

const item = (ref: string, reason: 'stranded' | 'asking' | 'done', at = '2026-01-01T00:00:00Z'): TaskListItem =>
  ({ id: 'id-' + ref, ref, updated_at: at, attention: [{ reason }] }) as unknown as TaskListItem;

describe('needsQuery', () => {
  it('keeps the filters, sets attention=true and the limit, and drops a cursor', () => {
    const q = needsQuery(new URLSearchParams('status=all&initiative=aa&limit=5&after=x&attention=false'));
    expect(q.get('status')).toBe('all');
    expect(q.get('initiative')).toBe('aa');
    expect(q.get('attention')).toBe('true');
    expect(q.get('limit')).toBe('200');
    expect(q.has('after')).toBe(false);
    expect(NEEDS_LIMIT).toBe(200);
  });
  it('does not change the query it is given', () => {
    const base = new URLSearchParams('status=all');
    needsQuery(base);
    expect(base.toString()).toBe('status=all');
  });
});

describe('texts', () => {
  it('words the more line and the heading', () => {
    expect(moreText(7)).toBe('+7 more');
    expect(needsHeading({ items: [item('a-1', 'done')], more: 0 })).toBe('Needs attention (1)');
    expect(needsHeading({ items: [item('a-1', 'done')], more: 4 })).toBe('Needs attention (5)');
  });
});

describe('readNeeds', () => {
  it('orders one page by the attention order and counts nothing more', async () => {
    const read = vi.fn(async (_q: URLSearchParams): Promise<TaskList> => ({ items: [item('a-1', 'done'), item('a-2', 'stranded'), item('a-3', 'asking')] }));
    const got = await readNeeds(read, new URLSearchParams('status=all'));
    expect(got.items.map((t) => t.ref)).toEqual(['a-2', 'a-3', 'a-1']);
    expect(got.more).toBe(0);
    expect(read).toHaveBeenCalledTimes(1);
    expect(read.mock.calls[0]![0].toString()).toBe('status=all&attention=true&limit=200');
  });

  it('counts the tasks past the limit by following the pages', async () => {
    const pages: Record<string, TaskList> = {
      '': { items: Array.from({ length: 200 }, (_, n) => item(`a-${n}`, 'done')), next_after: 'c1' },
      c1: { items: [item('b-1', 'done'), item('b-2', 'done')], next_after: 'c2' },
      c2: { items: [item('b-3', 'done')] },
    };
    const queries: string[] = [];
    const read = async (q: URLSearchParams) => {
      queries.push(q.toString());
      return pages[q.get('after') ?? '']!;
    };
    const got = await readNeeds(read, new URLSearchParams());
    expect(got.items).toHaveLength(200);
    expect(got.more).toBe(3);
    expect(queries[1]).toBe('attention=true&limit=1000&after=c1');
    expect(queries[2]).toBe('attention=true&limit=1000&after=c2');
  });

  it('shows at most 200 even when the API returns a larger page', async () => {
    const read = async (): Promise<TaskList> => ({ items: Array.from({ length: 201 }, (_, n) => item(`a-${n}`, 'done')) });
    expect((await readNeeds(read, new URLSearchParams())).items).toHaveLength(200);
  });

  it('passes a failure on', async () => {
    await expect(readNeeds(async () => Promise.reject(new Error('down')), new URLSearchParams())).rejects.toThrow('down');
  });
});

describe('withoutNeeds', () => {
  it('drops the tasks the section shows and keeps the rest in order', () => {
    const needs: NeedsAttention = { items: [item('a-2', 'done')], more: 0 };
    expect(withoutNeeds([item('a-1', 'done'), item('a-2', 'done'), item('a-3', 'done')], needs).map((t) => t.ref)).toEqual(['a-1', 'a-3']);
    expect(withoutNeeds([item('a-1', 'done')], NO_NEEDS)).toHaveLength(1);
  });
});

describe('NeedsStore', () => {
  const some = (ref: string): NeedsAttention => ({ items: [item(ref, 'done')], more: 0 });

  it('starts empty and holds what a load reads', async () => {
    const seen: NeedsState[] = [];
    const fetch = vi.fn(async () => some('a-1'));
    const s = new NeedsStore(fetch, (st) => seen.push(st));
    expect(s.state).toEqual({ needs: NO_NEEDS, error: undefined });
    await s.load(new URLSearchParams('initiative=aa'));
    expect(s.state.needs.items.map((t) => t.ref)).toEqual(['a-1']);
    expect(fetch).toHaveBeenCalledWith(new URLSearchParams('initiative=aa'));
    expect(seen).toHaveLength(1);
  });

  it('shows an empty section without a fetch for a filter that cannot hold one', async () => {
    const fetch = vi.fn(async () => some('a-1'));
    const s = new NeedsStore(fetch, () => undefined);
    await s.load(new URLSearchParams());
    await s.load(null);
    expect(s.state).toEqual({ needs: NO_NEEDS, error: undefined });
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('keeps the section it had on a failure and says why', async () => {
    let fail = false;
    const s = new NeedsStore(async () => (fail ? Promise.reject(new Error('The server is not reachable.')) : some('a-1')), () => undefined);
    await s.load(new URLSearchParams());
    fail = true;
    await s.load(new URLSearchParams());
    expect(s.state.needs.items.map((t) => t.ref)).toEqual(['a-1']);
    expect(s.state.error).toBe('The server is not reachable.');
    fail = false;
    await s.load(new URLSearchParams());
    expect(s.state.error).toBeUndefined();
  });

  it('drops a fetch that lands after a newer one', async () => {
    const gates: ((n: NeedsAttention) => void)[] = [];
    const s = new NeedsStore(() => new Promise<NeedsAttention>((resolve) => gates.push(resolve)), () => undefined);
    const first = s.load(new URLSearchParams('a=1'));
    const second = s.load(new URLSearchParams('a=2'));
    gates[1]!(some('new-1'));
    await second;
    gates[0]!(some('old-1'));
    await first;
    expect(s.state.needs.items.map((t) => t.ref)).toEqual(['new-1']);
  });
});
