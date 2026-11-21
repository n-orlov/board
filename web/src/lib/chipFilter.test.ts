import { describe, expect, it } from 'vitest';
import {
  INITIATIVE_FOLD,
  NO_CHIPS,
  canonicalTag,
  canonicalTags,
  chipName,
  chipSourceText,
  chipSummary,
  chipsActive,
  chipsFromQuery,
  chipsToParams,
  initiativeChoices,
  mergeChoices,
  withoutSource,
  sourceLabel,
  tagHue,
  tagStyle,
  toggle,
  visibleInitiatives,
  type ChipState,
} from './chipFilter';

describe('the chip state', () => {
  it('is empty and inactive by default, active with any chip', () => {
    expect(chipsActive(NO_CHIPS)).toBe(false);
    expect(chipsActive({ tags: ['a'], initiatives: [] })).toBe(true);
    expect(chipsActive({ tags: [], initiatives: ['x'] })).toBe(true);
  });

  it('toggles a value on at the end and off again, keeping the others in order', () => {
    expect(toggle([], 'a')).toEqual(['a']);
    expect(toggle(['a', 'b'], 'c')).toEqual(['a', 'b', 'c']);
    expect(toggle(['a', 'b', 'c'], 'b')).toEqual(['a', 'c']);
    const before = ['a'];
    toggle(before, 'b');
    expect(before).toEqual(['a']);
  });

  it('picks two tags and two initiatives independently of each other', () => {
    let state: ChipState = NO_CHIPS;
    state = { ...state, tags: toggle(state.tags, 'red') };
    state = { ...state, initiatives: toggle(state.initiatives, 'x') };
    state = { ...state, tags: toggle(state.tags, 'blue') };
    state = { ...state, initiatives: toggle(state.initiatives, 'y') };
    expect(state).toEqual({ tags: ['red', 'blue'], initiatives: ['x', 'y'] });
    state = { ...state, tags: toggle(state.tags, 'red') };
    expect(state).toEqual({ tags: ['blue'], initiatives: ['x', 'y'] });
  });
});

describe('the fragment round trip', () => {
  const apply = (state: ChipState) => {
    const params = new URLSearchParams();
    chipsToParams(params, state);
    return params.toString();
  };

  it('writes repeated initiative then repeated tag parameters, and reads them back', () => {
    const state: ChipState = { tags: ['a', 'b:c'], initiatives: ['x', 'y'] };
    const query = apply(state);
    expect(query).toBe('initiative=x&initiative=y&tag=a&tag=b%3Ac');
    expect(chipsFromQuery(query)).toEqual(state);
  });

  it('writes nothing for no chips and reads nothing from an empty query', () => {
    expect(apply(NO_CHIPS)).toBe('');
    expect(chipsFromQuery('')).toEqual(NO_CHIPS);
  });

  it('drops blanks and repeats and ignores other parameters', () => {
    expect(chipsFromQuery('status=all&tag=&tag=a&tag=a&initiative=&initiative=x&q=t')).toEqual({ tags: ['a'], initiatives: ['x'] });
  });

  it('reads each tag in the server\'s spelling (trimmed, lower case), so a link or saved filter that spells it otherwise names the same chip', () => {
    expect(canonicalTag(' RALPH ')).toBe('ralph');
    expect(canonicalTag('Release.2026:Q1')).toBe('release.2026:q1');
    expect(canonicalTags([' Own-Tag ', 'own-tag', 'CODER', '  ', 'b'])).toEqual(['own-tag', 'coder', 'b']);
    expect(chipsFromQuery('tag=%20OWN-TAG%20&tag=RALPH&tag=own-tag&tag=%20&initiative=Mixed')).toEqual({ tags: ['own-tag', 'ralph'], initiatives: ['Mixed'] });
  });

  it('trims and lower-cases the way the server (Go) does, not the way String.trim does', () => {
    // Go's strings.TrimSpace strips U+0085, U+00A0 and the Unicode space separators; it keeps U+FEFF (which String.trim strips).
    expect(canonicalTag('\u0085own\u0085')).toBe('own');
    expect(canonicalTag('\u00a0\u2003own\u3000\u2028')).toBe('own');
    expect(canonicalTag('\t\n\v\f\r own \t')).toBe('own');
    expect(canonicalTag('\ufeffown')).toBe('\ufeffown');
    expect(canonicalTag('own\u200b')).toBe('own\u200b');
    // Go lower-cases rune by rune: U+0130 is `i`, where String.toLowerCase gives `i` and a combining dot.
    expect(canonicalTag('\u0130vy')).toBe('ivy');
    expect(canonicalTag('\u212aey')).toBe('key');
    expect(canonicalTags(['\u0085A\u0085', 'a', '\u0085'])).toEqual(['a']);
  });

  it('adds to the parameters already there', () => {
    const params = new URLSearchParams('status=all');
    chipsToParams(params, { tags: ['a'], initiatives: [] });
    expect(params.toString()).toBe('status=all&tag=a');
  });
});

