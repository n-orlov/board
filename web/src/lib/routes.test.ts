import { describe, expect, it } from 'vitest';
import { ROUTES, hashPath, navEntries, resolveRoute, routeParam, type Route } from './routes';

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
    ['#/', 'attention'],
    ['', 'attention'],
    ['#/attention', 'attention'],
    ['#/attention?tag=work&initiative=aa', 'attention'],
    ['#/initiatives', 'initiatives'],
    ['#/settings', 'attention'],
    ['#/settings/keys/', 'attention'],
    ['#/tasks', 'tasks'],
    ['#/tasks?status=done&kind=watch', 'tasks'],
    ['#/tasks/', 'attention'],
    ['#/i/phone-onboard?x=1', 'initiative'],
    ['#/t/aa-1', 'task'],
    ['#/t/', 'attention'],
    ['#/t/aa-1/x', 'attention'],
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

// A table with a parameter route and a page inside another view.
const withParam: Route[] = [
  ...synthetic,
  { id: 'item', hash: '#/x/', param: true, parent: 'beta', title: 'Item', label: 'Item', icon: '' },
];

describe('parameter routes', () => {
  it.each([
    ['#/x/abc', 'item', 'abc'],
    ['#/x/a-b-1', 'item', 'a-b-1'],
    ['#/b/c', 'beta', ''],
    ['#/', 'alpha', ''],
  ])('resolves %j to %s with the parameter %j', (hash, id, param) => {
    expect(resolveRoute(hash, withParam).id).toBe(id);
    expect(routeParam(hash, withParam)).toBe(param);
  });

  it.each([['#/x/'], ['#/x'], ['#/x/a/b'], ['#/x/a/'], ['x/abc'], ['']])('sends %j to the first entry with no parameter', (hash) => {
    expect(resolveRoute(hash, withParam).id).toBe('alpha');
    expect(routeParam(hash, withParam)).toBe('');
  });

  it('keeps a page inside another view out of the navigation and flags its parent as current', () => {
    const item = withParam[3];
    const entries = navEntries(item, withParam);
    expect(entries.map((e) => e.id)).toEqual(['alpha', 'beta', 'gamma']);
    expect(entries.map((e) => e.current)).toEqual([false, true, false]);
  });
});

describe('ROUTES', () => {
  it('has distinct ids and hashes, each hash a fragment route, each entry labelled', () => {
    expect(new Set(ROUTES.map((r) => r.id)).size).toBe(ROUTES.length);
    expect(new Set(ROUTES.map((r) => r.hash)).size).toBe(ROUTES.length);
    for (const route of ROUTES as readonly Route[]) {
      expect(route.hash.startsWith('#/')).toBe(true);
      expect(route.label).not.toBe('');
      expect(route.title).not.toBe('');
      if (route.parent === undefined) expect(route.icon).not.toBe('');
    }
  });

  it('routes #/i/<slug> to the initiative page, which hangs under Initiatives', () => {
    const route = resolveRoute('#/i/phone-onboard', ROUTES);
    expect(route.id).toBe('initiative');
    expect(routeParam('#/i/phone-onboard', ROUTES)).toBe('phone-onboard');
    expect(routeParam('#/i/phone-onboard?x=1', ROUTES)).toBe('phone-onboard');
    expect(navEntries(route, ROUTES).map((e) => [e.id, e.current])).toEqual([
      ['attention', false],
      ['initiatives', true],
      ['tasks', false],
      ['keys', false],
    ]);
  });

  it('routes #/t/<ref> to the task page, which hangs under Tasks', () => {
    const route = resolveRoute('#/t/aa-1', ROUTES);
    expect(route.id).toBe('task');
    expect(routeParam('#/t/aa-1', ROUTES)).toBe('aa-1');
    expect(routeParam('#/t/', ROUTES)).toBe('');
    expect(navEntries(route, ROUTES).map((e) => [e.id, e.current])).toEqual([
      ['attention', false],
      ['initiatives', false],
      ['tasks', true],
      ['keys', false],
    ]);
  });

  it('puts Tasks between Initiatives and Settings in the navigation, current on its filtered fragment', () => {
    const route = resolveRoute('#/tasks?status=done', ROUTES);
    expect(navEntries(route, ROUTES).map((e) => [e.id, e.href, e.current])).toEqual([
      ['attention', '#/attention', false],
      ['initiatives', '#/initiatives', false],
      ['tasks', '#/tasks', true],
      ['keys', '#/settings/keys', false],
    ]);
  });

  it('starts with the Attention view, the default: `#/` names no entry and so lands on it', () => {
    expect(ROUTES[0].id).toBe('attention');
    expect(ROUTES[0].hash).toBe('#/attention');
    expect(resolveRoute('#/', ROUTES)).toBe(ROUTES[0]);
  });

  it('keeps the filter query on the Attention fragment and marks it current', () => {
    const route = resolveRoute('#/attention?tag=work', ROUTES);
    expect(navEntries(route, ROUTES).map((e) => [e.id, e.current])).toEqual([
      ['attention', true],
      ['initiatives', false],
      ['tasks', false],
      ['keys', false],
    ]);
  });
});

describe('hashPath', () => {
  it('drops the query and keeps the rest', () => {
    expect(hashPath('#/tasks?status=done')).toBe('#/tasks');
    expect(hashPath('#/tasks')).toBe('#/tasks');
    expect(hashPath('#/tasks?a=1?b')).toBe('#/tasks');
    expect(hashPath('')).toBe('');
  });
});
