// The filters of the Notes view (PRD R10): broadcast only, task, initiative, resource, author and mention. They live
// in the URL fragment's query (`#/notes?broadcast=only&author=ralph`) so a reload keeps them. This module turns the
// filter into that query and back, and into the query of `GET /notes`.

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
