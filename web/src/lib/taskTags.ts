// A task's tags as chips (PRD R9, SPEC 11): what a chip says (its tag and every source it has), which chips the operator may
// remove (a task's own, never an initiative's or a derived actor kind's), the add rule (a duplicate and an invalid tag send
// nothing), the suggestions typing offers, and the one read that gives any list of tasks its `tag_marks`. The components only
// draw; every decision is here.
import { chipSourceText, type TagChoice, type TagMark, type TagSource } from './chipFilter';
import { errorText } from './fetches';

/** The grammar of a tag (SPEC 4.2), the same one the server applies: `^[a-z0-9][a-z0-9._:-]{0,39}$`. */
export const TAG_PATTERN = /^[a-z0-9][a-z0-9._:-]{0,39}$/;

/** A task carries at most this many tags of its own. */
export const MAX_TAGS = 20;

/** How many suggestions typing shows. */
export const SUGGESTION_LIMIT = 8;

/** The tags a task shows, one chip each in the order the marks first name them: a tag with two sources is one chip naming both. */
export function tagChips(marks: readonly TagMark[]): TagChoice[] {
  const sources = new Map<string, TagSource[]>();
  for (const mark of marks) {
    const seen = sources.get(mark.tag) ?? [];
    if (!seen.includes(mark.source)) seen.push(mark.source);
    sources.set(mark.tag, seen);
  }
  return [...sources].map(([tag, own]) => ({ tag, sources: own }));
}

/** The task's own tags among its marks. */
export function ownTags(marks: readonly TagMark[]): string[] {
  return marks.filter((m) => m.source === 'task').map((m) => m.tag);
}

/** Whether the operator may remove a chip: only when the tag is one of the task's own. */
export function removable(chip: TagChoice): boolean {
  return chip.sources.includes('task');
}

/** What a chip's hover title says: the tag and its sources. */
export function chipTitle(chip: TagChoice): string {
  return `${chip.tag}: ${chipSourceText(chip)}`;
}

/** The accessible name of a chip's remove button. */
export function removeLabel(tag: string, ref: string): string {
  return `Remove tag ${tag} from ${ref}`;
}

/** What typing a tag would do: add it, nothing (the task has it), or refuse it with the reason. */
export type TagPlan = { kind: 'add'; tag: string } | { kind: 'duplicate'; tag: string } | { kind: 'invalid'; message: string };

/** The tag a typed text names: trimmed and lowercased, as the server does. */
export function normalizeTag(text: string): string {
  return text.trim().toLowerCase();
}

/** Decides what typing `text` does on a task whose own tags are `own`. A duplicate and an invalid tag send no request. */
export function planAdd(own: readonly string[], text: string): TagPlan {
  const tag = normalizeTag(text);
  if (tag === '') return { kind: 'invalid', message: 'Type a tag to add.' };
  if (!TAG_PATTERN.test(tag)) {
    return { kind: 'invalid', message: 'A tag is lowercase letters, digits and . _ : -, starts with a letter or digit and has at most 40 characters.' };
  }
  if (own.includes(tag)) return { kind: 'duplicate', tag };
  if (own.length >= MAX_TAGS) return { kind: 'invalid', message: `A task carries at most ${MAX_TAGS} tags.` };
  return { kind: 'add', tag };
}

/**
 * The tags typing `text` suggests: the tags in use that contain the text (those that start with it first), none the task
 * already has as its own, each once, alphabetical within each group, at most `SUGGESTION_LIMIT`. Nothing for blank text.
 */
export function suggestTags(inUse: readonly string[], own: readonly string[], text: string, limit = SUGGESTION_LIMIT): string[] {
  const needle = normalizeTag(text);
  if (needle === '') return [];
  const fits = [...new Set(inUse)].filter((tag) => tag.includes(needle) && !own.includes(tag)).sort();
  const starts = fits.filter((tag) => tag.startsWith(needle));
  return [...starts, ...fits.filter((tag) => !tag.startsWith(needle))].slice(0, limit);
}

/** The writes the editor makes. */
export interface TagWriter {
  addTag(ref: string, tag: string): Promise<unknown>;
  removeTag(ref: string, tag: string): Promise<unknown>;
}

/** What an add or a remove came to: whether the task's tags changed on the server, and the refusal to show (empty when none). */
export interface TagOutcome {
  changed: boolean;
  error: string;
}

/** Adds the typed tag to the task `ref` whose own tags are `own`. An invalid tag and a duplicate make no request. */
export async function addTaskTag(api: TagWriter, ref: string, own: readonly string[], text: string): Promise<TagOutcome> {
  const plan = planAdd(own, text);
  if (plan.kind === 'invalid') return { changed: false, error: plan.message };
  if (plan.kind === 'duplicate') return { changed: false, error: '' };
  return write(() => api.addTag(ref, plan.tag));
}

/** Removes `tag` from the task `ref`: only a tag among its own marks is sent; any other chip has no remove. */
export async function removeTaskTag(api: TagWriter, ref: string, marks: readonly TagMark[], tag: string): Promise<TagOutcome> {
  if (!ownTags(marks).includes(tag)) return { changed: false, error: '' };
  return write(() => api.removeTag(ref, tag));
}

async function write(call: () => Promise<unknown>): Promise<TagOutcome> {
  try {
    await call();
    return { changed: true, error: '' };
  } catch (e) {
    return { changed: false, error: errorText(e) };
  }
}

/** What makes a chip list editable: the tags in use to suggest, and the two writes (each answers the refusal to show, empty when it worked). */
export interface TagEditor {
  inUse: readonly string[];
  onadd: (text: string) => Promise<string>;
  onremove: (tag: string) => Promise<string>;
}

/** The editor of task `ref` whose marks are `marks`: each write that changed the task runs `changed` (the view reads again) before it answers. */
export function tagEditor(api: TagWriter, ref: string, marks: readonly TagMark[], inUse: readonly string[], changed: () => unknown): TagEditor {
  const finish = async (outcome: TagOutcome) => {
    if (outcome.changed) await changed();
    return outcome.error;
  };
  return {
    inUse,
    onadd: async (text) => finish(await addTaskTag(api, ref, ownTags(marks), text)),
    onremove: async (tag) => finish(await removeTaskTag(api, ref, marks, tag)),
  };
}

/** How many refs one `GET /tasks?ref=` read carries: the URL stays short and the page size (100) holds them. */
export const REFS_PER_READ = 100;

/** A page of tasks as the marks read needs it. */
export interface MarksPage {
  items: readonly { ref: string; tag_marks: readonly TagMark[] }[];
}

/** The marks of the tasks `refs` name, by ref (the canonical ref the server gives): one `GET /tasks?status=all&ref=..` per 100 refs. */
export async function readMarks(page: (query: URLSearchParams) => Promise<MarksPage>, refs: readonly string[]): Promise<Record<string, TagMark[]>> {
  const out: Record<string, TagMark[]> = {};
  const unique = [...new Set(refs)];
  for (let at = 0; at < unique.length; at += REFS_PER_READ) {
    const query = new URLSearchParams({ status: 'all', limit: String(REFS_PER_READ) });
    for (const ref of unique.slice(at, at + REFS_PER_READ)) query.append('ref', ref);
    for (const item of (await page(query)).items) out[item.ref] = [...item.tag_marks];
  }
  return out;
}

/** The tags in use on the board, from the marks a view holds: every distinct tag of every source, sorted. */
export function tagsInUse(...lists: readonly (readonly TagMark[])[]): string[] {
  return [...new Set(lists.flatMap((marks) => marks.map((m) => m.tag)))].sort();
}
