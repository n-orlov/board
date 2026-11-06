import { describe, expect, it } from 'vitest';
import { apiBase, apiUrl, currentApiBase, mountPrefix, problemDetail } from './apibase';

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

describe('problemDetail', () => {
  const doc = (body: string) =>
    new Response(body, { status: 503, headers: { 'Content-Type': 'application/problem+json' } });

  it('is the detail of a problem document', async () => {
    expect(await problemDetail(doc('{"detail":"The store is down.","title":"Service Unavailable"}'))).toBe(
      'The store is down.',
    );
  });

  it('falls back to the title when the detail is missing or blank', async () => {
    expect(await problemDetail(doc('{"title":"Service Unavailable"}'))).toBe('Service Unavailable');
    expect(await problemDetail(doc('{"detail":"  ","title":"Service Unavailable"}'))).toBe('Service Unavailable');
  });

  it('ignores a detail or title that is not a string', async () => {
    expect(await problemDetail(doc('{"detail":5,"title":"Gone"}'))).toBe('Gone');
    expect(await problemDetail(doc('{"detail":5,"title":["x"]}'))).toBeUndefined();
  });

  it.each([
    ['an empty body', ''],
    ['a body that is not JSON', '<html>bad gateway</html>'],
    ['a JSON value that is not an object', '"down"'],
    ['null', 'null'],
    ['a document with neither detail nor title', '{"status":503}'],
    ['a blank title', '{"title":""}'],
  ])('is undefined for %s', async (_name, body) => {
    expect(await problemDetail(doc(body))).toBeUndefined();
  });
});
