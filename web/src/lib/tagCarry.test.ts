import { describe, expect, it, vi } from 'vitest';
import type { TagMark } from './chipFilter';
import { carriedTags } from './tagCarry';

const known = [{ tags: ['work'] }, { tags: ['home'] }];
const mark = (tag: string, source: TagMark['source']): TagMark => ({ tag, source });

describe('carriedTags', () => {
  it('probes every tag once and keeps the unlisted ones a task carries, with every mark found', async () => {
    const probe = vi.fn(async (tag: string) => (tag === 'gone' ? [] : tag === 'ralph' ? [mark('ralph', 'actor')] : [mark(tag, 'task')]));
    const { tags, marks } = await carriedTags(['work', 'only-own', 'ralph', 'gone', 'only-own'], known, probe);
    expect([...tags].sort()).toEqual(['only-own', 'ralph']);
    expect(probe.mock.calls.map((c) => c[0]).sort()).toEqual(['gone', 'only-own', 'ralph', 'work']);
    expect(marks).toEqual([mark('work', 'task'), mark('only-own', 'task'), mark('ralph', 'actor')]);
  });

  it('keeps the sources of a tag an initiative lists that a task also carries as its own', async () => {
    const { tags, marks } = await carriedTags(['work'], known, async () => [mark('work', 'task')]);
    expect(tags.size).toBe(0);
    expect(marks).toEqual([mark('work', 'task')]);
  });

  it('keeps a tag whose check failed, with no marks: what cannot be checked is not dropped', async () => {
    const { tags, marks } = await carriedTags(['flaky'], known, () => Promise.reject(new Error('down')));
    expect([...tags]).toEqual(['flaky']);
    expect(marks).toEqual([]);
  });

  it('drops a tag nothing carries and no initiative lists', async () => {
    const { tags, marks } = await carriedTags(['ghost'], known, async () => []);
    expect(tags.size).toBe(0);
    expect(marks).toEqual([]);
  });

  it('asks nothing for no tags', async () => {
    const probe = vi.fn(async () => []);
    expect((await carriedTags([], known, probe)).tags.size).toBe(0);
    expect(probe).not.toHaveBeenCalled();
  });

  it('reads each tag in the server\'s spelling: a padded or upper-case saved tag is probed and listed as the server holds it', async () => {
    const probe = vi.fn(async (tag: string) => [mark(tag, 'task')]);
    const { tags, marks } = await carriedTags(['\u0085only-own\u0085', 'WORK', ' Only-Own'], known, probe);
    expect(probe.mock.calls.map((c) => c[0]).sort()).toEqual(['only-own', 'work']);
    expect([...tags]).toEqual(['only-own']);
    expect(marks).toEqual([mark('only-own', 'task'), mark('work', 'task')]);
  });

  it('drops a tag the server\'s grammar refuses without asking: it can be on no task, and the server would answer 400', async () => {
    const probe = vi.fn(async (tag: string) => [mark(tag, 'task')]);
    const { tags, marks } = await carriedTags(['\ufeffonly-own', 'has space', 'x'.repeat(41), 'fine'], known, probe);
    expect(probe.mock.calls.map((c) => c[0])).toEqual(['fine']);
    expect([...tags]).toEqual(['fine']);
    expect(marks).toEqual([mark('fine', 'task')]);
  });
});
