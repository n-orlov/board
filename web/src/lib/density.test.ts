import { describe, expect, it } from 'vitest';
import { createDensityController, DENSITY_STORAGE_KEY, parseDensity, type DensityEnv } from './density.ts';

function fakeEnv(stored?: string, failing = false) {
  const data = new Map<string, string>();
  if (stored !== undefined) data.set(DENSITY_STORAGE_KEY, stored);
  const attrs = new Map<string, string>();
  const env: DensityEnv = {
    storage: {
      getItem: (key) => {
        if (failing) throw new Error('blocked');
        return data.get(key) ?? null;
      },
      setItem: (key, value) => {
        if (failing) throw new Error('blocked');
        data.set(key, value);
      },
    },
    root: { setAttribute: (name, value) => void attrs.set(name, value) },
  };
  return { env, data, attrs };
}

describe('density preference', () => {
  it('uses a versioned vh. key, never the prototype one', () => {
    expect(DENSITY_STORAGE_KEY).toMatch(/^vh\.[a-z]+\.v\d+$/);
    expect(DENSITY_STORAGE_KEY.startsWith('vh.proto.')).toBe(false);
  });
  it('defaults to compact and applies it', () => {
    const { env, attrs } = fakeEnv();
    expect(createDensityController(env).density()).toBe('compact');
    expect(attrs.get('data-density')).toBe('compact');
  });
  it('persists a choice, applies it and reads it back on the next load', () => {
    const { env, data, attrs } = fakeEnv();
    createDensityController(env).set('sparse');
    expect(data.get(DENSITY_STORAGE_KEY)).toBe('sparse');
    expect(attrs.get('data-density')).toBe('sparse');
    const again = fakeEnv(data.get(DENSITY_STORAGE_KEY));
    expect(createDensityController(again.env).density()).toBe('sparse');
    expect(again.attrs.get('data-density')).toBe('sparse');
  });
  it('ignores a bad stored value', () => {
    for (const raw of ['dense', '', 'Sparse', '{"a":1}', 'null']) {
      const { env, attrs } = fakeEnv(raw);
      expect(createDensityController(env).density()).toBe('compact');
      expect(attrs.get('data-density')).toBe('compact');
      expect(parseDensity(raw)).toBe('compact');
    }
    expect(parseDensity(null)).toBe('compact');
    expect(parseDensity(undefined)).toBe('compact');
  });
  it('still works for this page view when storage is blocked', () => {
    const { env, attrs } = fakeEnv(undefined, true);
    const controller = createDensityController(env);
    expect(controller.density()).toBe('compact');
    controller.set('sparse');
    expect(controller.density()).toBe('sparse');
    expect(attrs.get('data-density')).toBe('sparse');
  });
});
