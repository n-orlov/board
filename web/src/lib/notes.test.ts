import { describe, expect, it } from 'vitest';
import {
  DEFAULT_NOTES_FILTER,
  apiQuery,
  filterFromHash,
  filterFromQuery,
  filterToQuery,
  isFiltered,
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
