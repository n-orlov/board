// The theme setting (System, Light or Dark) and the effective theme it resolves to.
//
// The setting is the user's choice and is persisted; the effective theme is what the page shows.
// System follows the browser's `prefers-color-scheme`. A stored value this module does not know
// falls back to System. The same rule is repeated in the inline script of web/index.html, which
// runs before the app bundle so the first paint already has the right theme; theme.test.ts runs
// that script against this module's mapping so the two cannot drift apart.

export type ThemeSetting = 'system' | 'light' | 'dark';
export type EffectiveTheme = 'light' | 'dark';

export const THEME_SETTINGS: readonly ThemeSetting[] = ['system', 'light', 'dark'];
export const THEME_STORAGE_KEY = 'vh.theme';

/** The `--surface` token of each theme: the `theme-color` the browser chrome takes. */
export const THEME_COLORS: Readonly<Record<EffectiveTheme, string>> = {
  light: '#f6f7f9',
  dark: '#14161a',
};

/** A stored value as a setting: anything unknown (or absent) is System. */
export function parseSetting(raw: string | null | undefined): ThemeSetting {
  return raw === 'light' || raw === 'dark' ? raw : 'system';
}

/** The theme the page shows for `setting` when the system prefers dark (or not). */
export function effectiveTheme(setting: ThemeSetting, systemDark: boolean): EffectiveTheme {
  if (setting === 'system') return systemDark ? 'dark' : 'light';
  return setting;
}

/** The surface colour of an effective theme. */
export function themeColor(theme: EffectiveTheme): string {
  return THEME_COLORS[theme];
}

/** What the controller needs from the browser: tests pass fakes. */
export interface ThemeEnv {
  storage: Pick<Storage, 'getItem' | 'setItem'>;
  systemDark: () => boolean;
  /** Registers a callback for a change of the system preference. */
  onSystemChange: (callback: () => void) => void;
  root: { setAttribute: (name: string, value: string) => void };
  meta: { setAttribute: (name: string, value: string) => void } | null;
}

export interface ThemeController {
  setting: () => ThemeSetting;
  effective: () => EffectiveTheme;
  /** Chooses and persists a setting, then applies it. */
  set: (setting: ThemeSetting) => void;
}

/** Reads the stored setting, applies it, and re-applies it when the system preference changes. */
export function createThemeController(env: ThemeEnv): ThemeController {
  let current: ThemeSetting = 'system';
  try {
    current = parseSetting(env.storage.getItem(THEME_STORAGE_KEY));
  } catch {
    // Storage can be blocked: the page then runs on System.
  }
  const effective = () => effectiveTheme(current, env.systemDark());
  const apply = () => {
    const theme = effective();
    env.root.setAttribute('data-theme', theme);
    env.meta?.setAttribute('content', themeColor(theme));
  };
  env.onSystemChange(apply);
  apply();
  return {
    setting: () => current,
    effective,
    set(setting) {
      current = setting;
      try {
        env.storage.setItem(THEME_STORAGE_KEY, setting);
      } catch {
        // The choice still applies to this page view.
      }
      apply();
    },
  };
}
