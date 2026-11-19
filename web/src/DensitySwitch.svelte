<script lang="ts">
  // UX prototype: compact / sparse, shared by every list page and remembered in this browser.
  import { prefs, setDensity, type Density } from './lib/proto/prefs.svelte';
  const OPTIONS: { value: Density; label: string }[] = [
    { value: 'compact', label: 'Compact' },
    { value: 'sparse', label: 'Sparse' },
  ];
</script>

<div role="group" aria-label="Density" class="inline-flex overflow-hidden rounded-md border border-border text-xs" data-testid="density-switch">
  {#each OPTIONS as o (o.value)}
    <button
      type="button"
      class="px-2 py-1 text-text-muted aria-pressed:bg-accent aria-pressed:font-semibold aria-pressed:text-surface-raised"
      aria-pressed={prefs.density === o.value}
      onclick={() => setDensity(o.value)}>{o.label}</button
    >
  {/each}
</div>
