import { describe, expect, it, vi } from 'vitest';
import {
  ActionsApi,
  AttentionActions,
  actionLabel,
  actionName,
  actionsFor,
  answerProblem,
  initiativeHref,
  initiativeOfHref,
  initiativeOfTask,
  planFor,
  reasonProblem,
  resolutionProblem,
  type ActionApi,
  type ActionsState,
  type Ask,
} from './attentionActions';
import type { Browser } from './session';

const ASK = { ref: 'A1', question: 'Which?', details: 'See **this**', options: ['Left', 'Right'], state: 'open' } as unknown as Ask;

function fakeApi(over: Partial<ActionApi> = {}): ActionApi {
  return {
    ask: vi.fn(async () => ASK),
    answer: vi.fn(async () => ({})),
    withdraw: vi.fn(async () => ({})),
    close: vi.fn(async () => ({})),
    acknowledge: vi.fn(async () => ({})),
    check: vi.fn(async () => ({})),
    ...over,
  };
}

function rig(over: Partial<ActionApi> = {}) {
  const api = fakeApi(over);
  const states: ActionsState[] = [];
  const actions = new AttentionActions(api, (s) => states.push(s));
  return { api, actions, states };
}

describe('what each section offers', () => {
  it('lists the actions in the order they are drawn', () => {
    expect(actionsFor('open_asks')).toEqual(['answer', 'withdraw']);
    expect(actionsFor('done_not_closed')).toEqual(['close']);
    expect(actionsFor('expired_leases')).toEqual(['acknowledge']);
    expect(actionsFor('overdue_watches')).toEqual(['check']);
    expect(actionsFor('conflicts')).toEqual([]);
  });

  it('names the buttons', () => {
    expect(actionLabel('check')).toBe('Record check');
    expect(actionLabel('acknowledge')).toBe('Acknowledge');
    expect(actionName('answer', 'A3')).toBe('Answer A3');
  });
});

describe('initiative links', () => {
  it('reads the slug from a task ref', () => {
    expect(initiativeOfTask('aa-12')).toBe('aa');
    expect(initiativeOfTask('my-slug-3')).toBe('my-slug');
    expect(initiativeOfTask('nodash')).toBeUndefined();
    expect(initiativeOfTask('-3')).toBeUndefined();
    expect(initiativeOfTask('aa-x')).toBeUndefined();
  });

  it('reads it from an item link', () => {
    expect(initiativeOfHref('#/t/aa-1')).toBe('aa');
    expect(initiativeOfHref('#/t/my%2Dslug-3')).toBe('my-slug');
    expect(initiativeOfHref(undefined)).toBeUndefined();
    expect(initiativeOfHref('#/other/aa-1')).toBeUndefined();
    expect(initiativeOfHref('#/t/%E0%A4%A-1')).toBeUndefined();
    expect(initiativeHref('my slug')).toBe('#/i/my%20slug');
  });
});

describe('input checks', () => {
  it('refuses an empty or oversized answer', () => {
    expect(answerProblem('  ')).toMatch(/Write an answer/);
    expect(answerProblem('x'.repeat(20001))).toBe('An answer is at most 20000 characters; this one has 20001.');
    expect(answerProblem('x'.repeat(20000))).toBeUndefined();
  });

  it('allows an empty reason but not one over 200', () => {
    expect(reasonProblem('')).toBeUndefined();
    expect(reasonProblem('x'.repeat(200))).toBeUndefined();
    expect(reasonProblem('x'.repeat(201))).toBe('A reason is at most 200 characters; this one has 201.');
  });

  it('knows the four resolutions', () => {
    for (const r of ['completed', 'dropped', 'superseded', 'duplicate']) expect(resolutionProblem(r)).toBeUndefined();
    expect(resolutionProblem('')).toBe('Pick a resolution.');
    expect(resolutionProblem('gone')).toBe('Pick a resolution.');
  });
});

