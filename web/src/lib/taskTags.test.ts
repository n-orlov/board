import { describe, expect, it, vi } from 'vitest';
import { InitiativesError } from './initiatives';
import type { TagMark } from './chipFilter';
import {
  MAX_TAGS,
  REFS_PER_READ,
  SUGGESTION_LIMIT,
  addTaskTag,
  chipTitle,
  normalizeTag,
  ownTags,
  planAdd,
  readMarks,
  removable,
  removeLabel,
  removeTaskTag,
  suggestTags,
  tagEditor,
  tagChips,
  tagsInUse,
} from './taskTags';

const marks: TagMark[] = [
  { tag: 'urgent', source: 'task' },
  { tag: 'web', source: 'task' },
  { tag: 'web', source: 'initiative' },
  { tag: 'team', source: 'initiative' },
  { tag: 'claude', source: 'actor' },
];

function writer() {
  return { addTag: vi.fn().mockResolvedValue({}), removeTag: vi.fn().mockResolvedValue({}) };
}

describe('the chips of a task', () => {
  it('is one chip per tag, in the order the marks name them, naming every source', () => {
    expect(tagChips(marks)).toEqual([
      { tag: 'urgent', sources: ['task'] },
      { tag: 'web', sources: ['task', 'initiative'] },
      { tag: 'team', sources: ['initiative'] },
      { tag: 'claude', sources: ['actor'] },
    ]);
  });

  it('has no chip for no marks', () => {
    expect(tagChips([])).toEqual([]);
  });

  it('lets the operator remove a chip only when the tag is one of the task’s own', () => {
    const chips = tagChips(marks);
    expect(chips.map((c) => [c.tag, removable(c)])).toEqual([
      ['urgent', true],
      ['web', true],
      ['team', false],
      ['claude', false],
    ]);
  });

  it('lists the own tags of the marks', () => {
    expect(ownTags(marks)).toEqual(['urgent', 'web']);
  });

  it('names the source in the title and the tag and the task in the remove label', () => {
    expect(chipTitle({ tag: 'web', sources: ['task', 'initiative'] })).toBe('web: task tag, initiative tag');
    expect(removeLabel('web', 'aa-1')).toBe('Remove tag web from aa-1');
  });
});

describe('planning an add', () => {
  it('adds a new valid tag, trimmed and lowercased', () => {
    expect(planAdd(['a'], '  Urgent ')).toEqual({ kind: 'add', tag: 'urgent' });
    expect(normalizeTag(' ABC ')).toBe('abc');
  });

  it('does nothing for a tag the task already has, whatever the letter case', () => {
    expect(planAdd(['urgent'], 'URGENT')).toEqual({ kind: 'duplicate', tag: 'urgent' });
  });

  it.each(['', '   ', '-lead', 'has space', 'x'.repeat(41), 'bad/char', 'ünï'])('refuses %j', (text) => {
    expect(planAdd([], text).kind).toBe('invalid');
  });

  it('accepts the longest tag and every allowed punctuation mark', () => {
    expect(planAdd([], 'a'.repeat(40)).kind).toBe('add');
    expect(planAdd([], 'a.b_c:d-e').kind).toBe('add');
  });

  it('refuses a 21st own tag but still reports a duplicate among 20', () => {
    const own = Array.from({ length: MAX_TAGS }, (_, n) => `t${n}`);
    expect(planAdd(own, 'extra')).toEqual({ kind: 'invalid', message: 'A task carries at most 20 tags.' });
    expect(planAdd(own, 't3')).toEqual({ kind: 'duplicate', tag: 't3' });
  });
});

describe('adding a tag', () => {
  it('sends one request with the normalized tag', async () => {
    const api = writer();
    expect(await addTaskTag(api, 'aa-1', ['x'], ' Work ')).toEqual({ changed: true, error: '' });
    expect(api.addTag).toHaveBeenCalledTimes(1);
    expect(api.addTag).toHaveBeenCalledWith('aa-1', 'work');
  });

  it('sends no request for a duplicate', async () => {
    const api = writer();
    expect(await addTaskTag(api, 'aa-1', ['work'], 'work')).toEqual({ changed: false, error: '' });
    expect(api.addTag).not.toHaveBeenCalled();
  });

  it('sends no request for an invalid tag and says why', async () => {
    const api = writer();
    const out = await addTaskTag(api, 'aa-1', [], 'not valid!');
    expect(out.changed).toBe(false);
    expect(out.error).toContain('lowercase letters');
    expect(api.addTag).not.toHaveBeenCalled();
  });

  it('shows the refusal of the server and changes nothing', async () => {
    const api = writer();
    api.addTag.mockRejectedValue(new InitiativesError(409, 'Task aa-1 is closed.'));
    expect(await addTaskTag(api, 'aa-1', [], 'work')).toEqual({ changed: false, error: 'Task aa-1 is closed.' });
  });
});

describe('removing a tag', () => {
  it('sends one request for a tag among the task’s own', async () => {
    const api = writer();
    expect(await removeTaskTag(api, 'aa-1', marks, 'urgent')).toEqual({ changed: true, error: '' });
    expect(api.removeTag).toHaveBeenCalledTimes(1);
    expect(api.removeTag).toHaveBeenCalledWith('aa-1', 'urgent');
  });

  it('sends nothing for an initiative tag or a derived one', async () => {
    const api = writer();
    expect(await removeTaskTag(api, 'aa-1', marks, 'team')).toEqual({ changed: false, error: '' });
    expect(await removeTaskTag(api, 'aa-1', marks, 'claude')).toEqual({ changed: false, error: '' });
    expect(api.removeTag).not.toHaveBeenCalled();
  });

  it('shows the refusal of the server', async () => {
    const api = writer();
    api.removeTag.mockRejectedValue(new InitiativesError(0, 'The server is not reachable.'));
    expect(await removeTaskTag(api, 'aa-1', marks, 'web')).toEqual({ changed: false, error: 'The server is not reachable.' });
  });
});

