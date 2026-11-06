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
    children,
  }: {
    label: string;
    onclose: () => void;
    /** CSS selector of the element to focus first; the first focusable element when absent or unmatched. */
    initialFocus?: string;
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

    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = overflow;
      returnFocusTarget<HTMLElement>(opener, (el) => el.isConnected, null)?.focus();
    };
  });
</script>

<svelte:window {onkeydown} />

<div class="fixed inset-0 z-50 flex items-end justify-center md:items-center">
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
    class="dialog-panel relative max-h-[85dvh] w-full overflow-y-auto rounded-t-md border border-border bg-surface-raised p-4 text-text shadow-lg md:max-w-lg md:rounded-md"
    role="dialog"
    aria-modal="true"
    aria-label={label}
    tabindex="-1"
    data-testid="dialog"
  >
    {@render children()}
  </div>
</div>
