import { describe, expect, it } from 'vitest';
import { actionLabels, taskActions, type TaskActions } from './taskActions';

const table: [string, string, TaskActions][] = [
  ['open', 'work', { edit: true, done: true, reopen: false, close: true, milestone: true, recordCheck: false }],
  ['open', 'watch', { edit: true, done: true, reopen: false, close: true, milestone: true, recordCheck: true }],
  ['done', 'work', { edit: true, done: false, reopen: true, close: true, milestone: true, recordCheck: false }],
  ['done', 'watch', { edit: true, done: false, reopen: true, close: true, milestone: true, recordCheck: true }],
  ['closed', 'work', { edit: false, done: false, reopen: true, close: false, milestone: true, recordCheck: false }],
  ['closed', 'watch', { edit: false, done: false, reopen: true, close: false, milestone: true, recordCheck: false }],
];

describe('the action availability table', () => {
  it.each(table)('%s %s', (status, kind, actions) => {
    expect(taskActions(status, kind)).toEqual(actions);
  });
});

describe('actionLabels', () => {
  it('lists the available actions in page order', () => {
    expect(actionLabels(taskActions('open', 'watch'))).toEqual(['Edit', 'Done', 'Close', 'Milestone', 'Record check']);
    expect(actionLabels(taskActions('open', 'work'))).toEqual(['Edit', 'Done', 'Close', 'Milestone']);
    expect(actionLabels(taskActions('done', 'work'))).toEqual(['Edit', 'Reopen', 'Close', 'Milestone']);
    expect(actionLabels(taskActions('closed', 'watch'))).toEqual(['Reopen', 'Milestone']);
  });
});