describe('plans', () => {
  it('send the trimmed input to the right call', async () => {
    const api = fakeApi();
    await planFor(api, 'answer', 'A1', ' Left ').call();
    await planFor(api, 'withdraw', 'A1', ' no longer ').call();
    await planFor(api, 'close', 'aa-1', 'dropped').call();
    await planFor(api, 'acknowledge', 'L2', '').call();
    await planFor(api, 'check', 'aa-2', ' all well ').call();
    expect(api.answer).toHaveBeenCalledWith('A1', 'Left');
    expect(api.withdraw).toHaveBeenCalledWith('A1', 'no longer');
    expect(api.close).toHaveBeenCalledWith('aa-1', 'dropped');
    expect(api.acknowledge).toHaveBeenCalledWith('L2');
    expect(api.check).toHaveBeenCalledWith('aa-2', 'all well');
  });

  it('say what worked', () => {
    const api = fakeApi();
    expect(planFor(api, 'answer', 'A1', 'x').done).toBe('Answered A1');
    expect(planFor(api, 'withdraw', 'A1', '').done).toBe('Withdrew A1');
    expect(planFor(api, 'close', 'aa-1', 'completed').done).toBe('Task aa-1 closed as completed');
    expect(planFor(api, 'acknowledge', 'L2', '').done).toBe('Lease L2 acknowledged');
    expect(planFor(api, 'check', 'aa-2', 'x').done).toBe('Check recorded on aa-2');
  });

  it('carry the input problem of the action', () => {
    const api = fakeApi();
    expect(planFor(api, 'answer', 'A1', '').problem).toBeDefined();
    expect(planFor(api, 'check', 'aa-2', '').problem).toBe('Write a summary.');
    expect(planFor(api, 'close', 'aa-1', '').problem).toBe('Pick a resolution.');
    expect(planFor(api, 'acknowledge', 'L2', '').problem).toBeUndefined();
  });
});

describe('the controller', () => {
  it('opens the Answer dialog and reads the ask into it', async () => {
    const { api, actions } = rig();
    await actions.open('answer', 'A1');
    expect(api.ask).toHaveBeenCalledWith('A1');
    expect(actions.state.open).toEqual({ kind: 'answer', target: 'A1' });
    expect(actions.state.ask).toBe(ASK);
    expect(actions.state.error).toBeUndefined();
  });

  it('opens the other dialogs without reading anything', async () => {
    const { api, actions } = rig();
    await actions.open('close', 'aa-1');
    expect(api.ask).not.toHaveBeenCalled();
    expect(actions.state.open).toEqual({ kind: 'close', target: 'aa-1' });
  });

  it('shows a failure to read the ask inside the dialog', async () => {
    const { actions } = rig({ ask: vi.fn(async () => Promise.reject(new Error('Ask A1 was not found.'))) });
    await actions.open('answer', 'A1');
    expect(actions.state.error).toBe('Ask A1 was not found.');
    expect(actions.state.ask).toBeUndefined();
  });

  it('drops an ask that lands after another dialog opened', async () => {
    let release: (a: Ask) => void = () => undefined;
    const { actions } = rig({ ask: vi.fn(() => new Promise<Ask>((r) => (release = r))) });
    const opening = actions.open('answer', 'A1');
    await actions.open('close', 'aa-1');
    release(ASK);
    await opening;
    expect(actions.state.open).toEqual({ kind: 'close', target: 'aa-1' });
    expect(actions.state.ask).toBeUndefined();
  });

  it('drops a failed read after another dialog opened', async () => {
    let fail: (e: Error) => void = () => undefined;
    const { actions } = rig({ ask: vi.fn(() => new Promise<Ask>((_, r) => (fail = r))) });
    const opening = actions.open('answer', 'A1');
    await actions.open('close', 'aa-1');
    fail(new Error('late'));
    await opening;
    expect(actions.state.error).toBeUndefined();
  });

  it('cancels with the refusal it showed', async () => {
    const { actions } = rig();
    await actions.open('close', 'aa-1');
    await actions.run('close', 'aa-1', '');
    expect(actions.state.error).toBe('Pick a resolution.');
    actions.cancel();
    expect(actions.state).toEqual({ open: undefined, ask: undefined, error: undefined, itemError: undefined });
  });

  it('closes the dialog and returns the toast on success', async () => {
    const { api, actions } = rig();
    await actions.open('answer', 'A1');
    expect(await actions.run('answer', 'A1', 'Left')).toBe('Answered A1');
    expect(api.answer).toHaveBeenCalledWith('A1', 'Left');
    expect(actions.state.open).toBeUndefined();
    expect(actions.state.ask).toBeUndefined();
  });

  it('keeps the dialog open with the API refusal', async () => {
    const { actions } = rig({ answer: vi.fn(async () => Promise.reject(new Error('Ask A1 is already answered.'))) });
    await actions.open('answer', 'A1');
    expect(await actions.run('answer', 'A1', 'Left')).toBeUndefined();
    expect(actions.state.open).toEqual({ kind: 'answer', target: 'A1' });
    expect(actions.state.error).toBe('Ask A1 is already answered.');
  });

  it('sends nothing for an input that is refused', async () => {
    const { api, actions } = rig();
    await actions.open('answer', 'A1');
    expect(await actions.run('answer', 'A1', '   ')).toBeUndefined();
    expect(api.answer).not.toHaveBeenCalled();
    expect(actions.state.error).toMatch(/Write an answer/);
  });

  it('puts the refusal of an action without a dialog on the item', async () => {
    const { actions } = rig({ acknowledge: vi.fn(async () => Promise.reject(new Error('Lease L2 has not expired.'))) });
    expect(await actions.run('acknowledge', 'L2')).toBeUndefined();
    expect(actions.state.itemError).toEqual({ target: 'L2', text: 'Lease L2 has not expired.' });
    expect(actions.state.error).toBeUndefined();
    expect(await rig().actions.run('acknowledge', 'L2')).toBe('Lease L2 acknowledged');
  });

  it('forgets an item refusal when the action works or a dialog opens', async () => {
    let fails = true;
    const { actions } = rig({ acknowledge: vi.fn(async () => (fails ? Promise.reject(new Error('no')) : {})) });
    await actions.run('acknowledge', 'L2');
    expect(actions.state.itemError).toBeDefined();
    fails = false;
    expect(await actions.run('acknowledge', 'L2')).toBe('Lease L2 acknowledged');
    expect(actions.state.itemError).toBeUndefined();
    fails = true;
    await actions.run('acknowledge', 'L2');
    await actions.open('check', 'aa-2');
    expect(actions.state.itemError).toBeUndefined();
  });

  it('publishes every change', async () => {
    const { actions, states } = rig();
    await actions.open('close', 'aa-1');
    expect(states.at(-1)).toBe(actions.state);
  });
});

