// The focus rules of the one dialog/sheet component, as pure functions over the position of the
// focused element in the dialog's list of focusable elements (-1 = none of them, e.g. the dialog
// container itself or an element outside it). The component maps positions to elements.

/** What a key press inside the dialog should do. */
export type KeyAction = { kind: 'close' } | { kind: 'focus'; index: number } | { kind: 'none' };

/** Position to focus when the dialog opens: the preferred one if it exists, else the first, else -1 (the container). */
export function initialFocusIndex(count: number, preferred?: number): number {
  if (count <= 0) return -1;
  if (preferred !== undefined && Number.isInteger(preferred) && preferred >= 0 && preferred < count) {
    return preferred;
  }
  return 0;
}

/** Position Tab (or Shift+Tab when `backwards`) moves to, wrapping at both ends. -1 when nothing is focusable. */
export function nextFocusIndex(current: number, count: number, backwards: boolean): number {
  if (count <= 0) return -1;
  const inside = current >= 0 && current < count;
  if (backwards) return inside ? (current === 0 ? count - 1 : current - 1) : count - 1;
  return inside ? (current === count - 1 ? 0 : current + 1) : 0;
}

/**
 * Element focus returns to when the dialog closes: the element that had focus when it opened if it
 * is still usable, otherwise the fallback (null when there is none).
 */
export function returnFocusTarget<T>(opener: T | null, isUsable: (el: T) => boolean, fallback: T | null): T | null {
  return opener !== null && isUsable(opener) ? opener : fallback;
}

/** The action for a key pressed while focus sits at `current` of `count` focusable elements. */
export function keyAction(key: string, shiftKey: boolean, current: number, count: number): KeyAction {
  if (key === 'Escape') return { kind: 'close' };
  if (key === 'Tab') return { kind: 'focus', index: nextFocusIndex(current, count, shiftKey) };
  return { kind: 'none' };
}
