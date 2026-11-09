import { describe, expect, it } from 'vitest';
import {
  answeredLine,
  blockedReasons,
  blockingLabel,
  canAct,
  modeLabel,
  openAsks,
  orderAsks,
  stateLabel,
  stateLine,
  withdrawnLine,
  type Ask,
} from './asks';

function ask(over: Partial<Ask>): Ask {
  return {
    id: 'id',
    ref: 'A1',
    asker: 'ralph',
    question: 'Which database?',
    details: '',
    options: [],
    mode: 'stop',
    blocking: false,
    state: 'open',
    created_at: '2026-01-01T00:00:00Z',
    asker_waiting: false,
    ...over,
  };
}

describe('an open ask', () => {
  it('is labelled open, can be acted on, and says the asker stopped', () => {
    const a = ask({});
    expect(stateLabel(a.state)).toBe('Open');
    expect(canAct(a)).toBe(true);
    expect(stateLine(a)).toBe('asker stopped');
    expect(answeredLine(a)).toBe('');
    expect(withdrawnLine(a)).toBe('');
  });

  it('says whether the polling asker is waiting', () => {
    expect(stateLine(ask({ mode: 'poll', asker_waiting: true }))).toBe('asker waiting');
    expect(stateLine(ask({ mode: 'poll', asker_waiting: false }))).toBe('asker not waiting');
  });

  it('says it blocks its task only when it is blocking', () => {
    expect(blockingLabel(ask({ blocking: true }))).toBe('blocks its task');
    expect(blockingLabel(ask({ blocking: false }))).toBe('');
  });

  it('names its mode', () => {
    expect(modeLabel('stop')).toContain('stop');
    expect(modeLabel('poll')).toContain('poll');
  });
});

describe('an answered ask', () => {
  const answered = ask({ state: 'answered', answer: 'Postgres', answered_by: 'alice', channel: 'ui' });

  it('says who answered and through which channel', () => {
    expect(stateLabel(answered.state)).toBe('Answered');
    expect(stateLine(answered)).toBe('answered by alice in the UI');
    expect(answeredLine({ ...answered, channel: 'api', answered_by: 'ralph' })).toBe('answered by ralph through the API');
  });

  it('names the person a relayed answer came from', () => {
    const relayed = { ...answered, channel: 'relay' as const, answered_by: 'ralph', on_behalf_of: 'bob' };
    expect(answeredLine(relayed)).toBe('answered by ralph relayed (on behalf of bob)');
  });

  it('copes with a missing answerer and channel', () => {
    expect(answeredLine(ask({ state: 'answered' }))).toBe('answered');
  });

  it('takes no action, and no longer blocks', () => {
    expect(canAct(answered)).toBe(false);
    expect(blockingLabel({ ...answered, blocking: true })).toBe('blocked its task');
  });
});

describe('a withdrawn ask', () => {
  it('says it was withdrawn, with the reason when there is one', () => {
    const withdrawn = ask({ state: 'withdrawn', withdraw_reason: 'solved it myself' });
    expect(stateLabel(withdrawn.state)).toBe('Withdrawn');
    expect(stateLine(withdrawn)).toBe('withdrawn: solved it myself');
    expect(withdrawnLine(ask({ state: 'withdrawn', withdraw_reason: '  ' }))).toBe('withdrawn');
    expect(withdrawnLine(ask({ state: 'withdrawn' }))).toBe('withdrawn');
    expect(canAct(withdrawn)).toBe(false);
  });
});

describe('ordering', () => {
  const list = [
    ask({ ref: 'A1', state: 'withdrawn' }),
    ask({ ref: 'A2', state: 'answered' }),
    ask({ ref: 'A3', state: 'open' }),
    ask({ ref: 'A4', state: 'answered' }),
    ask({ ref: 'A5', state: 'open' }),
  ];

  it('puts open first, then answered, then withdrawn, keeping the order within a state', () => {
    expect(orderAsks(list).map((a) => a.ref)).toEqual(['A3', 'A5', 'A2', 'A4', 'A1']);
  });

  it('does not change the list it is given', () => {
    orderAsks(list);
    expect(list[0]!.ref).toBe('A1');
  });

  it('keeps only the open asks for the initiative page', () => {
    expect(openAsks(list).map((a) => a.ref)).toEqual(['A3', 'A5']);
  });
});

describe('the reasons a task is blocked', () => {
  it('lists the open blocking asks only', () => {
    const list = [
      ask({ ref: 'A1', blocking: true, question: 'Which database?' }),
      ask({ ref: 'A2', blocking: false }),
      ask({ ref: 'A3', blocking: true, state: 'answered' }),
    ];
    expect(blockedReasons(list)).toEqual(['blocked by A1: Which database?']);
    expect(blockedReasons([])).toEqual([]);
  });
});
