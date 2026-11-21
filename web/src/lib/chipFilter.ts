// The shared chip filter (PRD R7, SPEC 11): tags combine with AND (a task must carry every tag chosen), initiatives with
// OR (a task in any initiative chosen), and the two kinds of chip with each other by AND. The state lives in the URL
// fragment as repeatable `tag` and `initiative` parameters, goes to the API the same way (`GET /tasks?tag=a&tag=b&initiative=x`),
// and is saved with the view's query. This module holds the state, the query round trip, the choices a view offers and the
// one colour rule for a tag; the components only draw it.
import type { components } from './api/schema';

/** The state of the chip filter: the tags chosen (all of them) and the initiatives chosen (any of them). */
export interface ChipState {
  tags: string[];
  initiatives: string[];
}

export const NO_CHIPS: ChipState = { tags: [], initiatives: [] };

/** Where a task's tag comes from, as the API's `tag_marks` name it. */
export type TagSource = components['schemas']['TagMark']['source'];

/** A tag of a task with its source, as the API's `tag_marks` give it. */
export type TagMark = components['schemas']['TagMark'];

/** One tag chip on offer, with every source it was seen with (a tag can be a task's own and its initiative's). */
export interface TagChoice {
  tag: string;
  sources: TagSource[];
}

/** Whether any chip is chosen. */
export function chipsActive(state: ChipState): boolean {
  return state.tags.length + state.initiatives.length > 0;
}

/** A list with `value` added when it is absent and removed when it is there; the order of the others is kept. */
export function toggle(values: readonly string[], value: string): string[] {
  return values.includes(value) ? values.filter((v) => v !== value) : [...values, value];
}

// The runes Go's `strings.TrimSpace` strips (`unicode.IsSpace`): not `String.prototype.trim`'s set, which keeps U+0085 and strips U+FEFF.
const SERVER_SPACE = '\\t\\n\\v\\f\\r \\u0085\\u00a0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000';
const SERVER_TRIM = new RegExp(`^[${SERVER_SPACE}]+|[${SERVER_SPACE}]+$`, 'g');

/**
 * A tag as the server holds it (SPEC 6.4, `domain.NormalizeTags`: `strings.ToLower(strings.TrimSpace(raw))`). A link or a saved
 * filter may spell a tag ` RALPH ` and the server reads it as `ralph`; the chip, its source and the restore check all use this one
 * spelling. The trim is Go's (U+0085 goes, U+FEFF stays) and the lower-casing is Go's per-rune one (U+0130 is `i`, where
 * JavaScript's full mapping adds a combining dot).
 */
export function canonicalTag(raw: string): string {
  return raw.replace(SERVER_TRIM, '').replaceAll('\u0130', 'i').toLowerCase();
}

/** Tags in the server's spelling, each once, in the order given, blanks dropped. */
export function canonicalTags(values: readonly string[]): string[] {
  return [...new Set(values.map(canonicalTag).filter((v) => v !== ''))];
}

/** The chips a query names: each repeated `tag` (in the server's spelling) and `initiative` once, in the order given, blanks dropped. */
export function chipsFromQuery(query: string): ChipState {
  const params = new URLSearchParams(query);
  const initiatives = [...new Set(params.getAll('initiative').filter((v) => v !== ''))];
  return { tags: canonicalTags(params.getAll('tag')), initiatives };
}

/** Adds the chips to `params` as repeated `initiative` then repeated `tag`: the form the fragment and `GET /tasks` share. */
export function chipsToParams(params: URLSearchParams, state: ChipState): void {
  for (const slug of state.initiatives) params.append('initiative', slug);
  for (const tag of state.tags) params.append('tag', tag);
}

/** The words a screen reader and the hover title give for a source of a tag. */
export function sourceLabel(source: TagSource): string {
  switch (source) {
    case 'task':
      return 'task tag';
    case 'initiative':
      return 'initiative tag';
    default:
      return 'actor kind, from the holder of a lease';
  }
}

/** "task tag, initiative tag": every source of a chip, in the order task, initiative, actor kind. */
export function chipSourceText(choice: TagChoice): string {
  const order: TagSource[] = ['task', 'initiative', 'actor'];
  return order
    .filter((s) => choice.sources.includes(s))
    .map(sourceLabel)
    .join(', ');
}

