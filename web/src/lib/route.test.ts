import { describe, expect, it } from 'vitest';
import { KEYS_HASH, viewFromHash } from './route';

describe('viewFromHash', () => {
  it.each([
    [KEYS_HASH, 'keys'],
    ['#/settings/keys', 'keys'],
    ['', 'initiatives'],
    ['#', 'initiatives'],
    ['#/settings', 'initiatives'],
    ['#/settings/keys/', 'initiatives'],
  ])('maps %j to %s', (hash, want) => {
    expect(viewFromHash(hash)).toBe(want);
  });
});
