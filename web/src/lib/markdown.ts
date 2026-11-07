// A small, safe Markdown renderer for initiative descriptions (PRD R9). The source is never trusted: every
// character that reaches the output is escaped first, so raw HTML shows as text, and the only markup produced
// is what this module writes itself. Supported: paragraphs, headings, bullet and numbered lists, block quotes,
// fenced code, inline code, bold, italic and links. A link whose scheme is not http, https or mailto is dropped
// to its text, and a kept link carries `rel="noopener noreferrer"`.

const ESCAPES: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };

/** `text` with the five HTML-significant characters escaped; fit for element content and attribute values. */
export function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (c) => ESCAPES[c]);
}

const SAFE_SCHEMES = new Set(['http', 'https', 'mailto']);
const SCHEME = /^([a-z][a-z0-9+.-]*):/i;
// Browsers ignore whitespace and control characters inside a URL, so `java\tscript:` is `javascript:`.
function isNoise(code: number): boolean {
  return code <= 0x20 || (code >= 0x7f && code <= 0x9f);
}

function withoutNoise(url: string): string {
  return [...url].filter((c) => !isNoise(c.codePointAt(0) as number)).join('');
}

/** Whether a link target may be linked: a relative reference, or an http, https or mailto URL. */
export function isSafeUrl(url: string): boolean {
  const scheme = SCHEME.exec(withoutNoise(url));
  return scheme === null || SAFE_SCHEMES.has(scheme[1].toLowerCase());
}

function link(label: string, url: string): string {
  const text = renderInline(label);
  if (!isSafeUrl(url)) return text;
  return `<a href="${escapeHtml(url)}" rel="noopener noreferrer">${text}</a>`;
}

// One alternative per inline construct; the capture groups tell which one matched.
const INLINE = /`([^`]+)`|\[([^\]]+)\]\(([^)\s]+)\)|\*\*(.+?)\*\*|\*(.+?)\*/g;

function inlineToken(m: RegExpExecArray): string {
  if (m[1] !== undefined) return `<code>${escapeHtml(m[1])}</code>`;
  if (m[2] !== undefined) return link(m[2], m[3]);
  if (m[4] !== undefined) return `<strong>${renderInline(m[4])}</strong>`;
  return `<em>${renderInline(m[5])}</em>`;
}

/** One line of text as inline HTML. */
export function renderInline(text: string): string {
  let out = '';
  let last = 0;
  for (const m of text.matchAll(INLINE)) {
    out += escapeHtml(text.slice(last, m.index)) + inlineToken(m as RegExpExecArray);
    last = m.index + m[0].length;
  }
  return out + escapeHtml(text.slice(last));
}

const FENCE = /^```/;
const HEADING = /^(#{1,6})\s+(.*?)\s*#*\s*$/;
const BULLET = /^\s*[-*+]\s+(.*)$/;
const NUMBERED = /^\s*\d+[.)]\s+(.*)$/;
const QUOTE = /^>\s?(.*)$/;

/** A heading is shown two levels down, because the page's own title is the h1 and its sections the h2. */
function heading(m: RegExpExecArray): string {
  const level = Math.min(m[1].length + 2, 6);
  return `<h${level}>${renderInline(m[2])}</h${level}>`;
}

// A block that spans one run of lines: a list or a quote. `pattern` picks its lines, `wrap` makes the element.
interface Run {
  pattern: RegExp;
  wrap: (lines: string[]) => string;
}

const items = (lines: string[]): string => lines.map((l) => `<li>${renderInline(l)}</li>`).join('');

const RUNS: Run[] = [
  { pattern: BULLET, wrap: (lines) => `<ul>${items(lines)}</ul>` },
  { pattern: NUMBERED, wrap: (lines) => `<ol>${items(lines)}</ol>` },
  { pattern: QUOTE, wrap: (lines) => `<blockquote><p>${lines.map(renderInline).join('<br>')}</p></blockquote>` },
];

function startsBlock(line: string): boolean {
  return FENCE.test(line) || HEADING.test(line) || RUNS.some((r) => r.pattern.test(line));
}

/** The index after the lines from `from` that `keep` accepts. */
function runEnd(lines: string[], from: number, keep: (line: string) => boolean): number {
  let end = from;
  while (end < lines.length && keep(lines[end])) end++;
  return end;
}

function fenced(lines: string[], from: number): [string, number] {
  const body = runEnd(lines, from + 1, (l) => !FENCE.test(l));
  const html = `<pre><code>${escapeHtml(lines.slice(from + 1, body).join('\n'))}</code></pre>`;
  return [html, body + 1];
}

function run(lines: string[], from: number, r: Run): [string, number] {
  const end = runEnd(lines, from, (l) => r.pattern.test(l));
  const texts = lines.slice(from, end).map((l) => (r.pattern.exec(l) as RegExpExecArray)[1]);
  return [r.wrap(texts), end];
}

function paragraph(lines: string[], from: number): [string, number] {
  const end = runEnd(lines, from + 1, (l) => l.trim() !== '' && !startsBlock(l));
  return [`<p>${lines.slice(from, end).map((l) => renderInline(l.trim())).join('<br>')}</p>`, end];
}

/** The block that starts at line `from`, and the index of the line after it. */
function block(lines: string[], from: number): [string, number] {
  const line = lines[from];
  if (FENCE.test(line)) return fenced(lines, from);
  const head = HEADING.exec(line);
  if (head !== null) return [heading(head), from + 1];
  const r = RUNS.find((candidate) => candidate.pattern.test(line));
  return r === undefined ? paragraph(lines, from) : run(lines, from, r);
}

/** `source` as safe HTML. Blank input gives the empty string. */
export function renderMarkdown(source: string): string {
  const lines = source.replace(/\r\n?/g, '\n').split('\n');
  const html: string[] = [];
  let i = 0;
  while (i < lines.length) {
    if (lines[i].trim() === '') {
      i++;
      continue;
    }
    const [out, next] = block(lines, i);
    html.push(out);
    i = next;
  }
  return html.join('');
}
