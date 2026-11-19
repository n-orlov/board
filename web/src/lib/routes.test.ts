import { describe, expect, it } from 'vitest';
import { BADGE_MAX, ROUTES, badgeLabel, badgeText, liveBadgeLabel, hashPath, navEntries, phoneSplit, redirectHash, resolveRoute, routeParam, type Route } from './routes';

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
    ['#/', 'now'],
    ['', 'now'],
    ['#/now', 'now'],
    ['#/now?tag=work&initiative=aa', 'now'],
    ['#/initiatives', 'initiatives'],
    ['#/settings', 'now'],
    ['#/settings/keys/', 'now'],
    ['#/tasks', 'tasks'],
    ['#/timeline', 'timeline'],
    ['#/timeline?initiative=aa&closed=30', 'timeline'],
    ['#/actors', 'actors'],
    ['#/actors/', 'now'],
    ['#/archive', 'archive'],
    ['#/archive?window=7', 'archive'],
    ['#/archive/', 'now'],
    ['#/tasks?status=done&kind=watch', 'tasks'],
    ['#/tasks/', 'now'],
    ['#/i/phone-onboard?x=1', 'initiative'],
    ['#/t/aa-1', 'task'],
    ['#/t/', 'now'],
    ['#/t/aa-1/x', 'now'],
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
      ['now', false],
      ['initiatives', true],
      ['tasks', false],
      ['timeline', false],
      ['actors', false],
      ['archive', false],
      ['notes', false],
      ['keys', false],
    ]);
  });

  it('routes #/t/<ref> to the task page, which hangs under Tasks', () => {
    const route = resolveRoute('#/t/aa-1', ROUTES);
    expect(route.id).toBe('task');
    expect(routeParam('#/t/aa-1', ROUTES)).toBe('aa-1');
    expect(routeParam('#/t/', ROUTES)).toBe('');
    expect(navEntries(route, ROUTES).map((e) => [e.id, e.current])).toEqual([
      ['now', false],
      ['initiatives', false],
      ['tasks', true],
      ['timeline', false],
      ['actors', false],
      ['archive', false],
      ['notes', false],
      ['keys', false],
    ]);
  });

  it('puts Tasks between Initiatives and Timeline in the navigation, current on its filtered fragment', () => {
    const route = resolveRoute('#/tasks?status=done', ROUTES);
    expect(navEntries(route, ROUTES).map((e) => [e.id, e.href, e.current])).toEqual([
      ['now', '#/now', false],
      ['initiatives', '#/initiatives', false],
      ['tasks', '#/tasks', true],
      ['timeline', '#/timeline', false],
      ['actors', '#/actors', false],
      ['archive', '#/archive', false],
      ['notes', '#/notes', false],
      ['keys', '#/settings/keys', false],
    ]);
  });

  it('routes #/notes, with or without its filter query, to the Notes view, current in the navigation', () => {
    for (const hash of ['#/notes', '#/notes?broadcast=only&author=ann']) {
      const route = resolveRoute(hash, ROUTES);
      expect(route.id).toBe('notes');
      expect(navEntries(route, ROUTES).find((e) => e.current)?.id).toBe('notes');
    }
  });

  it('starts with the Now view, the default: a fragment naming no entry lands on it', () => {
    expect(ROUTES[0].id).toBe('now');
    expect(ROUTES[0].hash).toBe('#/now');
    expect(ROUTES[0].label).toBe('Now');
    expect(resolveRoute('#/', ROUTES)).toBe(ROUTES[0]);
  });

  it('keeps the filter query on the Now fragment and marks it current', () => {
    const route = resolveRoute('#/now?tag=work', ROUTES);
    expect(navEntries(route, ROUTES).map((e) => [e.id, e.current])).toEqual([
      ['now', true],
      ['initiatives', false],
      ['tasks', false],
      ['timeline', false],
      ['actors', false],
      ['archive', false],
      ['notes', false],
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

describe('the navigation order', () => {
  it('lists Now, Initiatives, Tasks, Timeline, Actors, Archive, Notes, then Settings, Now first and the default', () => {
    const entries = navEntries(ROUTES[0], ROUTES);
    expect(entries.map((e) => e.id)).toEqual(['now', 'initiatives', 'tasks', 'timeline', 'actors', 'archive', 'notes', 'keys']);
    expect(entries.map((e) => e.label)).toEqual(['Now', 'Initiatives', 'Tasks', 'Timeline', 'Actors', 'Archive', 'Notes', 'Settings']);
    expect(resolveRoute('', ROUTES).id).toBe('now');
  });
});

describe('badgeText', () => {
  it.each([
    [0, ''],
    [-1, ''],
    [Number.NaN, ''],
    [1, '1'],
    [3, '3'],
    [99, '99'],
    [100, '99+'],
    [120, '99+'],
  ])('prints %d as %j', (total, want) => {
    expect(badgeText(total)).toBe(want);
  });

  it('caps at BADGE_MAX', () => {
    expect(BADGE_MAX).toBe(99);
  });
});

describe('badgeLabel', () => {
  it.each([
    [0, ''],
    [1, '1 item needs attention'],
    [3, '3 items need attention'],
    [120, '120 items need attention'],
  ])('words %d as %j, the whole count even where the badge says 99+', (total, want) => {
    expect(badgeLabel(total)).toBe(want);
  });
});

describe('the Needs me badge on the navigation entries', () => {
  it('rides on the Now entry only, from the total handed in', () => {
    for (const [total, text, label] of [
      [0, '', ''],
      [3, '3', '3 items need attention'],
      [120, '99+', '120 items need attention'],
    ] as const) {
      const entries = navEntries(ROUTES[0], ROUTES, total);
      expect(entries.map((e) => [e.id, e.badge, e.badgeLabel])).toEqual([
        ['now', text, label],
        ['initiatives', '', ''],
        ['tasks', '', ''],
        ['timeline', '', ''],
        ['actors', '', ''],
        ['archive', '', ''],
        ['notes', '', ''],
        ['keys', '', ''],
      ]);
    }
  });

  it('has no badge anywhere without a total', () => {
    expect(navEntries(ROUTES[0], ROUTES).every((e) => e.badge === '' && e.badgeLabel === '')).toBe(true);
  });

  it('puts the badge on whichever entry of the table names it', () => {
    const table: Route[] = [{ ...synthetic[0] }, { ...synthetic[1], badge: 'attention' }];
    expect(navEntries(table[0], table, 7).map((e) => e.badge)).toEqual(['', '7']);
  });
});

describe('liveBadgeLabel', () => {
  it.each([
    [0, ''],
    [-1, ''],
    [Number.NaN, ''],
    [1, '1 open task has a live lease'],
    [2, '2 open tasks have a live lease'],
    [120, '120 open tasks have a live lease'],
  ])('words %d as %j', (total, want) => {
    expect(liveBadgeLabel(total)).toBe(want);
  });
});

describe('the live-lease badge on the navigation entries', () => {
  it('rides on the Now entry only, from the open tasks with a live lease handed in', () => {
    for (const [live, text, label] of [
      [0, '', ''],
      [2, '2', '2 open tasks have a live lease'],
      [150, '99+', '150 open tasks have a live lease'],
    ] as const) {
      const entries = navEntries(ROUTES[0], ROUTES, 0, live);
      expect(entries.map((e) => [e.id, e.liveBadge, e.liveBadgeLabel])).toEqual(
        entries.map((e) => (e.id === 'now' ? [e.id, text, label] : [e.id, '', ''])),
      );
      expect(entries.filter((e) => e.liveBadge !== '').map((e) => e.id)).toEqual(text === '' ? [] : ['now']);
    }
  });

  it('hides each badge at zero on its own: red without green, green without red', () => {
    const red = navEntries(ROUTES[0], ROUTES, 4, 0)[0];
    expect([red.badge, red.liveBadge]).toEqual(['4', '']);
    const green = navEntries(ROUTES[0], ROUTES, 0, 5)[0];
    expect([green.badge, green.liveBadge]).toEqual(['', '5']);
    const both = navEntries(ROUTES[0], ROUTES, 4, 5)[0];
    expect([both.badge, both.liveBadge, both.badgeLabel, both.liveBadgeLabel]).toEqual([
      '4',
      '5',
      '4 items need attention',
      '5 open tasks have a live lease',
    ]);
    const none = navEntries(ROUTES[0], ROUTES, 0, 0)[0];
    expect([none.badge, none.badgeLabel, none.liveBadge, none.liveBadgeLabel]).toEqual(['', '', '', '']);
  });

  it('has no green badge without a count, and only on a route that names one', () => {
    expect(navEntries(ROUTES[0], ROUTES).every((e) => e.liveBadge === '' && e.liveBadgeLabel === '')).toBe(true);
    const table: Route[] = [{ ...synthetic[0] }, { ...synthetic[1], liveBadge: true }];
    expect(navEntries(table[0], table, 0, 7).map((e) => e.liveBadge)).toEqual(['', '7']);
  });
});

describe('the phone tab bar split', () => {
  it('keeps Now, Initiatives, Tasks and Notes as tabs and puts Timeline, Actors, Archive and Settings under More, in table order', () => {
    const { tabs, more } = phoneSplit(navEntries(resolveRoute('#/now', ROUTES), ROUTES));
    expect(tabs.map((e) => e.label)).toEqual(['Now', 'Initiatives', 'Tasks', 'Notes']);
    expect(more.map((e) => [e.label, e.href])).toEqual([
      ['Timeline', '#/timeline'],
      ['Actors', '#/actors'],
      ['Archive', '#/archive'],
      ['Settings', '#/settings/keys'],
    ]);
  });

  it('lists the route table in the order Now, Initiatives, Tasks, Timeline, Actors, Archive, Notes, Settings', () => {
    expect(navEntries(ROUTES[0], ROUTES).map((e) => e.label)).toEqual(['Now', 'Initiatives', 'Tasks', 'Timeline', 'Actors', 'Archive', 'Notes', 'Settings']);
  });

  it('is a function of the table: a table with no phone flag has no tabs, one that flags all has no More', () => {
    const none = navEntries(synthetic[0], synthetic);
    expect(phoneSplit(none)).toEqual({ tabs: [], more: none });
    const all = navEntries(synthetic[0], synthetic.map((r) => ({ ...r, phone: true as const })));
    expect(phoneSplit(all)).toEqual({ tabs: all, more: [] });
  });

  it('flags a view under More as current there: the Archive entry is current on #/archive, not Tasks', () => {
    const entries = navEntries(resolveRoute('#/archive', ROUTES), ROUTES);
    expect(entries.filter((e) => e.current).map((e) => e.id)).toEqual(['archive']);
    expect(phoneSplit(entries).more.some((e) => e.current)).toBe(true);
  });
});

describe('redirectHash', () => {
  it.each([
    ['', '#/now'],
    ['#', '#/now'],
    ['#/', '#/now'],
    ['#/attention', '#/now'],
    ['#/attention?tag=work&initiative=aa', '#/now?tag=work&initiative=aa'],
    ['#/?tag=work', '#/now?tag=work'],
    ['#/attention?', '#/now?'],
  ])('sends %j to %j, keeping the filter in the fragment', (hash, to) => {
    expect(redirectHash(hash)).toBe(to);
  });

  it.each([['#/now'], ['#/now?tag=work'], ['#/tasks'], ['#/nope'], ['#/attention/'], ['#/attentions'], ['#/i/x']])('leaves %j where it is', (hash) => {
    expect(redirectHash(hash)).toBeUndefined();
  });
});
