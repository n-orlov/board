// What the account menu says about the session (SPEC 11): the signed-in email and the operator
// handle the session acts as (`actor` of GET /auth/session), so the person can tell which operator
// the board will record their writes under.
import type { Session } from './session';

/** One labelled line of the account menu. */
export interface AccountRow {
  label: string;
  value: string;
  /** The data-testid of the value element. */
  testid: string;
}

/** The lines of the account menu: the email, then the operator handle. */
export function accountRows(session: Pick<Session, 'email' | 'actor'>): AccountRow[] {
  return [
    { label: 'Signed in as', value: session.email, testid: 'menu-email' },
    { label: 'Operator handle', value: session.actor, testid: 'menu-handle' },
  ];
}
