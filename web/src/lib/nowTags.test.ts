import { describe, expect, it } from 'vitest';
import type { Attention } from './attentionView';
import { marksOf, nowTaskRefs, taskOfHref } from './nowTags';

const NOW = new Date('2026-01-01T12:00:00Z');
const bob = { handle: 'bob', way_back: 'ralphctl status bob' };
const attention: Attention = {
  counts: { open_asks: 2, done_not_closed: 1, expired_leases: 0, overdue_watches: 0, conflicts: 0, total: 3 },
  open_asks: [
    { ref: 'A1', question: 'Which?', task: 'aa-1', asker: bob, mode: 'poll', asker_waiting: false, blocking: false, created_at: '2026-01-01T11:00:00Z', age_seconds: 3600 },
    { ref: 'A2', question: 'On no task?', asker: bob, mode: 'poll', asker_waiting: false, blocking: false, created_at: '2026-01-01T11:00:00Z', age_seconds: 3600 },
  ],
  done_not_closed: [{ task: 'aa-2', title: 'Ship', done_by: bob, done_at: '2026-01-01T10:00:00Z' }],
};

describe('the task a link names', () => {
  it('is the decoded ref of a task link', () => {
    expect(taskOfHref('#/t/aa-1')).toBe('aa-1');
    expect(taskOfHref('#/t/a%20b-1')).toBe('a b-1');
  });

  it('is nothing for no link, another fragment or a bad escape', () => {
    expect(taskOfHref(undefined)).toBeUndefined();
    expect(taskOfHref('#/i/aa')).toBeUndefined();
    expect(taskOfHref('#/t/%E0%A4%A')).toBeUndefined();
  });
});

describe('the tasks Now shows', () => {
  it('lists the Needs me tasks, then In flight, then Recently touched, each once, and skips an item on no task', () => {
    expect(nowTaskRefs(attention, ['aa-1', 'bb-1'], ['bb-1', 'cc-1'], NOW)).toEqual(['aa-1', 'aa-2', 'bb-1', 'cc-1']);
  });

  it('lists only the flight and recent tasks before the Needs me read answers', () => {
    expect(nowTaskRefs(undefined, ['bb-1'], ['cc-1'], NOW)).toEqual(['bb-1', 'cc-1']);
  });

  it('is empty for nothing', () => {
    expect(nowTaskRefs(undefined, [], [], NOW)).toEqual([]);
  });
});

describe('the marks of a row', () => {
  const marks = { 'aa-1': [{ tag: 'work', source: 'task' as const }] };

  it('are the task’s marks, or none for an unknown task, no task or an unanswered read', () => {
    expect(marksOf(marks, 'aa-1')).toEqual([{ tag: 'work', source: 'task' }]);
    expect(marksOf(marks, 'zz-9')).toEqual([]);
    expect(marksOf(marks, undefined)).toEqual([]);
    expect(marksOf({}, 'aa-1')).toEqual([]);
  });
});
