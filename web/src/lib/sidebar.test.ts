import { describe, expect, it } from 'vitest';
import { createSidebarController, parseCollapsed, SIDEBAR_STORAGE_KEY, type SidebarEnv } from './sidebar.ts';

function fakeEnv(stored?: string, failing = false) {
  const data = new Map<string, string>();
  if (stored !== undefined) data.set(SIDEBAR_STORAGE_KEY, stored);
  const env: SidebarEnv = {
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
  };
  return { env, data };
}

describe('sidebar-collapsed preference', () => {
  it('uses a versioned vh. key, never the prototype one', () => {
    expect(SIDEBAR_STORAGE_KEY).toMatch(/^vh\.[a-z]+\.v\d+$/);
    expect(SIDEBAR_STORAGE_KEY.startsWith('vh.proto.')).toBe(false);
  });
  it('defaults to expanded', () => {
    expect(createSidebarController(fakeEnv().env).collapsed()).toBe(false);
  });
  it('persists a choice and reads it back on the next load', () => {
    const { env, data } = fakeEnv();
    const controller = createSidebarController(env);
    controller.set(true);
    expect(controller.collapsed()).toBe(true);
    expect(createSidebarController(fakeEnv(data.get(SIDEBAR_STORAGE_KEY)).env).collapsed()).toBe(true);
    controller.set(false);
    expect(createSidebarController(fakeEnv(data.get(SIDEBAR_STORAGE_KEY)).env).collapsed()).toBe(false);
  });
  it('ignores a bad stored value', () => {
    for (const raw of ['true', '1', '', 'Collapsed', '{"a":1}', 'null']) {
      expect(createSidebarController(fakeEnv(raw).env).collapsed()).toBe(false);
      expect(parseCollapsed(raw)).toBe(false);
    }
    expect(parseCollapsed(null)).toBe(false);
    expect(parseCollapsed(undefined)).toBe(false);
  });
  it('still works for this page view when storage is blocked', () => {
    const controller = createSidebarController(fakeEnv(undefined, true).env);
    expect(controller.collapsed()).toBe(false);
    controller.set(true);
    expect(controller.collapsed()).toBe(true);
  });
});
