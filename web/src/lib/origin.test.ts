import { describe, expect, it } from 'vitest';
import { actorOrigin, keyOwnerText, viaOrigin } from './origin';

describe('viaOrigin', () => {
  it('says nothing when the origin is the actor', () => {
    expect(viaOrigin('alice', 'alice')).toBe('');
  });
  it('says "via X" when the origin differs from the actor', () => {
    expect(viaOrigin('builder', 'alice')).toBe('via alice');
  });
  it('says nothing when the origin is null, absent or empty', () => {
    expect(viaOrigin('builder', null)).toBe('');
    expect(viaOrigin('builder', undefined)).toBe('');
    expect(viaOrigin('builder', '')).toBe('');
  });
});

describe('actorOrigin', () => {
  it('names the origin of an agent', () => {
    expect(actorOrigin('agent', 'alice')).toBe('alice');
  });
  it('has nothing for an agent without one or for an operator', () => {
    expect(actorOrigin('agent', undefined)).toBeUndefined();
    expect(actorOrigin('agent', null)).toBeUndefined();
    expect(actorOrigin('operator', 'alice')).toBeUndefined();
  });
});

describe('keyOwnerText', () => {
  it('names the owner, or says no owner', () => {
    expect(keyOwnerText('alice')).toBe('alice');
    expect(keyOwnerText(undefined)).toBe('no owner');
    expect(keyOwnerText(null)).toBe('no owner');
  });
});
