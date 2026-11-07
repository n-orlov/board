import { describe, expect, it } from 'vitest';
import { closeBody, closedToast, closeLabel, notClosedCount, REOPEN_BODY } from './initiativeDialogs';

describe('notClosedCount', () => {
  it('counts the open and done tasks and not the closed ones', () => {
    expect(notClosedCount([{ status: 'open' }, { status: 'done' }, { status: 'closed' }, { status: 'open' }])).toBe(3);
    expect(notClosedCount([])).toBe(0);
    expect(notClosedCount([{ status: 'closed' }])).toBe(0);
  });
});

describe('closeLabel', () => {
  it('is a plain close with nothing to drop, else names how many tasks it drops', () => {
    expect(closeLabel(0)).toBe('Close initiative');
    expect(closeLabel(1)).toBe('Close and drop 1 task');
    expect(closeLabel(2)).toBe('Close and drop 2 tasks');
  });
});

describe('closeBody', () => {
  it('says the effect with no open tasks and how many tasks it drops otherwise', () => {
    expect(closeBody(0)).toBe('Closing stops new tasks and edits to its tasks until it is reopened.');
    expect(closeBody(1)).toBe('1 task not closed. Closing drops it too: each is closed as dropped.');
    expect(closeBody(3)).toBe('3 tasks not closed. Closing drops them too: each is closed as dropped.');
  });
});

describe('closedToast and REOPEN_BODY', () => {
  it('names the initiative and the tasks dropped, when any', () => {
    expect(closedToast('aa', 0)).toBe('Initiative aa closed');
    expect(closedToast('aa', 1)).toBe('Initiative aa closed, 1 task dropped');
    expect(closedToast('aa', 2)).toBe('Initiative aa closed, 2 tasks dropped');
  });

  it('reopening says the closed tasks stay closed', () => {
    expect(REOPEN_BODY).toContain('closed tasks stay closed');
  });
});
