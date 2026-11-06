import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import {
  createThemeController,
  effectiveTheme,
  parseSetting,
  THEME_COLORS,
  THEME_STORAGE_KEY,
  themeColor,
  type EffectiveTheme,
  type ThemeEnv,
  type ThemeSetting,
} from './theme.ts';

// The 3 settings x 2 system preferences.
const cases: [ThemeSetting, boolean, EffectiveTheme][] = [
  ['system', false, 'light'],
  ['system', true, 'dark'],
  ['light', false, 'light'],
  ['light', true, 'light'],
  ['dark', false, 'dark'],
  ['dark', true, 'dark'],
];

describe('effectiveTheme', () => {
  for (const [setting, systemDark, want] of cases) {
    it(`${setting} with a ${systemDark ? 'dark' : 'light'} system gives ${want}`, () => {
      expect(effectiveTheme(setting, systemDark)).toBe(want);
    });
  }
});

describe('parseSetting', () => {
  it('knows the three settings', () => {
    expect(parseSetting('system')).toBe('system');
    expect(parseSetting('light')).toBe('light');
    expect(parseSetting('dark')).toBe('dark');
  });
  it('falls back to System on an unknown, empty or absent value', () => {
    for (const raw of ['solarized', '', 'Dark', ' light', null, undefined]) expect(parseSetting(raw)).toBe('system');
  });
});

describe('themeColor', () => {
  it('is the surface token of each theme in styles.css', () => {
    const css = readFileSync(new URL('../styles.css', import.meta.url), 'utf8');
    const surface = (selector: string) => new RegExp(`${selector}\\s*\\{[^}]*--surface:\\s*(#[0-9a-f]{6});`, 'i').exec(css)?.[1];
    expect(themeColor('light')).toBe(surface("\\[data-theme='light'\\]"));
    expect(themeColor('dark')).toBe(surface("\\[data-theme='dark'\\]"));
    expect(themeColor('light')).not.toBe(themeColor('dark'));
  });
});

interface Fake {
  env: ThemeEnv;
  store: Map<string, string>;
  attrs: Record<string, string>;
  metaAttrs: Record<string, string>;
  setDark: (dark: boolean) => void;
}

function fake(stored?: string, opts: { dark?: boolean; noMeta?: boolean; blocked?: boolean } = {}): Fake {
  const store = new Map<string, string>();
  if (stored !== undefined) store.set(THEME_STORAGE_KEY, stored);
  const attrs: Record<string, string> = {};
  const metaAttrs: Record<string, string> = {};
  let dark = opts.dark ?? false;
  const listeners: (() => void)[] = [];
  const env: ThemeEnv = {
    storage: opts.blocked
      ? {
          getItem: () => {
            throw new Error('blocked');
          },
          setItem: () => {
            throw new Error('blocked');
          },
        }
      : { getItem: (k) => store.get(k) ?? null, setItem: (k, v) => void store.set(k, v) },
    systemDark: () => dark,
    onSystemChange: (cb) => void listeners.push(cb),
    root: { setAttribute: (n, v) => void (attrs[n] = v) },
    meta: opts.noMeta ? null : { setAttribute: (n, v) => void (metaAttrs[n] = v) },
  };
  return {
    env,
    store,
    attrs,
    metaAttrs,
    setDark(next) {
      dark = next;
      listeners.forEach((l) => l());
    },
  };
}

