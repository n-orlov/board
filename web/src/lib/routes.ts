// The one route table. The hash router and every navigation surface (the phone tab bar, the
// tablet icon rail, the desktop sidebar) are derived from it, so adding a view is one entry here
// plus the component that renders it: neither the router nor the navigation names a view.
//
// The views are chosen by the URL fragment so a reload and a copied link land on the same view
// and the server needs no route for it.

export interface Route {
  /** Stable key: the shell maps it to the component that renders the view. */
  id: string;
  /** The URL fragment that selects the view. */
  hash: string;
  /** The view's title, shown in the phone top bar. */
  title: string;
  /** The navigation label. */
  label: string;
  /** SVG path data (24x24 viewBox, stroked) of the navigation icon. */
  icon: string;
  /**
   * A route with a parameter: `hash` is a prefix and the fragment is that prefix plus one segment, which is the
   * view's parameter (`#/i/<slug>`). A route without one is selected by its exact fragment.
   */
  param?: true;
  /** A route that is a page inside another view and has no navigation entry of its own. */
  parent?: string;
}

export const ROUTES = [
  {
    id: 'attention',
    hash: '#/attention',
    title: 'Attention',
    label: 'Attention',
    icon: 'M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9M13.7 21a2 2 0 0 1-3.4 0',
  },
  {
    id: 'initiatives',
    hash: '#/initiatives',
    title: 'Initiatives',
    label: 'Initiatives',
    icon: 'M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01',
  },
  {
    id: 'initiative',
    hash: '#/i/',
    param: true,
    parent: 'initiatives',
    title: 'Initiative',
    label: 'Initiative',
    icon: '',
  },
  {
    id: 'tasks',
    hash: '#/tasks',
    title: 'Tasks',
    label: 'Tasks',
    icon: 'M9 11l3 3 8-8M20 12v7a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h9',
  },
  {
    id: 'task',
    hash: '#/t/',
    param: true,
    parent: 'tasks',
    title: 'Task',
    label: 'Task',
    icon: '',
  },
  {
    id: 'keys',
    hash: '#/settings/keys',
    title: 'API keys',
    label: 'Settings',
    icon: 'M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6',
  },
] as const satisfies readonly Route[];

export type RouteId = (typeof ROUTES)[number]['id'];

/** The segment after a parameter route's prefix: one piece with no slash, the empty string when there is none. */
function segment(hash: string, prefix: string): string {
  const rest = hash.startsWith(prefix) ? hash.slice(prefix.length) : '';
  return rest.includes('/') ? '' : rest;
}

/** The fragment without its query: a view may keep state after the `?` (`#/tasks?status=done`) and is still that view. */
export function hashPath(hash: string): string {
  const at = hash.indexOf('?');
  return at < 0 ? hash : hash.slice(0, at);
}

function selects(route: Route, hash: string): boolean {
  const path = hashPath(hash);
  return route.param === true ? segment(path, route.hash) !== '' : route.hash === path;
}

/** The route a fragment names; anything the table does not name (`#/` included) is its first entry, the Attention view. */
export function resolveRoute<R extends Route>(hash: string, table: readonly R[]): R {
  return table.find((route) => selects(route, hash)) ?? table[0];
}

/** The parameter of the route the fragment selects (`#/i/phone-onboard` gives `phone-onboard`), else the empty string. */
export function routeParam(hash: string, table: readonly Route[]): string {
  const route = resolveRoute(hash, table);
  return route.param === true ? segment(hashPath(hash), route.hash) : '';
}

export interface NavEntry {
  id: string;
  href: string;
  label: string;
  icon: string;
  /** True for the entry of the view on screen: it gets `aria-current="page"`. */
  current: boolean;
}

/** One navigation entry per table entry that is not a page inside another view; the current one, or its parent, flagged. */
export function navEntries(current: Route, table: readonly Route[]): NavEntry[] {
  return table
    .filter((route) => route.parent === undefined)
    .map((route) => ({
      id: route.id,
      href: route.hash,
      label: route.label,
      icon: route.icon,
      current: route.id === current.id || route.id === current.parent,
    }));
}
