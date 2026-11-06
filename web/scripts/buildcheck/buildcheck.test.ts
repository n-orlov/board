import { mkdtempSync, readdirSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { build } from 'vite';
import {
  cssUrls,
  htmlReferences,
  hasFocusVisibleRing,
  isNonRelative,
  missingTokens,
  nonRelativeReferences,
  reducedMotionBlock,
  TOKENS,
} from './buildcheck.ts';

let outDir: string;
let html: string;
let css: string;

// A real production build into a scratch directory (the shipped dist is left alone).
beforeAll(async () => {
  outDir = mkdtempSync(join(tmpdir(), 'board-web-build-'));
  await build({
    mode: 'production',
    logLevel: 'silent',
    build: { outDir, emptyOutDir: true },
  });
  html = readFileSync(join(outDir, 'index.html'), 'utf8');
  const cssFiles = readdirSync(join(outDir, 'assets')).filter((f) => f.endsWith('.css'));
  css = cssFiles.map((f) => readFileSync(join(outDir, 'assets', f), 'utf8')).join('\n');
}, 60_000);

afterAll(() => {
  rmSync(outDir, { recursive: true, force: true });
});

describe('built design tokens', () => {
  it('declares every token for the light theme', () => {
    expect(missingTokens(css, 'light')).toEqual([]);
  });

  it('declares every token for the dark theme', () => {
    expect(missingTokens(css, 'dark')).toEqual([]);
  });

  it('emits a selector for each theme', () => {
    expect(css).toMatch(/\[data-theme=["']?dark["']?\]/);
    expect(css).toMatch(/\[data-theme=["']?light["']?\]/);
  });
});

describe('built page references', () => {
  it('collects the index.html and stylesheet references', () => {
    expect(htmlReferences(html).length).toBeGreaterThan(0);
  });

  it('uses only relative src, href and url(...) values', () => {
    expect(nonRelativeReferences(html, css)).toEqual([]);
  });
});

describe('missingTokens', () => {
  const block = (theme: string) => `[data-theme=${theme}]{${TOKENS.map((t) => `${t}:1`).join(';')}}`;

  it('is empty when both blocks carry every token', () => {
    const both = block('light') + block('dark');
    expect(missingTokens(both, 'light')).toEqual([]);
    expect(missingTokens(both, 'dark')).toEqual([]);
  });

  it('reports every token when the dark block is deleted', () => {
    expect(missingTokens(block('light'), 'dark')).toEqual([...TOKENS]);
    expect(missingTokens(block('dark'), 'light')).toEqual([...TOKENS]);
  });

  it('reports one removed token only for the theme that lost it', () => {
    const dark = `[data-theme="dark"]{${TOKENS.filter((t) => t !== '--accent')
      .map((t) => `${t}: 1`)
      .join(';')}}`;
    const css = block('light') + dark;
    expect(missingTokens(css, 'dark')).toEqual(['--accent']);
    expect(missingTokens(css, 'light')).toEqual([]);
  });

  it('accepts a selector list and quoted attribute values', () => {
    const css = `:root,[data-theme='light']{${TOKENS.map((t) => `${t}:1`).join(';')}}`;
    expect(missingTokens(css, 'light')).toEqual([]);
  });

  it('ignores a token that is only referenced, not declared', () => {
    expect(missingTokens('[data-theme=dark]{color:var(--accent)}', 'dark')).toContain('--accent');
  });

  it('ignores a declaration outside the theme block', () => {
    expect(missingTokens(`.x{${TOKENS.map((t) => `${t}:1`).join(';')}}`, 'dark')).toEqual([...TOKENS]);
  });

  it('does not count a declaration with an empty value', () => {
    expect(missingTokens('[data-theme=dark]{--accent:;}', 'dark')).toContain('--accent');
  });
});

describe('reference extraction', () => {
  it('reads double, single and unquoted src/href values', () => {
    const doc = `<script src="a.js"></script><link href='b.css'><img src=c.png>`;
    expect(htmlReferences(doc)).toEqual(['a.js', 'b.css', 'c.png']);
  });

  it('reads quoted and unquoted url() values', () => {
    expect(cssUrls(`a{background:url("x.png")}b{background:url( 'y.png' )}c{background:url(z.png)}`)).toEqual([
      'x.png',
      'y.png',
      'z.png',
    ]);
  });

  it('flags absolute, root-relative, protocol-relative and scheme URLs', () => {
    for (const u of ['https://example.com/x.png', 'http://a/b', '//cdn.example.com/x.js', '/assets/x.js', 'data:image/png;base64,AA', 'mailto:a@b.c']) {
      expect(isNonRelative(u), u).toBe(true);
    }
  });

  it('accepts relative URLs and fragments', () => {
    for (const u of ['./assets/x.js', 'assets/x.css', '../x', '#top', '']) {
      expect(isNonRelative(u), u).toBe(false);
    }
  });

  it('fails a planted absolute reference in the page or the stylesheet', () => {
    expect(nonRelativeReferences('<img src="https://example.com/x.png">', '')).toEqual([
      'https://example.com/x.png',
    ]);
    expect(nonRelativeReferences('', 'a{background:url(https://example.com/x.png)}')).toEqual([
      'https://example.com/x.png',
    ]);
    expect(nonRelativeReferences('<link href="//example.com/x.css">', '')).toEqual(['//example.com/x.css']);
  });

  it('passes a relative page', () => {
    expect(nonRelativeReferences('<script src="./a.js"></script>', 'a{background:url(./b.png)}')).toEqual([]);
  });
});

describe('built dialog styles', () => {
  it('gives interactive elements a visible :focus-visible ring', () => {
    expect(hasFocusVisibleRing(css)).toBe(true);
  });

  it('switches animations and transitions off under prefers-reduced-motion: reduce', () => {
    const block = reducedMotionBlock(css) ?? '';
    expect(block).toMatch(/animation\s*:\s*none/);
    expect(block).toMatch(/transition\s*:\s*none/);
  });

  it('declares the dialog animations the reduced-motion block has to switch off', () => {
    expect(css).toMatch(/\.dialog-panel\s*\{[^}]*animation/);
  });
});

describe('reducedMotionBlock', () => {
  it('returns the nested body of the block', () => {
    expect(reducedMotionBlock('a{x:1}@media (prefers-reduced-motion: reduce){*{animation:none}}b{y:2}')).toBe(
      '*{animation:none}',
    );
  });
  it('is undefined without the block or when it is unterminated', () => {
    expect(reducedMotionBlock('a{x:1}')).toBeUndefined();
    expect(reducedMotionBlock('@media (prefers-reduced-motion:reduce){*{animation:none}')).toBeUndefined();
  });
  it('does not match a no-preference block', () => {
    expect(reducedMotionBlock('@media (prefers-reduced-motion: no-preference){*{animation:none}}')).toBeUndefined();
  });
});

describe('hasFocusVisibleRing', () => {
  it('accepts an outline on :focus-visible', () => {
    expect(hasFocusVisibleRing('a:focus-visible{outline:2px solid red}')).toBe(true);
  });
  it('rejects a missing rule, outline none and an offset alone', () => {
    expect(hasFocusVisibleRing('a{outline:2px solid red}')).toBe(false);
    expect(hasFocusVisibleRing('a:focus-visible{outline:none}')).toBe(false);
    expect(hasFocusVisibleRing('a:focus-visible{outline-offset:2px}')).toBe(false);
  });
});