describe('createThemeController', () => {
  it('starts on System when nothing is stored and applies the effective theme and colour', () => {
    const f = fake(undefined, { dark: true });
    const c = createThemeController(f.env);
    expect(c.setting()).toBe('system');
    expect(c.effective()).toBe('dark');
    expect(f.attrs['data-theme']).toBe('dark');
    expect(f.metaAttrs['content']).toBe(THEME_COLORS.dark);
  });

  it('restores a persisted setting', () => {
    const f = fake('light', { dark: true });
    const c = createThemeController(f.env);
    expect(c.setting()).toBe('light');
    expect(f.attrs['data-theme']).toBe('light');
    expect(f.metaAttrs['content']).toBe(THEME_COLORS.light);
  });

  it('falls back to System on an unknown stored value', () => {
    const f = fake('neon', { dark: true });
    const c = createThemeController(f.env);
    expect(c.setting()).toBe('system');
    expect(f.attrs['data-theme']).toBe('dark');
  });

  it('persists a chosen setting under the storage key and applies it', () => {
    const f = fake(undefined, { dark: true });
    const c = createThemeController(f.env);
    c.set('light');
    expect(f.store.get(THEME_STORAGE_KEY)).toBe('light');
    expect(c.setting()).toBe('light');
    expect(f.attrs['data-theme']).toBe('light');
    expect(f.metaAttrs['content']).toBe(THEME_COLORS.light);
    c.set('system');
    expect(f.store.get(THEME_STORAGE_KEY)).toBe('system');
    expect(f.attrs['data-theme']).toBe('dark');
    expect(f.metaAttrs['content']).toBe(THEME_COLORS.dark);
  });

  it('a fresh controller over the same storage sees the persisted choice', () => {
    const f = fake(undefined);
    createThemeController(f.env).set('dark');
    expect(createThemeController(f.env).setting()).toBe('dark');
  });

  it('follows a change of the system preference on System, not on an explicit setting', () => {
    const f = fake(undefined, { dark: false });
    const c = createThemeController(f.env);
    expect(f.attrs['data-theme']).toBe('light');
    f.setDark(true);
    expect(f.attrs['data-theme']).toBe('dark');
    expect(f.metaAttrs['content']).toBe(THEME_COLORS.dark);
    c.set('light');
    f.setDark(false);
    f.setDark(true);
    expect(f.attrs['data-theme']).toBe('light');
  });

  it('works without a theme-color element', () => {
    const f = fake(undefined, { dark: true, noMeta: true });
    createThemeController(f.env).set('light');
    expect(f.attrs['data-theme']).toBe('light');
    expect(f.metaAttrs).toEqual({});
  });

  it('runs on System, and still applies a choice, when storage is blocked', () => {
    const f = fake(undefined, { dark: true, blocked: true });
    const c = createThemeController(f.env);
    expect(c.setting()).toBe('system');
    c.set('light');
    expect(c.setting()).toBe('light');
    expect(f.attrs['data-theme']).toBe('light');
  });
});

// The inline script in index.html is the same rule, run before the bundle: run it for every case.
describe('the pre-paint script in index.html', () => {
  const html = readFileSync(new URL('../../index.html', import.meta.url), 'utf8');
  const head = html.slice(0, html.indexOf('</head>'));
  const script = /<script>([\s\S]*?)<\/script>/.exec(head)?.[1] ?? '';

  function run(stored: string | null, systemDark: boolean, blocked = false) {
    const attrs: Record<string, string> = {};
    const meta: Record<string, string> = {};
    const selectors: string[] = [];
    const doc = {
      documentElement: { setAttribute: (n: string, v: string) => void (attrs[n] = v) },
      querySelector: (s: string) => {
        selectors.push(s);
        return { setAttribute: (n: string, v: string) => void (meta[n] = v) };
      },
    };
    const storage = {
      getItem: (k: string) => {
        if (blocked) throw new Error('blocked');
        return k === THEME_STORAGE_KEY ? stored : null;
      },
    };
    const mm = (q: string) => ({ matches: q === '(prefers-color-scheme: dark)' && systemDark });
    new Function('localStorage', 'matchMedia', 'document', script)(storage, mm, doc);
    return { attrs, meta, selectors };
  }

  it('is in the head, before the app bundle', () => {
    expect(script).not.toBe('');
    expect(head).toContain('<meta name="theme-color"');
    expect(html.indexOf('<script>')).toBeLessThan(html.indexOf('src="/src/main.ts"'));
  });

  for (const [setting, systemDark, want] of cases) {
    it(`${setting} with a ${systemDark ? 'dark' : 'light'} system sets ${want} and its surface colour`, () => {
      const r = run(setting, systemDark);
      expect(r.attrs['data-theme']).toBe(want);
      expect(r.meta['content']).toBe(themeColor(want));
      expect(r.selectors).toEqual(['meta[name="theme-color"]']);
    });
  }

  it('treats nothing stored, an unknown value and blocked storage as System', () => {
    for (const stored of [null, 'neon', '']) {
      expect(run(stored, true).attrs['data-theme']).toBe('dark');
      expect(run(stored, false).attrs['data-theme']).toBe('light');
    }
    expect(run(null, true, true).attrs['data-theme']).toBe('dark');
  });

  it('ships a theme-color for the default (light) theme', () => {
    expect(html).toContain(`<meta name="theme-color" content="${THEME_COLORS.light}" />`);
  });
});
