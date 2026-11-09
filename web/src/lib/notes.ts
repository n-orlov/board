// The filters of the Notes view (PRD R10): broadcast only, task, initiative, resource, author and mention. They live
// in the URL fragment's query (`#/notes?broadcast=only&author=ralph`) so a reload keeps them. This module turns the
// filter into that query and back, and into the query of `GET /notes`. It also holds the Post note form's checks and
// the order a thread of notes is drawn in.
import type { components } from './api/schema';

export interface NotesFilter {
  /** Keep only the notes that have neither a task nor an initiative. */
  broadcastOnly: boolean;
  /** A task ref; empty for no filter. */
  task: string;
  /** An initiative slug; empty for no filter. */
  initiative: string;
  /** A resource such as `host:build-1`; empty for no filter. */
  resource: string;
  /** The handle of the author; empty for no filter. */
  author: string;
  /** The handle of a mentioned actor; empty for no filter. */
  mention: string;
}

export const DEFAULT_NOTES_FILTER: NotesFilter = {
  broadcastOnly: false,
  task: '',
  initiative: '',
  resource: '',
  author: '',
  mention: '',
};

type TextField = 'task' | 'initiative' | 'resource' | 'author' | 'mention';

/** The text filters in the order their keys appear in the query; the key is the field's own name. */
const TEXT_FIELDS: readonly TextField[] = ['task', 'initiative', 'resource', 'author', 'mention'];

/** The filter a query names; an absent value is the default for that field, and values are trimmed. */
export function filterFromQuery(query: string): NotesFilter {
  const params = new URLSearchParams(query);
  const text = (name: TextField): string => (params.get(name) ?? '').trim();
  return {
    broadcastOnly: params.get('broadcast') === 'only',
    task: text('task'),
    initiative: text('initiative'),
    resource: text('resource'),
    author: text('author'),
    mention: text('mention'),
  };
}

/** The query of a filter for the URL fragment: only the fields that are set, so the default is empty. */
export function filterToQuery(filter: NotesFilter): string {
  const params = new URLSearchParams();
  if (filter.broadcastOnly) params.set('broadcast', 'only');
  for (const name of TEXT_FIELDS) {
    const value = filter[name].trim();
    if (value !== '') params.set(name, value);
  }
  return params.toString();
}

/** The fragment of the Notes view under a filter: `#/notes`, or `#/notes?...` when a field is set. */
export function notesHash(filter: NotesFilter): string {
  const query = filterToQuery(filter);
  return query === '' ? '#/notes' : `#/notes?${query}`;
}

/** The filter held by a fragment: everything after the first `?` is the query. */
export function filterFromHash(hash: string): NotesFilter {
  const at = hash.indexOf('?');
  return filterFromQuery(at < 0 ? '' : hash.slice(at + 1));
}

/** The query of `GET /notes` for a filter: the same keys, and `broadcast=only` for the broadcast filter. */
export function apiQuery(filter: NotesFilter): URLSearchParams {
  return new URLSearchParams(filterToQuery(filter));
}

/** Whether any field is set (the view offers "clear filters" then). */
export function isFiltered(filter: NotesFilter): boolean {
  return filterToQuery(filter) !== '';
}

export type Note = components['schemas']['Note'];
export type NotePost = components['schemas']['NotePost'];

/** The most a note's summary, resource, body and mention list may hold. */
export const SUMMARY_LIMIT = 200;
export const RESOURCE_LIMIT = 100;
export const BODY_LIMIT = 20000;
export const MENTIONS_LIMIT = 20;

/** What the Post note form holds; `mentions` is a list of handles separated by spaces or commas. */
export interface NoteForm {
  summary: string;
  body: string;
  resource: string;
  mentions: string;
}

export const EMPTY_NOTE_FORM: NoteForm = { summary: '', body: '', resource: '', mentions: '' };

/** The notes newest first (the API gives them in posting order). */
export function newestFirst(notes: readonly Note[]): Note[] {
  return [...notes].reverse();
}

/** The handles a mentions field names: split on spaces and commas, empty pieces dropped, repeats kept once. */
export function parseMentions(text: string): string[] {
  return [...new Set(text.split(/[\s,]+/).filter((h) => h !== ''))];
}

function chars(text: string): number {
  return [...text].length;
}

function overLimit(what: string, text: string, limit: number): string | undefined {
  const length = chars(text);
  return length > limit ? `${what} is at most ${limit} characters; this one has ${length}.` : undefined;
}

/** Why a form cannot be sent: the first check it fails, in the order the fields are drawn. Undefined when it can. */
export function noteProblem(form: NoteForm): string | undefined {
  const mentions = parseMentions(form.mentions).length;
  return (
    (form.summary.trim() === '' ? 'Write a summary.' : undefined) ??
    overLimit('A summary', form.summary.trim(), SUMMARY_LIMIT) ??
    overLimit('A body', form.body, BODY_LIMIT) ??
    overLimit('A resource', form.resource.trim(), RESOURCE_LIMIT) ??
    (mentions > MENTIONS_LIMIT ? `A note mentions at most ${MENTIONS_LIMIT} actors; this one names ${mentions}.` : undefined)
  );
}

/** The post a form makes for a note on `scope` (a task ref or an initiative slug), or the reason it cannot be sent. */
export function toNotePost(form: NoteForm, scope: { task?: string; initiative?: string }): { ok: true; body: NotePost } | { ok: false; error: string } {
  const problem = noteProblem(form);
  if (problem !== undefined) return { ok: false, error: problem };
  const mentions = parseMentions(form.mentions);
  const body: NotePost = { summary: form.summary.trim(), ...scope };
  if (form.body.trim() !== '') body.body = form.body;
  if (form.resource.trim() !== '') body.resource = form.resource.trim();
  if (mentions.length > 0) body.mentions = mentions;
  return { ok: true, body };
}
