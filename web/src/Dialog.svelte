<script lang="ts">
  import type { Snippet } from 'svelte';
  import { onMount } from 'svelte';
  import { initialFocusIndex, keyAction, returnFocusTarget } from './lib/focustrap';

  // The one dialog/sheet: mounted while open (the parent wraps it in {#if}). Below 768 px it is a
  // bottom sheet, from 768 px up a centred dialog. The focus rules live in lib/focustrap.ts.
  let {
    label,
    onclose,
    initialFocus,
    wide = false,
    children,
  }: {
    label: string;
    onclose: () => void;
    /** CSS selector of the element to focus first; the first focusable element when absent or unmatched. */
    initialFocus?: string;
    /** From 768 px a wider dialog (a whole task to read) than the forms' one. */
    wide?: boolean;
    children: Snippet;
  } = $props();

  const FOCUSABLE =
    'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

  let panel: HTMLElement;
  // The element focused when the dialog opened is where focus goes back to on close.
  const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;

  const focusables = () =>
    [...panel.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => el.getClientRects().length > 0);

  function onkeydown(event: KeyboardEvent) {
    const items = focusables();
    const action = keyAction(event.key, event.shiftKey, items.indexOf(document.activeElement as HTMLElement), items.length);
    if (action.kind === 'close') {
      event.preventDefault();
      onclose();
    } else if (action.kind === 'focus') {
      event.preventDefault();
      (action.index < 0 ? panel : items[action.index]).focus();
    }
  }

  onMount(() => {
    const items = focusables();
    const wanted = initialFocus === undefined ? undefined : items.findIndex((el) => el.matches(initialFocus));
    const index = initialFocusIndex(items.length, wanted);
    (index < 0 ? panel : items[index]).focus();

    // The page behind does not scroll: hidden overflow on both roots (Safari scrolls the html one), and a touch drag
    // outside the panel is cancelled, since iOS scrolls the page under a fixed overlay regardless. The panel's own
    // scroll does not chain to the page (overscroll-contain).
    const overflow = [document.documentElement.style.overflow, document.body.style.overflow];
    document.documentElement.style.overflow = 'hidden';
    document.body.style.overflow = 'hidden';
    return () => {
      [document.documentElement.style.overflow, document.body.style.overflow] = overflow;
      returnFocusTarget<HTMLElement>(opener, (el) => el.isConnected, null)?.focus();
    };
  });

  function blockOutside(node: HTMLElement) {
    const ontouchmove = (event: TouchEvent) => {
      if (!(event.target instanceof Node) || !panel.contains(event.target)) event.preventDefault();
    };
    node.addEventListener('touchmove', ontouchmove, { passive: false });
    return { destroy: () => node.removeEventListener('touchmove', ontouchmove) };
  }
</script>

<svelte:window {onkeydown} />

<div class="fixed inset-0 z-50 flex items-end justify-center md:items-center" use:blockOutside>
  <button
    type="button"
    class="dialog-backdrop absolute inset-0 cursor-default bg-text/50"
    tabindex="-1"
    aria-hidden="true"
    data-testid="dialog-backdrop"
    onclick={onclose}
  ></button>
  <div
    bind:this={panel}
    class="dialog-panel relative max-h-[85dvh] w-full overflow-y-auto overscroll-contain rounded-t-md border border-border bg-surface-raised p-4 text-text shadow-lg md:rounded-md {wide ? 'md:max-w-2xl' : 'md:max-w-lg'}"
    role="dialog"
    aria-modal="true"
    aria-label={label}
    tabindex="-1"
    data-testid="dialog"
  >
    {@render children()}
  </div>
</div>
