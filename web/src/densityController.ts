import './migrateOnLoad';
import { createDensityController } from './lib/density';
import { createSidebarController } from './lib/sidebar';

// The one density controller and the one sidebar controller of the page, wired to the real browser.
export const density = createDensityController({ storage: window.localStorage, root: document.documentElement });
export const sidebar = createSidebarController({ storage: window.localStorage });