describe('suggestions', () => {
  const inUse = ['work', 'home', 'homework', 'urgent', 'overwork', 'web'];

  it('offers nothing for blank text', () => {
    expect(suggestTags(inUse, [], '')).toEqual([]);
    expect(suggestTags(inUse, [], '  ')).toEqual([]);
  });

  it('keeps the tags that contain the text, those that start with it first, each group alphabetical', () => {
    expect(suggestTags(inUse, [], 'wor')).toEqual(['work', 'homework', 'overwork']);
    expect(suggestTags(inUse, [], 'ho')).toEqual(['home', 'homework']);
    expect(suggestTags(inUse, [], 'k')).toEqual(['homework', 'overwork', 'work']);
  });

  it('matches in any letter case', () => {
    expect(suggestTags(inUse, [], 'URG')).toEqual(['urgent']);
  });

  it('leaves out the tags the task already has and repeats', () => {
    expect(suggestTags(['work', 'work', 'web'], ['work'], 'w')).toEqual(['web']);
  });

  it('offers nothing when no tag matches', () => {
    expect(suggestTags(inUse, [], 'zzz')).toEqual([]);
  });

  it('shows at most the limit', () => {
    const many = Array.from({ length: 20 }, (_, n) => `tag${String(n).padStart(2, '0')}`);
    expect(suggestTags(many, [], 'tag')).toHaveLength(SUGGESTION_LIMIT);
    expect(suggestTags(many, [], 'tag', 3)).toEqual(['tag00', 'tag01', 'tag02']);
  });

  it('lists the tags in use once, sorted, from every list of marks', () => {
    expect(tagsInUse(marks, [{ tag: 'alpha', source: 'task' }, { tag: 'web', source: 'actor' }])).toEqual(['alpha', 'claude', 'team', 'urgent', 'web']);
  });
});

describe('reading the marks of some tasks', () => {
  it('asks for every ref of the status all and keys the marks by ref', async () => {
    const page = vi.fn().mockResolvedValue({ items: [{ ref: 'aa-1', tag_marks: [{ tag: 'x', source: 'task' }] }, { ref: 'bb-2', tag_marks: [] }] });
    const got = await readMarks(page, ['aa-1', 'bb-2', 'aa-1']);
    expect(got).toEqual({ 'aa-1': [{ tag: 'x', source: 'task' }], 'bb-2': [] });
    expect(page).toHaveBeenCalledTimes(1);
    const query: URLSearchParams = page.mock.calls[0][0];
    expect(query.getAll('ref')).toEqual(['aa-1', 'bb-2']);
    expect(query.get('status')).toBe('all');
  });

  it('reads a long list in chunks', async () => {
    const refs = Array.from({ length: REFS_PER_READ + 5 }, (_, n) => `aa-${n + 1}`);
    const page = vi.fn().mockResolvedValue({ items: [] });
    await readMarks(page, refs);
    expect(page).toHaveBeenCalledTimes(2);
    expect(page.mock.calls[0][0].getAll('ref')).toHaveLength(REFS_PER_READ);
    expect(page.mock.calls[1][0].getAll('ref')).toHaveLength(5);
  });

  it('makes no request for no refs', async () => {
    const page = vi.fn();
    expect(await readMarks(page, [])).toEqual({});
    expect(page).not.toHaveBeenCalled();
  });
});

describe('the editor of a task', () => {
  it('reads the view again after an add that changed the task, and answers no error', async () => {
    const api = writer();
    const changed = vi.fn();
    const editor = tagEditor(api, 'aa-1', marks, ['web'], changed);
    expect(editor.inUse).toEqual(['web']);
    expect(await editor.onadd('fresh')).toBe('');
    expect(api.addTag).toHaveBeenCalledWith('aa-1', 'fresh');
    expect(changed).toHaveBeenCalledTimes(1);
  });

  it('neither writes nor reads again for a duplicate add, and answers the reason for an invalid one', async () => {
    const api = writer();
    const changed = vi.fn();
    const editor = tagEditor(api, 'aa-1', marks, [], changed);
    expect(await editor.onadd('urgent')).toBe('');
    expect(await editor.onadd('Bad Tag')).toContain('lowercase letters');
    expect(api.addTag).not.toHaveBeenCalled();
    expect(changed).not.toHaveBeenCalled();
  });

  it('removes an own tag and reads again, and does not touch an initiative tag', async () => {
    const api = writer();
    const changed = vi.fn();
    const editor = tagEditor(api, 'aa-1', marks, [], changed);
    expect(await editor.onremove('urgent')).toBe('');
    expect(await editor.onremove('team')).toBe('');
    expect(api.removeTag).toHaveBeenCalledTimes(1);
    expect(changed).toHaveBeenCalledTimes(1);
  });

  it('answers the refusal of the server and does not read again', async () => {
    const api = writer();
    api.addTag.mockRejectedValue(new InitiativesError(409, 'closed'));
    const changed = vi.fn();
    expect(await tagEditor(api, 'aa-1', marks, [], changed).onadd('x')).toBe('closed');
    expect(changed).not.toHaveBeenCalled();
  });
});
