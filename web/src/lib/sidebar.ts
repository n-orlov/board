// The sidebar-collapsed preference. From 1024 px the labelled sidebar can collapse to the icon rail the 768-1023 px
// layout already uses; the choice is kept in this browser. A stored value this module does not know means expanded.

/** The versioned storage key of the preference. */
export const SIDEBAR_STORAGE_KEY = 'vh.sidebar.v1';

/** A stored value as "collapsed?": only the exact word `collapsed` collapses; anything else (or absent) is expanded. */
export function parseCollapsed(raw: string | null | undefined): boolean {
  return raw === 'collapsed';
}

/** What the controller needs from the browser: tests pass fakes. */
export interface SidebarEnv {
  storage: Pick<Storage, 'getItem' | 'setItem'>;
}

export interface SidebarController {
  collapsed: () => boolean;
  /** Collapses or expands the sidebar and persists the choice. */
  set: (collapsed: boolean) => void;
}

/** Reads the stored preference and persists each later choice. */
export function createSidebarController(env: SidebarEnv): SidebarController {
  let current = false;
  try {
    current = parseCollapsed(env.storage.getItem(SIDEBAR_STORAGE_KEY));
  } catch {
    // Storage can be blocked: the sidebar then starts expanded.
  }
  return {
    collapsed: () => current,
    set(collapsed) {
      current = collapsed;
      try {
        env.storage.setItem(SIDEBAR_STORAGE_KEY, collapsed ? 'collapsed' : 'expanded');
      } catch {
        // The choice still applies to this page view.
      }
    },
  };
}
