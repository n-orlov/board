<script lang="ts">
  import type { Snippet } from 'svelte';

  // The centred card every session state outside the app frame shows: the product name and a body.
  let { title, children }: { title: string; children: Snippet } = $props();
</script>

<main class="flex min-h-dvh items-center justify-center p-4">
  <div class="w-full max-w-sm rounded-lg border border-border bg-surface-raised p-6 text-center shadow-sm">
    <h1 class="mb-4 text-lg font-bold">Village Hall</h1>
    <h2 class="sr-only">{title}</h2>
    {@render children()}
  </div>
</main>
