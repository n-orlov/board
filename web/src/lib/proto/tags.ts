// UX prototype: per-task tags. A task's tags are its initiative's tags, the kind of each live lease holder
// (`ralph:run-7` gives `ralph`) and the tags the operator added by hand (kept in this browser only).
// The chip filter combines tags with AND and initiatives with OR, the same as GET /attention.

/** The handle prefixes that name a kind of agent; a holder with one of them tags its task. */
export const ACTOR_KINDS = ['ralph', 'claude', 'pi', 'codex', 'deck'] as const;

/** The agent kind of a handle (`deck:sess-1` gives `deck`), or undefined for a person or an unknown prefix. */
export function actorKind(handle: string): string | undefined {
  const at = handle.indexOf(':');
  if (at <= 0) return undefined;
  const prefix = handle.slice(0, at).toLowerCase();
  return (ACTOR_KINDS as readonly string[]).includes(prefix) ? prefix : undefined;
}

/** A stable hue (0-359) for a tag: FNV-1a over its text. */
export function tagHue(tag: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < tag.length; i++) {
    h ^= tag.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0) % 360;
}

export type TagSource = 'initiative' | 'actor' | 'manual';

export interface TaskTag {
  tag: string;
  source: TagSource;
}

export interface Taggable {
  ref: string;
  initiative: string;
  live_leases?: readonly { actor: string }[];
}

/** The tags of a task, each once, initiative tags first, then agent kinds, then manual tags. */
export function taskTags(task: Taggable, initiativeTags: ReadonlyMap<string, readonly string[]>, manual: readonly string[]): TaskTag[] {
  const out: TaskTag[] = [];
  const seen = new Set<string>();
  const add = (tag: string, source: TagSource) => {
    if (tag === '' || seen.has(tag)) return;
    seen.add(tag);
    out.push({ tag, source });
  };
  for (const tag of initiativeTags.get(task.initiative) ?? []) add(tag, 'initiative');
  for (const lease of task.live_leases ?? []) {
    const kind = actorKind(lease.actor);
    if (kind !== undefined) add(kind, 'actor');
  }
  for (const tag of manual) add(tag, 'manual');
  return out;
}

/** The chip filter's state: tags (all of them) and initiatives (any of them). */
export interface ChipState {
  tags: string[];
  initiatives: string[];
}

export const NO_CHIPS: ChipState = { tags: [], initiatives: [] };

export function chipsActive(state: ChipState): boolean {
  return state.tags.length + state.initiatives.length > 0;
}

/** Whether a task with these tags in this initiative passes the chips. */
export function passesChips(tags: readonly string[], initiative: string | undefined, state: ChipState): boolean {
  if (state.initiatives.length > 0 && (initiative === undefined || !state.initiatives.includes(initiative))) return false;
  return state.tags.every((tag) => tags.includes(tag));
}

/** A tag choice the chip filter offers, with where it comes from (the first source seen wins). */
export interface TagChoice {
  tag: string;
  source: TagSource;
}

/** The tag choices: every tag of every task given plus the initiative tags, plus the selected ones, sorted by source then name. */
export function tagChoices(lists: readonly (readonly TaskTag[])[], initiativeTags: Iterable<readonly string[]>, selected: readonly string[]): TagChoice[] {
  const map = new Map<string, TagSource>();
  for (const tags of initiativeTags) for (const tag of tags) if (!map.has(tag)) map.set(tag, 'initiative');
  for (const list of lists) for (const t of list) if (!map.has(t.tag)) map.set(t.tag, t.source);
  for (const tag of selected) if (!map.has(tag)) map.set(tag, 'initiative');
  const order: Record<TagSource, number> = { initiative: 0, actor: 1, manual: 2 };
  return [...map.entries()].map(([tag, source]) => ({ tag, source })).sort((a, b) => order[a.source] - order[b.source] || a.tag.localeCompare(b.tag));
}

/** Toggle a value in a list. */
export function toggle(values: readonly string[], value: string): string[] {
  return values.includes(value) ? values.filter((v) => v !== value) : [...values, value];
}

/** The ref of the task a `#/t/<ref>` fragment names. */
export function refOfHref(href: string | undefined): string | undefined {
  if (href === undefined || !href.startsWith('#/t/')) return undefined;
  return decodeURIComponent(href.slice(4));
}
