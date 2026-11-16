import './migrateOnLoad';
import { createThemeController } from './lib/theme';

// The one theme controller of the page, wired to the real browser.
const query = window.matchMedia('(prefers-color-scheme: dark)');

export const theme = createThemeController({
  storage: window.localStorage,
  systemDark: () => query.matches,
  onSystemChange: (callback) => query.addEventListener('change', callback),
  root: document.documentElement,
  meta: document.querySelector('meta[name="theme-color"]'),
});
