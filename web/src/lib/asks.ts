// The presentation of asks on the task page and the initiative page (PRD R10, SPEC 11): open asks first, then the
// answered and the withdrawn; each with its mode, whether it blocks, and for an answered one who answered and how.
import type { components } from './api/schema';

export type Ask = components['schemas']['Ask'];
export type AskState = Ask['state'];

/** The states in display order: open first, then answered, then withdrawn. */
export const ASK_STATE_ORDER: readonly AskState[] = ['open', 'answered', 'withdrawn'];

const STATE_LABELS: Record<AskState, string> = { open: 'Open', answered: 'Answered', withdrawn: 'Withdrawn' };

const MODE_LABELS: Record<Ask['mode'], string> = {
  stop: 'stop: the asker stops until answered',
  poll: 'poll: the asker polls for the answer',
};

const CHANNEL_LABELS: Record<NonNullable<Ask['channel']>, string> = {
  ui: 'in the UI',
  api: 'through the API',
  relay: 'relayed',
};

/** The label of a state. */
export function stateLabel(state: AskState): string {
  return STATE_LABELS[state];
}

/** What a mode means for the asker. */
export function modeLabel(mode: Ask['mode']): string {
  return MODE_LABELS[mode];
}

/** The label of a blocking ask, saying what it holds up; empty for a non-blocking ask. */
export function blockingLabel(ask: Pick<Ask, 'blocking' | 'state'>): string {
  if (!ask.blocking) return '';
  return ask.state === 'open' ? 'blocks its task' : 'blocked its task';
}

/** The asks with the open ones first, then the answered, then the withdrawn; within a state the order given is kept. */
export function orderAsks(asks: readonly Ask[]): Ask[] {
  const rank = (a: Ask): number => ASK_STATE_ORDER.indexOf(a.state);
  return asks.map((ask, i) => ({ ask, i })).sort((a, b) => rank(a.ask) - rank(b.ask) || a.i - b.i).map((e) => e.ask);
}

/** The open asks, in the order given (the initiative page shows only these). */
export function openAsks(asks: readonly Ask[]): Ask[] {
  return asks.filter((a) => a.state === 'open');
}

/** Who answered an ask and how: "answered by alice in the UI", "answered by ralph relayed (on behalf of bob)"; empty until answered. */
export function answeredLine(ask: Ask): string {
  if (ask.state !== 'answered') return '';
  const who = ask.answered_by === undefined ? '' : ` by ${ask.answered_by}`;
  const how = ask.channel === undefined ? '' : ` ${CHANNEL_LABELS[ask.channel]}`;
  const behalf = ask.on_behalf_of === undefined ? '' : ` (on behalf of ${ask.on_behalf_of})`;
  return `answered${who}${how}${behalf}`;
}

/** Why an ask was withdrawn: "withdrawn: the reason"; empty for an ask that was not withdrawn. */
export function withdrawnLine(ask: Ask): string {
  if (ask.state !== 'withdrawn') return '';
  const reason = (ask.withdraw_reason ?? '').trim();
  return reason === '' ? 'withdrawn' : `withdrawn: ${reason}`;
}

/** The one line that tells how an ask ended or stands: the answered line, the withdrawn line, or who is waiting. */
export function stateLine(ask: Ask): string {
  if (ask.state === 'answered') return answeredLine(ask);
  if (ask.state === 'withdrawn') return withdrawnLine(ask);
  return ask.mode === 'poll' ? (ask.asker_waiting ? 'asker waiting' : 'asker not waiting') : 'asker stopped';
}

/** Whether the Answer and Withdraw actions apply: only an open ask takes them. */
export function canAct(ask: Pick<Ask, 'state'>): boolean {
  return ask.state === 'open';
}

/** The reason a task is blocked by its open blocking asks: "blocked by A3: Which database?", one per ask; empty for none. */
export function blockedReasons(asks: readonly Ask[]): string[] {
  return asks.filter((a) => a.state === 'open' && a.blocking).map((a) => `blocked by ${a.ref}: ${a.question}`);
}

/** The most an answer may hold, and the most a withdrawal's reason may hold. */
export const ANSWER_LIMIT = 20000;
export const REASON_LIMIT = 200;

/** Why an answer cannot be sent: empty after trimming, or over the API's limit. Undefined when it can. */
export function answerProblem(text: string): string | undefined {
  const length = [...text.trim()].length;
  if (length === 0) return 'Write an answer or pick one of the options.';
  return length > ANSWER_LIMIT ? `An answer is at most ${ANSWER_LIMIT} characters; this one has ${length}.` : undefined;
}

/** Why a withdrawal's reason cannot be sent (it may be empty). */
export function reasonProblem(text: string): string | undefined {
  const length = [...text.trim()].length;
  return length > REASON_LIMIT ? `A reason is at most ${REASON_LIMIT} characters; this one has ${length}.` : undefined;
}
