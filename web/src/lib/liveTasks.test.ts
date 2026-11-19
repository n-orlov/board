import { describe, expect, it, vi } from 'vitest';
import { affectsLiveTasks, countLiveTasks, liveTasksQuery } from './liveTasks';
import type { components } from './api/schema';

type TaskListItem = components['schemas']['TaskListItem'];

const item = (ref: string) => ({ ref }) as unknown as TaskListItem;

describe('liveTasksQuery', () => {
  it('asks for the open tasks that have a live lease, at the largest page', () => {
    expect(liveTasksQuery().toString()).toBe('status=open&active=true&limit=1000');
  });
});

describe('countLiveTasks', () => {
  it('counts the tasks the query keeps', async () => {
    const listQuery = vi.fn().mockResolvedValue([item('a-1'), item('a-2'), item('b-1')]);
    expect(await countLiveTasks({ listQuery })).toBe(3);
    expect(listQuery.mock.calls[0][0].get('active')).toBe('true');
    expect(listQuery.mock.calls[0][0].get('status')).toBe('open');
  });

  it('is zero when no task holds a live lease', async () => {
    expect(await countLiveTasks({ listQuery: vi.fn().mockResolvedValue([]) })).toBe(0);
  });

  it('rejects when the read fails, so the caller keeps the count it showed', async () => {
    await expect(countLiveTasks({ listQuery: vi.fn().mockRejectedValue(new Error('down')) })).rejects.toThrow('down');
  });
});

describe('affectsLiveTasks', () => {
  it('is true for an event that names a task and false for one that does not', () => {
    expect(affectsLiveTasks({ type: 'lease_started', task: 'a-1' })).toBe(true);
    expect(affectsLiveTasks({ type: 'note_posted' })).toBe(false);
  });
});
