import { describe, expect, it } from 'vitest';
import {
  DEFAULT_NOTES_FILTER,
  apiQuery,
  filterFromHash,
  filterFromQuery,
  filterToQuery,
  isFiltered,
  newestFirst,
  parseMentions,
  toNotePost,
  notesHash,
  type NotesFilter,
} from './notes';

const FILTERS: [string, Partial<NotesFilter>, string][] = [
  ['broadcast only', { broadcastOnly: true }, 'broadcast=only'],
  ['task', { task: 'web-3' }, 'task=web-3'],
  ['initiative', { initiative: 'web' }, 'initiative=web'],
  ['resource', { resource: 'host:build-1' }, 'resource=host%3Abuild-1'],
  ['author', { author: 'ralph' }, 'author=ralph'],
  ['mention', { mention: 'alice' }, 'mention=alice'],
];

describe('each of the six filters', () => {
  for (const [name, part, query] of FILTERS) {
    it(`round-trips ${name} through the query and the fragment`, () => {
      const filter = { ...DEFAULT_NOTES_FILTER, ...part };
      expect(filterToQuery(filter)).toBe(query);
      expect(filterFromQuery(query)).toEqual(filter);
      expect(filterFromHash(notesHash(filter))).toEqual(filter);
      expect(notesHash(filter)).toBe(`#/notes?${query}`);
      expect(apiQuery(filter).toString()).toBe(query);
      expect(isFiltered(filter)).toBe(true);
    });
  }

  it('round-trips all six together', () => {
    const all: NotesFilter = {
      broadcastOnly: true,
      task: 'web-3',
      initiative: 'web',
      resource: 'host:build-1',
      author: 'ralph',
      mention: 'alice',
    };
    expect(filterFromQuery(filterToQuery(all))).toEqual(all);
    expect(filterFromHash(notesHash(all))).toEqual(all);
  });
});

describe('the default', () => {
  it('has an empty query and the bare fragment, and is not filtered', () => {
    expect(filterToQuery(DEFAULT_NOTES_FILTER)).toBe('');
    expect(notesHash(DEFAULT_NOTES_FILTER)).toBe('#/notes');
    expect(isFiltered(DEFAULT_NOTES_FILTER)).toBe(false);
    expect(filterFromHash('#/notes')).toEqual(DEFAULT_NOTES_FILTER);
    expect(filterFromQuery('')).toEqual(DEFAULT_NOTES_FILTER);
  });
});

describe('odd input', () => {
  it('ignores unknown keys, blank values and any broadcast value but only', () => {
    expect(filterFromQuery('foo=1&author=%20%20&broadcast=exclude&task=')).toEqual(DEFAULT_NOTES_FILTER);
  });

  it('trims values and leaves blanks out of the query', () => {
    expect(filterFromQuery('author=%20ralph%20').author).toBe('ralph');
    expect(filterToQuery({ ...DEFAULT_NOTES_FILTER, author: '  ', task: ' web-1 ' })).toBe('task=web-1');
  });

  it('keeps the first ? as the start of the query', () => {
    expect(filterFromHash('#/notes?resource=a?b').resource).toBe('a?b');
  });
});

describe('newestFirst and parseMentions', () => {
  it('turns posting order into newest first without touching the input', () => {
    const notes = [{ ref: 'N1' }, { ref: 'N2' }] as never[];
    expect(newestFirst(notes).map((n: { ref: string }) => n.ref)).toEqual(['N2', 'N1']);
    expect((notes[0] as { ref: string }).ref).toBe('N1');
  });

  it('splits mentions on spaces and commas and keeps each handle once', () => {
    expect(parseMentions(' alice, bob  alice ,,')).toEqual(['alice', 'bob']);
    expect(parseMentions('')).toEqual([]);
  });
});

describe('toNotePost', () => {
  const form = { summary: ' s ', body: 'b', resource: ' host:x ', mentions: 'a b' };

  it('builds the post with the scope, the trimmed summary and resource, and only the fields that are set', () => {
    expect(toNotePost(form, { task: 'aa-1' })).toEqual({ ok: true, body: { summary: 's', task: 'aa-1', body: 'b', resource: 'host:x', mentions: ['a', 'b'] } });
    expect(toNotePost({ summary: 's', body: ' ', resource: '', mentions: '' }, { initiative: 'web' })).toEqual({ ok: true, body: { summary: 's', initiative: 'web' } });
  });

  it('says why a form cannot be sent', () => {
    const error = (patch: object) => {
      const r = toNotePost({ ...form, ...patch }, {});
      return r.ok ? '' : r.error;
    };
    expect(error({ summary: '  ' })).toBe('Write a summary.');
    expect(error({ summary: 'x'.repeat(201) })).toBe('A summary is at most 200 characters; this one has 201.');
    expect(error({ body: 'x'.repeat(20001) })).toBe('A body is at most 20000 characters; this one has 20001.');
    expect(error({ resource: 'x'.repeat(101) })).toBe('A resource is at most 100 characters; this one has 101.');
    expect(error({ mentions: Array.from({ length: 21 }, (_, i) => 'h' + i).join(' ') })).toBe('A note mentions at most 20 actors; this one names 21.');
  });

  it('accepts the limits themselves', () => {
    expect(toNotePost({ ...form, summary: 'x'.repeat(200), body: 'x'.repeat(20000), resource: 'x'.repeat(100) }, {}).ok).toBe(true);
  });
});