/** The name a screen reader reads for a tag chip: the tag, then its sources (just the tag when none is known). */
export function chipName(choice: TagChoice): string {
  const sources = chipSourceText(choice);
  return sources === '' ? choice.tag : `${choice.tag}, ${sources}`;
}

/** The tag choices after reading more tasks: the ones already known, every mark of `marks`, and the chosen ones; sorted by tag. */
export function mergeChoices(known: readonly TagChoice[], marks: readonly { tag: string; source: TagSource }[], chosen: readonly string[]): TagChoice[] {
  const map = new Map<string, Set<TagSource>>();
  const add = (tag: string, source: TagSource | undefined) => {
    const sources = map.get(tag) ?? new Set<TagSource>();
    if (source !== undefined) sources.add(source);
    map.set(tag, sources);
  };
  for (const c of known) for (const s of c.sources) add(c.tag, s);
  for (const m of marks) add(m.tag, m.source);
  for (const tag of chosen) add(tag, undefined);
  return [...map.entries()].map(([tag, sources]) => ({ tag, sources: [...sources] })).sort((a, b) => a.tag.localeCompare(b.tag));
}

/**
 * The choices after a fresh answer for some tags (the per-tag read of `carriedTags`, the server's rule over tasks of any status):
 * each tag of `answered` has exactly the sources `marks` give it now, so a source the server no longer reports goes instead of
 * being kept from an earlier read; the other tags keep theirs. A tag stays on offer with no source rather than being taken away.
 * `marks` may carry sources for other tags too (the initiatives' list): those are added as `mergeChoices` adds them.
 */
export function withFreshSources(known: readonly TagChoice[], answered: ReadonlySet<string>, marks: readonly TagMark[]): TagChoice[] {
  const reset = known.filter((c) => !answered.has(c.tag));
  return mergeChoices(reset, marks, known.filter((c) => answered.has(c.tag)).map((c) => c.tag));
}

/**
 * The choices without one source: a chip that had only that source goes, one with another source keeps it. For a catalog that
 * has just been read again: what it still carries is offered again from the new list, what it dropped is not kept by memory.
 */
export function withoutSource(choices: readonly TagChoice[], source: TagSource): TagChoice[] {
  return choices
    .filter((c) => c.sources.length === 0 || c.sources.some((s) => s !== source))
    .map((c) => ({ tag: c.tag, sources: c.sources.filter((s) => s !== source) }));
}

/** The initiative chips on offer: every slug of the board plus the chosen ones (a link may name one the list lacks), sorted. */
export function initiativeChoices(slugs: readonly string[], chosen: readonly string[]): string[] {
  return [...new Set([...slugs, ...chosen])].sort();
}

/** How many initiative chips show before the rest fold behind "more"; the chosen ones always show. */
export const INITIATIVE_FOLD = 14;

/** The initiative chips shown: all of them when open or few, else the first ones plus every chosen one. */
export function visibleInitiatives(all: readonly string[], chosen: readonly string[], open: boolean): string[] {
  if (open || all.length <= INITIATIVE_FOLD) return [...all];
  return all.filter((slug, n) => n < INITIATIVE_FOLD || chosen.includes(slug));
}

/** The summary of the folded phone filter: how many chips are chosen, or what could be (just tags on a view with no initiative chips). */
export function chipSummary(state: ChipState, withInitiatives = true): string {
  const n = state.tags.length + state.initiatives.length;
  return n === 0 ? (withInitiatives ? 'tags, initiatives' : 'tags') : `${n} selected`;
}

/**
 * The one colour rule for a tag: a hue (0-359) from the FNV-1a hash of its text. The same text always has the same hue (two
 * different texts usually differ, but with 360 hues some share one), and
 * the hue is the same in the light and the dark theme: the stylesheet's `.vh-tag` takes it as `--h` and picks the pastel
 * lightness for the theme.
 */
export function tagHue(tag: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < tag.length; i++) {
    h ^= tag.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0) % 360;
}

/** The inline style that gives a chip its hue. */
export function tagStyle(tag: string): string {
  return `--h:${tagHue(tag)}`;
}
