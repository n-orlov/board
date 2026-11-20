import { describe, expect, it, vi } from 'vitest';
import { carriedTags } from './tagCarry';

const known = [{ tags: ['work'] }, { tags: ['home'] }];

describe('carriedTags', () => {
  it('probes only the tags no initiative lists, once each, and keeps those a task carries', async () => {
    const probe = vi.fn(async (tag: string) => tag !== 'gone');
    const kept = await carriedTags(['work', 'only-own', 'ralph', 'gone', 'only-own'], known, probe);
    expect([...kept].sort()).toEqual(['only-own', 'ralph']);
    expect(probe.mock.calls.map((c) => c[0]).sort()).toEqual(['gone', 'only-own', 'ralph']);
  });

  it('keeps a tag whose check failed: what cannot be checked is not dropped', async () => {
    const kept = await carriedTags(['flaky'], known, () => Promise.reject(new Error('down')));
    expect([...kept]).toEqual(['flaky']);
  });

  it('asks nothing when every tag is an initiative\'s', async () => {
    const probe = vi.fn(async () => false);
    expect((await carriedTags(['work', 'home'], known, probe)).size).toBe(0);
    expect(probe).not.toHaveBeenCalled();
  });
});