describe('what a chip says about its source', () => {
  it('names each source in words', () => {
    expect(sourceLabel('task')).toBe('task tag');
    expect(sourceLabel('initiative')).toBe('initiative tag');
    expect(sourceLabel('actor')).toBe('actor kind, from the holder of a lease');
  });

  it('lists every source of a chip in a fixed order', () => {
    expect(chipSourceText({ tag: 'a', sources: ['actor', 'task'] })).toBe('task tag, actor kind, from the holder of a lease');
    expect(chipSourceText({ tag: 'a', sources: ['initiative'] })).toBe('initiative tag');
    expect(chipSourceText({ tag: 'a', sources: [] })).toBe('');
  });
});

describe('the name of a tag chip', () => {
  it('is the tag and its sources, or the tag alone when none is known', () => {
    expect(chipName({ tag: 'ui', sources: ['task'] })).toBe('ui, task tag');
    expect(chipName({ tag: 'ui', sources: ['initiative', 'task'] })).toBe('ui, task tag, initiative tag');
    expect(chipName({ tag: 'ui', sources: [] })).toBe('ui');
  });
});

describe('the tag choices', () => {
  it('merges the marks of the tasks read, keeping a tag once with all of its sources, sorted', () => {
    const first = mergeChoices([], [{ tag: 'b', source: 'task' }, { tag: 'a', source: 'initiative' }], []);
    expect(first).toEqual([
      { tag: 'a', sources: ['initiative'] },
      { tag: 'b', sources: ['task'] },
    ]);
    const next = mergeChoices(first, [{ tag: 'b', source: 'initiative' }, { tag: 'ralph', source: 'actor' }], []);
    expect(next).toEqual([
      { tag: 'a', sources: ['initiative'] },
      { tag: 'b', sources: ['task', 'initiative'] },
      { tag: 'ralph', sources: ['actor'] },
    ]);
  });

  it('keeps a chosen tag on offer even when no task read carries it', () => {
    expect(mergeChoices([], [], ['gone'])).toEqual([{ tag: 'gone', sources: [] }]);
    expect(mergeChoices([{ tag: 'a', sources: ['task'] }], [], ['a'])).toEqual([{ tag: 'a', sources: ['task'] }]);
  });

  it('offers every initiative once, with the chosen ones the list lacks, sorted', () => {
    expect(initiativeChoices(['b', 'a'], ['c', 'a'])).toEqual(['a', 'b', 'c']);
    expect(initiativeChoices([], [])).toEqual([]);
  });
});

describe('folding a long list of initiatives', () => {
  const all = Array.from({ length: INITIATIVE_FOLD + 3 }, (_, n) => `i${String(n).padStart(2, '0')}`);

  it('shows every chip when the list is short or open', () => {
    expect(visibleInitiatives(all.slice(0, INITIATIVE_FOLD), [], false)).toHaveLength(INITIATIVE_FOLD);
    expect(visibleInitiatives(all, [], true)).toEqual(all);
  });

  it('folds the rest behind "more" but keeps every chosen chip on show', () => {
    expect(visibleInitiatives(all, [], false)).toEqual(all.slice(0, INITIATIVE_FOLD));
    const shown = visibleInitiatives(all, [all[INITIATIVE_FOLD + 1]!], false);
    expect(shown).toEqual([...all.slice(0, INITIATIVE_FOLD), all[INITIATIVE_FOLD + 1]]);
  });
});

