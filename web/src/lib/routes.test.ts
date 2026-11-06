import { describe, expect, it } from 'vitest';
import { ROUTES, navEntries, resolveRoute, type Route } from './routes';

// A table the code has never seen: whatever it holds must come out of the router and the nav,
// so a view hard-coded in either one makes these tests fail.
const synthetic: Route[] = [
  { id: 'alpha', hash: '#/', title: 'Alpha title', label: 'Alpha', icon: 'M0 0' },
  { id: 'beta', hash: '#/b/c', title: 'Beta title', label: 'Beta nav', icon: 'M1 1' },
  { id: 'gamma', hash: '#/g', title: 'Gamma title', label: 'Gamma', icon: 'M2 2' },
];

describe('resolveRoute', () => {
  it('yields the routed view of each table entry, whatever the table holds', () => {
    for (const route of synthetic) expect(resolveRoute(route.hash, synthetic)).toBe(route);
  });

  it.each([[''], ['#'], ['#/nope'], ['#/b/c/'], ['#/b'], ['#/B/C']])('sends %j to the first entry', (hash) => {
    expect(resolveRoute(hash, synthetic)).toBe(synthetic[0]);
  });

  it.each([
    ['#/settings/keys', 'keys'],
    ['#/', 'initiatives'],
    ['', 'initiatives'],
    ['#/settings', 'initiatives'],
    ['#/settings/keys/', 'initiatives'],
  ])('maps the app table %j to %s', (hash, want) => {
    expect(resolveRoute(hash, ROUTES).id).toBe(want);
  });
});

describe('navEntries', () => {
  it('yields a nav entry for each table entry, in table order, carrying its link, label and icon', () => {
    const entries = navEntries(synthetic[0], synthetic);
    expect(entries.map((e) => [e.id, e.href, e.label, e.icon])).toEqual([
      ['alpha', '#/', 'Alpha', 'M0 0'],
      ['beta', '#/b/c', 'Beta nav', 'M1 1'],
      ['gamma', '#/g', 'Gamma', 'M2 2'],
    ]);
  });

  it('flags exactly the current route', () => {
    const route = resolveRoute('#/b/c', synthetic);
    expect(navEntries(route, synthetic).map((e) => e.current)).toEqual([false, true, false]);
  });

  it('flags nothing when the current route is not in the table', () => {
    const stranger: Route = { id: 'zeta', hash: '#/z', title: 'Z', label: 'Z', icon: '' };
    expect(navEntries(stranger, synthetic).some((e) => e.current)).toBe(false);
  });

  it('yields no entries for an empty table', () => {
    expect(navEntries(synthetic[0], [])).toEqual([]);
  });
});

describe('ROUTES', () => {
  it('has distinct ids and hashes, each hash a fragment route, each entry labelled', () => {
    expect(new Set(ROUTES.map((r) => r.id)).size).toBe(ROUTES.length);
    expect(new Set(ROUTES.map((r) => r.hash)).size).toBe(ROUTES.length);
    for (const route of ROUTES) {
      expect(route.hash.startsWith('#/')).toBe(true);
      expect(route.label).not.toBe('');
      expect(route.title).not.toBe('');
      expect(route.icon).not.toBe('');
    }
  });

  it('starts with the Initiatives view, the default', () => {
    expect(ROUTES[0].id).toBe('initiatives');
    expect(ROUTES[0].hash).toBe('#/');
  });
});
