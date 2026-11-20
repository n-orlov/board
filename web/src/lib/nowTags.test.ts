import { describe, expect, it } from 'vitest';
import { marksOf, taskOfHref } from './nowTags';

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

describe('the marks of a row', () => {
  const marks = { 'aa-1': [{ tag: 'work', source: 'task' as const }] };

  it('are the task’s marks, or none for an unknown task, no task or an unanswered read', () => {
    expect(marksOf(marks, 'aa-1')).toEqual([{ tag: 'work', source: 'task' }]);
    expect(marksOf(marks, 'zz-9')).toEqual([]);
    expect(marksOf(marks, undefined)).toEqual([]);
    expect(marksOf({}, 'aa-1')).toEqual([]);
  });
});
