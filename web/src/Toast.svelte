<script lang="ts">
  // The one polite live region of the page. It is in the DOM before it has anything to say, so a
  // screen reader announces the message when it appears; the message clears itself after a while.
  let { message, onclear, ms = 5000 }: { message: string; onclear: () => void; ms?: number } = $props();

  $effect(() => {
    if (message === '') return;
    const timer = setTimeout(onclear, ms);
    return () => clearTimeout(timer);
  });
</script>

<div
  aria-live="polite"
  data-testid="toast-region"
  class="pointer-events-none fixed inset-x-4 bottom-[calc(8.5rem+env(safe-area-inset-bottom))] z-40 flex justify-center md:inset-x-auto md:bottom-6 md:right-6"
>
  {#if message !== ''}
    <p data-testid="toast" class="rounded-md border border-success bg-surface-raised px-4 py-3 text-sm text-text shadow-lg">{message}</p>
  {/if}
</div>