describe('the phone summary', () => {
  it('says what can be picked, or how many are', () => {
    expect(chipSummary(NO_CHIPS)).toBe('tags, initiatives');
    expect(chipSummary(NO_CHIPS, false)).toBe('tags');
    expect(chipSummary({ tags: ['a'], initiatives: [] }, false)).toBe('1 selected');
    expect(chipSummary({ tags: ['a', 'b'], initiatives: ['x'] })).toBe('3 selected');
  });
});

describe('the tag colour', () => {
  it('gives the same text the same hue, always', () => {
    expect(tagHue('urgent')).toBe(tagHue('urgent'));
    expect(tagStyle('urgent')).toBe(tagStyle('urgent'));
  });

  it('gives different texts different hues', () => {
    const tags = ['urgent', 'ralph', 'claude', 'docs', 'ui', 'backend'];
    const hues = tags.map(tagHue);
    expect(new Set(hues).size).toBe(tags.length);
    expect(tagHue('a')).not.toBe(tagHue('b'));
  });

  it('has only 360 hues, so two texts can share one (the text, not the colour, tells them apart)', () => {
    expect(tagHue('tag28')).toBe(177);
    expect(tagHue('tag64')).toBe(177);
  });

  it('is the FNV-1a hash of the text modulo 360', () => {
    // FNV-1a 32-bit of "a" is 0xe40c292c = 3826002220, mod 360 = 340. The empty text is the offset basis 0x811c9dc5 = 2166136261, mod 360 = 61.
    expect(tagHue('a')).toBe(340);
    expect(tagHue('')).toBe(61);
    expect(tagHue('urgent')).toBe(216);
  });

  it('stays in 0..359 and is written as the --h custom property', () => {
    for (const tag of ['x', 'y:z', 'a.b_c-d', 'a'.repeat(40)]) {
      const hue = tagHue(tag);
      expect(hue).toBeGreaterThanOrEqual(0);
      expect(hue).toBeLessThan(360);
      expect(tagStyle(tag)).toBe(`--h:${hue}`);
    }
  });
});

describe('withoutSource', () => {
  const kept = [
    { tag: 'a', sources: ['initiative' as const] },
    { tag: 'b', sources: ['task' as const, 'initiative' as const] },
    { tag: 'c', sources: ['actor' as const] },
    { tag: 'd', sources: [] },
  ];
  it('drops a chip that had only that source and keeps the others of a chip that had several', () => {
    expect(withoutSource(kept, 'initiative')).toEqual([
      { tag: 'b', sources: ['task'] },
      { tag: 'c', sources: ['actor'] },
      { tag: 'd', sources: [] },
    ]);
  });
  it('keeps a chip that never named a source (a chosen tag) and does not change its input', () => {
    expect(withoutSource(kept, 'actor').map((c) => c.tag)).toEqual(['a', 'b', 'd']);
    expect(kept[0]).toEqual({ tag: 'a', sources: ['initiative'] });
  });
  it('is the identity when no chip has the source', () => {
    expect(withoutSource([{ tag: 'x', sources: ['task'] }], 'actor')).toEqual([{ tag: 'x', sources: ['task'] }]);
  });
  it('lets the catalog read again offer what it still carries, then merges it back with the sources of the tasks', () => {
    const next = mergeChoices(withoutSource(kept, 'initiative'), [{ tag: 'b', source: 'initiative' }], []);
    expect(next.find((c) => c.tag === 'a')).toBeUndefined();
    expect(next.find((c) => c.tag === 'b')?.sources).toEqual(['task', 'initiative']);
  });
});
