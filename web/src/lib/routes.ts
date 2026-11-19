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
  /** The count this entry's navigation link carries as a badge (the Needs me total); a view without one has none. */
  badge?: 'attention';
  /**
   * True for the entries that keep their own link in the phone tab bar; every other navigation entry is listed
   * under the tab bar's "More" entry. The tablet rail and the desktop sidebar list all of them.
   */
  phone?: true;
}

export const ROUTES = [
  {
    id: 'now',
    hash: '#/now',
    phone: true,
    title: 'Now',
    label: 'Now',
    badge: 'attention',
    icon: 'M22 12h-4l-3 9L9 3l-3 9H2',
  },
  {
    id: 'initiatives',
    hash: '#/initiatives',
    phone: true,
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
    phone: true,
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
    id: 'timeline',
    hash: '#/timeline',
    title: 'Timeline',
    label: 'Timeline',
    icon: 'M3 3v18h18M7 7h8M10 12h9M7 17h6',
  },
  {
    id: 'actors',
    hash: '#/actors',
    title: 'Actors',
    label: 'Actors',
    icon: 'M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8M23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75',
  },
  {
    id: 'archive',
    hash: '#/archive',
    title: 'Archive',
    label: 'Archive',
    icon: 'M21 8v13H3V8M1 3h22v5H1zM10 12h4',
  },
  {
    id: 'notes',
    hash: '#/notes',
    phone: true,
    title: 'Notes',
    label: 'Notes',
    icon: 'M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9zM14 3v6h6M8 13h8M8 17h5',
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

/** The route a fragment names; anything the table does not name is its first entry, the Now view. */
export function resolveRoute<R extends Route>(hash: string, table: readonly R[]): R {
  return table.find((route) => selects(route, hash)) ?? table[0];
}

/** The paths that land on Now: the root, and `#/attention`, the address of the view Now replaced. */
const NOW_LANDINGS: readonly string[] = ['', '#', '#/', '#/attention'];

/**
 * The fragment a page should rewrite itself to, or undefined when it is already where it belongs. The root (`#/`, or
 * none) and `#/attention` land on `#/now` and keep the query after the `?`, so a filter in an old link still applies.
 */
export function redirectHash(hash: string): string | undefined {
  if (!NOW_LANDINGS.includes(hashPath(hash))) return undefined;
  const at = hash.indexOf('?');
  return `#/now${at < 0 ? '' : hash.slice(at)}`;
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
  /** True for an entry that keeps its own link in the phone tab bar (the route's `phone`). */
  phone: boolean;
  /** The badge's visible text (`3`, `99+`), the empty string when there is none to show. */
  badge: string;
  /** The badge's words for assistive technology ("3 items need attention"), the empty string with no badge. */
  badgeLabel: string;
}

/** The most the badge prints before it says "more": a total above it shows as `99+`. */
export const BADGE_MAX = 99;

/** The badge of a count: nothing at 0, the number up to 99, `99+` above it. */
export function badgeText(total: number): string {
  if (!(total > 0)) return '';
  return total > BADGE_MAX ? `${BADGE_MAX}+` : String(total);
}

/** The accessible label of the badge: the whole count in words, even where the badge prints `99+`. */
export function badgeLabel(total: number): string {
  if (!(total > 0)) return '';
  return `${total} ${total === 1 ? 'item needs' : 'items need'} attention`;
}

/**
 * One navigation entry per table entry that is not a page inside another view; the current one, or its parent,
 * flagged. An entry whose route has a `badge` carries the badge of `attentionTotal`, the stream's `counts.total`.
 */
export function navEntries(current: Route, table: readonly Route[], attentionTotal = 0): NavEntry[] {
  return table
    .filter((route) => route.parent === undefined)
    .map((route) => ({
      id: route.id,
      href: route.hash,
      label: route.label,
      icon: route.icon,
      current: route.id === current.id || route.id === current.parent,
      phone: route.phone === true,
      badge: route.badge === 'attention' ? badgeText(attentionTotal) : '',
      badgeLabel: route.badge === 'attention' ? badgeLabel(attentionTotal) : '',
    }));
}

/** The phone tab bar's entries: `tabs` keep a link of their own, `more` is what the "More" entry lists, both in table order. */
export function phoneSplit(entries: readonly NavEntry[]): { tabs: NavEntry[]; more: NavEntry[] } {
  return { tabs: entries.filter((entry) => entry.phone), more: entries.filter((entry) => !entry.phone) };
}
