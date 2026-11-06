// The API base is derived from the page's own path, never from a hard-coded
// origin: the page may be served at the root or behind a path-prefixing reverse
// proxy, and either way the API sits under the same prefix, on the same origin.
// The result is always an absolute path ("/api", "/board/api"), never a URL with a
// scheme or host, and never protocol-relative ("//host/...").

/** The path prefix the app is mounted at: "" at the root, "/board" under /board/. */
export function mountPrefix(pathname: string): string {
  const segments = pathname.split('/').filter((s) => s !== '' && s !== '.' && s !== '..');
  if (segments.length > 0 && segments[segments.length - 1] === 'index.html') {
    segments.pop();
  }
  return segments.length === 0 ? '' : '/' + segments.join('/');
}

/** The API base for a page served at `pathname`. */
export function apiBase(pathname: string): string {
  return mountPrefix(pathname) + '/api';
}

/** The API base for the page currently open in the browser. */
export function currentApiBase(loc: { pathname: string } = window.location): string {
  return apiBase(loc.pathname);
}

/** `path` ("/initiatives" or "initiatives") joined onto the API base. */
export function apiUrl(base: string, path: string): string {
  return base + '/' + path.replace(/^\/+/, '');
}

/**
 * The text a problem document (RFC 9457) gives for a failed response: its `detail`,
 * else its `title`. Undefined when the body is not a problem document or names neither, so the
 * caller says what failed in its own words.
 */
export async function problemDetail(response: Response): Promise<string | undefined> {
  let problem: unknown;
  try {
    problem = await response.json();
  } catch {
    return undefined;
  }
  if (typeof problem !== 'object' || problem === null) return undefined;
  const { detail, title } = problem as { detail?: unknown; title?: unknown };
  for (const text of [detail, title]) {
    if (typeof text === 'string' && text.trim() !== '') return text;
  }
  return undefined;
}
