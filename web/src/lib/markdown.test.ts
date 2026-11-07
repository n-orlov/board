import { describe, expect, it } from 'vitest';
import { escapeHtml, isSafeUrl, renderInline, renderMarkdown } from './markdown';

describe('escapeHtml', () => {
  it('escapes the five significant characters', () => {
    expect(escapeHtml(`<a href="x" onclick='y'>&</a>`)).toBe('&lt;a href=&quot;x&quot; onclick=&#39;y&#39;&gt;&amp;&lt;/a&gt;');
  });
});

describe('raw HTML', () => {
  it('is escaped, never passed through', () => {
    const html = renderMarkdown('<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>');
    expect(html).toBe('<p>&lt;script&gt;alert(1)&lt;/script&gt;</p><p>&lt;img src=x onerror=alert(1)&gt;</p>');
    expect(html).not.toContain('<script');
    expect(html).not.toContain('<img');
  });
  it('is escaped inside emphasis, code, headings, lists, quotes and link text', () => {
    const html = renderMarkdown('# <b>h</b>\n\n- <i>x</i>\n\n> <u>q</u>\n\n**<s>b</s>** `<k>` [<em>l</em>](https://e.com)\n\n```\n<div>\n```');
    for (const tag of ['<b>', '<i>', '<u>', '<s>', '<k>', '<em>l', '<div>']) expect(html).not.toContain(tag);
    expect(html).toContain('&lt;div&gt;');
    expect(html).toContain('<code>&lt;k&gt;</code>');
  });
  it('cannot break out of an href attribute', () => {
    const html = renderInline('[x](https://e.com/"onmouseover="alert(1))');
    expect(html).toContain('href="https://e.com/&quot;onmouseover=&quot;alert(1"');
    expect(html).not.toContain('" onmouseover');
  });
});

describe('links', () => {
  it('get rel noopener noreferrer', () => {
    expect(renderInline('[docs](https://example.com/a?b=1&c=2)')).toBe(
      '<a href="https://example.com/a?b=1&amp;c=2" rel="noopener noreferrer">docs</a>',
    );
  });
  it('keep http, mailto and relative targets', () => {
    for (const url of ['http://e.com', 'MAILTO:a@b.c', '/i/aa', '#/i/aa', 'page.html', '//e.com/x']) {
      expect(renderInline(`[l](${url})`)).toContain('rel="noopener noreferrer"');
    }
  });
  it('drop a javascript: link to its text', () => {
    expect(renderInline('[click](java\u007fscript:alert)')).toBe('click');
    expect(renderInline('[click](java\u0085script:alert)')).toBe('click');
    expect(renderInline('[click](javascript:alert)')).toBe('click');
    expect(renderInline('[click](JavaScript:alert)')).toBe('click');
    expect(renderInline('[click](java\u0001script:alert)')).toBe('click');
    expect(renderInline('[click](  javascript:alert)')).not.toContain('<a');
  });
  it('leave only text, never a link, when a javascript: target holds a parenthesis', () => {
    const html = renderInline('[click](javascript:alert(1))');
    expect(html).toBe('click)');
  });
  it('drop data: and vbscript: links, keeping the escaped link text', () => {
    expect(renderInline('[<b>x</b>](data:text/html,hi)')).toBe('&lt;b&gt;x&lt;/b&gt;');
    expect(renderInline('[x](vbscript:run)')).not.toContain('<a');
  });
  it('treats an entity-encoded scheme as literal text', () => {
    const html = renderInline('[x](&#106;avascript:alert)');
    expect(html).toContain('href="&amp;#106;avascript:alert"');
  });
  it('decides safety by scheme alone', () => {
    expect(isSafeUrl('https://a.b')).toBe(true);
    expect(isSafeUrl('foo/bar:baz')).toBe(true);
    expect(isSafeUrl('javascript:x')).toBe(false);
    expect(isSafeUrl('ftp://a.b')).toBe(false);
  });
});

describe('inline markup', () => {
  it('renders code, bold and italic', () => {
    expect(renderInline('a `c<>` **b** *i* z')).toBe('a <code>c&lt;&gt;</code> <strong>b</strong> <em>i</em> z');
  });
  it('nests emphasis in a link and leaves code untouched', () => {
    expect(renderInline('[**b**](/x)')).toBe('<a href="/x" rel="noopener noreferrer"><strong>b</strong></a>');
    expect(renderInline('`**no**`')).toBe('<code>**no**</code>');
  });
  it('leaves an unmatched marker as text', () => {
    expect(renderInline('2 * 3 and [x] (y)')).toBe('2 * 3 and [x] (y)');
  });
});

describe('blocks', () => {
  it('gives the empty string for blank input', () => {
    expect(renderMarkdown('')).toBe('');
    expect(renderMarkdown('  \n\n \r\n')).toBe('');
  });
  it('joins the lines of a paragraph and splits paragraphs at a blank line', () => {
    expect(renderMarkdown('one\ntwo\n\nthree')).toBe('<p>one<br>two</p><p>three</p>');
  });
  it('renders headings two levels down, capped at h6', () => {
    expect(renderMarkdown('# A\n## B #\n###### F')).toBe('<h3>A</h3><h4>B</h4><h6>F</h6>');
  });
  it('renders bullet and numbered lists', () => {
    expect(renderMarkdown('- a\n* b\n\n1. c\n2) d')).toBe('<ul><li>a</li><li>b</li></ul><ol><li>c</li><li>d</li></ol>');
  });
  it('renders a block quote', () => {
    expect(renderMarkdown('> a\n> b')).toBe('<blockquote><p>a<br>b</p></blockquote>');
  });
  it('renders fenced code verbatim and closes at the end of input when unterminated', () => {
    expect(renderMarkdown('```go\nx := "<1>"\n\n  y\n```\nafter')).toBe('<pre><code>x := &quot;&lt;1&gt;&quot;\n\n  y</code></pre><p>after</p>');
    expect(renderMarkdown('```\nopen')).toBe('<pre><code>open</code></pre>');
  });
  it('ends a paragraph where another block starts', () => {
    expect(renderMarkdown('text\n- item\n# head\n```\nc\n```')).toBe('<p>text</p><ul><li>item</li></ul><h3>head</h3><pre><code>c</code></pre>');
  });
  it('accepts CRLF and CR line ends', () => {
    expect(renderMarkdown('a\r\n\r\nb\rc')).toBe('<p>a</p><p>b<br>c</p>');
  });
});
