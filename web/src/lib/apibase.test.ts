import { describe, expect, it } from 'vitest';
import { apiBase, apiUrl, currentApiBase, mountPrefix } from './apibase';

// An absolute origin is a scheme, or a protocol-relative "//host".
const absoluteOrigin = /^([a-z][a-z0-9+.-]*:)?\/\//i;

describe('apiBase', () => {
  it.each([
    ['/', '/api'],
    ['', '/api'],
    ['/index.html', '/api'],
    ['/board/', '/board/api'],
    ['/board', '/board/api'],
    ['/board/index.html', '/board/api'],
    ['/a/b/', '/a/b/api'],
  ])('derives the base for %j from the path alone: %j', (pathname, want) => {
    expect(apiBase(pathname)).toBe(want);
  });

  it.each(['/', '/board/', '//evil.example/', '///evil.example/x', '/http://evil.example/', '/../x/', '/./'])(
    'never returns an absolute origin for %j',
    (pathname) => {
      const got = apiBase(pathname);
      expect(got).not.toMatch(absoluteOrigin);
      expect(got.startsWith('/')).toBe(true);
      expect(got.startsWith('//')).toBe(false);
      expect(got).not.toContain('://');
    },
  );

  it('does not depend on the origin of the page', () => {
    const a = currentApiBase(new URL('http://one.example:8080/board/'));
    const b = currentApiBase(new URL('https://two.example/board/'));
    expect(a).toBe('/board/api');
    expect(b).toBe(a);
  });
});

describe('mountPrefix', () => {
  it('is empty at the root', () => {
    expect(mountPrefix('/')).toBe('');
  });

  it('drops dot segments', () => {
    expect(mountPrefix('/a/../b/./')).toBe('/a/b');
  });
});

describe('apiUrl', () => {
  it('joins with exactly one slash', () => {
    expect(apiUrl('/api', 'initiatives')).toBe('/api/initiatives');
    expect(apiUrl('/api', '/initiatives')).toBe('/api/initiatives');
    expect(apiUrl('/board/api', '//x')).toBe('/board/api/x');
  });
});
