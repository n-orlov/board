import { describe, expect, it } from 'vitest';
import { initialFocusIndex, keyAction, nextFocusIndex, returnFocusTarget } from './focustrap.ts';

describe('initialFocusIndex', () => {
  it('focuses the first element by default', () => {
    expect(initialFocusIndex(3)).toBe(0);
  });
  it('honours a preferred element that exists', () => {
    expect(initialFocusIndex(3, 2)).toBe(2);
    expect(initialFocusIndex(3, 0)).toBe(0);
  });
  it('falls back to the first element for a preference out of range or not an index', () => {
    expect(initialFocusIndex(3, 3)).toBe(0);
    expect(initialFocusIndex(3, -1)).toBe(0);
    expect(initialFocusIndex(3, 1.5)).toBe(0);
  });
  it('focuses the container (-1) when nothing is focusable', () => {
    expect(initialFocusIndex(0)).toBe(-1);
    expect(initialFocusIndex(0, 0)).toBe(-1);
  });
});

describe('nextFocusIndex', () => {
  it('moves forward and backward inside the list', () => {
    expect(nextFocusIndex(0, 3, false)).toBe(1);
    expect(nextFocusIndex(1, 3, false)).toBe(2);
    expect(nextFocusIndex(2, 3, true)).toBe(1);
    expect(nextFocusIndex(1, 3, true)).toBe(0);
  });
  it('wraps from the last element to the first on Tab', () => {
    expect(nextFocusIndex(2, 3, false)).toBe(0);
    expect(nextFocusIndex(0, 1, false)).toBe(0);
  });
  it('wraps from the first element to the last on Shift+Tab', () => {
    expect(nextFocusIndex(0, 3, true)).toBe(2);
    expect(nextFocusIndex(0, 1, true)).toBe(0);
  });
  it('enters the list from outside: first on Tab, last on Shift+Tab', () => {
    expect(nextFocusIndex(-1, 3, false)).toBe(0);
    expect(nextFocusIndex(-1, 3, true)).toBe(2);
    expect(nextFocusIndex(3, 3, false)).toBe(0);
    expect(nextFocusIndex(3, 3, true)).toBe(2);
  });
  it('stays on the container when nothing is focusable', () => {
    expect(nextFocusIndex(-1, 0, false)).toBe(-1);
    expect(nextFocusIndex(0, 0, true)).toBe(-1);
  });
});

describe('returnFocusTarget', () => {
  const usable = (el: string) => el !== 'gone';
  it('returns to the element that opened the dialog', () => {
    expect(returnFocusTarget('opener', usable, 'body')).toBe('opener');
  });
  it('uses the fallback when the opener is no longer usable', () => {
    expect(returnFocusTarget('gone', usable, 'body')).toBe('body');
  });
  it('uses the fallback when there was no opener', () => {
    expect(returnFocusTarget(null, usable, 'body')).toBe('body');
  });
  it('is null with neither opener nor fallback', () => {
    expect(returnFocusTarget<string>(null, usable, null)).toBeNull();
  });
});

describe('keyAction', () => {
  it('closes on Escape', () => {
    expect(keyAction('Escape', false, 1, 3)).toEqual({ kind: 'close' });
  });
  it('wraps Tab at the last element and Shift+Tab at the first', () => {
    expect(keyAction('Tab', false, 2, 3)).toEqual({ kind: 'focus', index: 0 });
    expect(keyAction('Tab', true, 0, 3)).toEqual({ kind: 'focus', index: 2 });
  });
  it('moves Tab one step inside the list', () => {
    expect(keyAction('Tab', false, 0, 3)).toEqual({ kind: 'focus', index: 1 });
    expect(keyAction('Tab', true, 2, 3)).toEqual({ kind: 'focus', index: 1 });
  });
  it('ignores every other key', () => {
    expect(keyAction('Enter', false, 0, 3)).toEqual({ kind: 'none' });
    expect(keyAction('a', true, 0, 3)).toEqual({ kind: 'none' });
  });
});
