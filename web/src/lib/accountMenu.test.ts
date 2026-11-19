import { describe, expect, it } from 'vitest';
import { accountRows } from './accountMenu';

describe('accountRows', () => {
  it('names the email and the operator handle of the session', () => {
    const rows = accountRows({ email: 'ann@example.test', actor: 'ann' });
    expect(rows.map((r) => r.value)).toEqual(['ann@example.test', 'ann']);
    expect(rows.find((r) => r.testid === 'menu-handle')).toMatchObject({ label: 'Operator handle', value: 'ann' });
  });

  it('carries the handle of whichever operator signed in, not a fixed one', () => {
    const handles = ['ann', 'bob'].map((actor) => accountRows({ email: `${actor}@example.test`, actor })[1].value);
    expect(handles).toEqual(['ann', 'bob']);
  });

  it('does not show the handle in place of the email', () => {
    const [email] = accountRows({ email: 'ann@example.test', actor: 'ann' });
    expect(email).toMatchObject({ testid: 'menu-email', value: 'ann@example.test' });
  });
});
