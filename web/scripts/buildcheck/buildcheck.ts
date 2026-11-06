// Checks over the built UI: the design tokens are present for both themes, and the
// built page references nothing that is not same-origin and relative.

export const TOKENS = [
  '--surface',
  '--surface-raised',
  '--border',
  '--text',
  '--text-muted',
  '--accent',
  '--danger',
  '--success',
  '--warning',
  '--radius',
  '--space-1',
  '--space-2',
  '--space-3',
  '--space-4',
  '--space-5',
  '--space-6',
  '--space-7',
  '--space-8',
  '--font-sans',
  '--font-mono',
] as const;

export type Theme = 'light' | 'dark';

/** Custom properties declared by the leaf rules whose selector list names the theme. */
export function declaredTokens(css: string, theme: Theme): Set<string> {
  const names = new Set<string>();
  const themeSelector = new RegExp(`\\[data-theme=["']?${theme}["']?\\]`);
  for (const rule of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (!themeSelector.test(rule[1])) continue;
    for (const decl of rule[2].matchAll(/(--[\w-]+)\s*:\s*[^;}\s]/g)) names.add(decl[1]);
  }
  return names;
}

export function missingTokens(css: string, theme: Theme): string[] {
  const declared = declaredTokens(css, theme);
  return TOKENS.filter((t) => !declared.has(t));
}

/** True for http://, https://, any other scheme:, and protocol-relative //host URLs. */
export function isNonRelative(url: string): boolean {
  const u = url.trim();
  return u.startsWith('//') || u.startsWith('/') || /^[a-z][a-z0-9+.-]*:/i.test(u);
}

/** The src and href attribute values of an HTML document. */
export function htmlReferences(html: string): string[] {
  return [...html.matchAll(/\s(?:src|href)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))/gi)].map(
    (m) => m[1] ?? m[2] ?? m[3],
  );
}

/** The url(...) values of a stylesheet, without quotes. */
export function cssUrls(css: string): string[] {
  return [...css.matchAll(/url\(\s*(?:"([^"]*)"|'([^']*)'|([^)"'\s]*))\s*\)/gi)].map(
    (m) => m[1] ?? m[2] ?? m[3],
  );
}

/** Every src/href/url(...) value in the built page that is absolute or protocol-relative. */
export function nonRelativeReferences(html: string, css: string): string[] {
  return [...htmlReferences(html), ...cssUrls(css)].filter(isNonRelative);
}