describe('the real calls', () => {
  function browser(status = 200, body: unknown = {}, reject = false) {
    const fetch = vi.fn(async (_url: string, _init?: RequestInit) => {
      if (reject) throw new TypeError('offline');
      return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': status === 200 ? 'application/json' : 'application/problem+json' } });
    });
    return { fetch, assign: vi.fn(), pathname: '/', setHash: vi.fn() } as unknown as Browser & { fetch: typeof fetch };
  }

  it('reads an ask without a CSRF token or a body', async () => {
    const b = browser(200, ASK);
    expect(await new ActionsApi(b, () => 'tok').ask('A 1')).toEqual(ASK);
    const [url, init] = b.fetch.mock.calls[0]!;
    expect(url).toBe('/api/v1/asks/A%201');
    expect(init!.method).toBe('GET');
    expect(init!.headers).toEqual({ Accept: 'application/json' });
    expect(init!.body).toBeUndefined();
  });

  it('sends each write to its path with the token and the body', async () => {
    const b = browser();
    const api = new ActionsApi(b, () => 'tok');
    await api.answer('A1', 'Left');
    await api.withdraw('A1', 'why');
    await api.withdraw('A1', '');
    await api.close('aa-1', 'dropped');
    await api.acknowledge('L2');
    await api.check('aa-2', 'fine');
    const calls = b.fetch.mock.calls.map(([url, init]) => [init!.method, url, (init!.headers as Record<string, string>)['X-CSRF-Token'], init!.body]);
    expect(calls).toEqual([
      ['POST', '/api/v1/asks/A1/answer', 'tok', '{"answer":"Left"}'],
      ['POST', '/api/v1/asks/A1/withdraw', 'tok', '{"reason":"why"}'],
      ['POST', '/api/v1/asks/A1/withdraw', 'tok', '{}'],
      ['POST', '/api/v1/tasks/aa-1/close', 'tok', '{"resolution":"dropped"}'],
      ['POST', '/api/v1/leases/L2/ack', 'tok', undefined],
      ['POST', '/api/v1/tasks/aa-2/checks', 'tok', '{"summary":"fine"}'],
    ]);
  });

  it('turns a problem document into the error text', async () => {
    const b = browser(409, { title: 'Conflict', detail: 'Ask A1 is already answered.' });
    await expect(new ActionsApi(b, () => 'tok').answer('A1', 'x')).rejects.toThrow('Ask A1 is already answered.');
  });

  it('says when the server is not reachable', async () => {
    await expect(new ActionsApi(browser(200, {}, true), () => 'tok').acknowledge('L2')).rejects.toThrow('The server is not reachable.');
  });
});
