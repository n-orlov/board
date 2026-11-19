<script lang="ts">
  // UX prototype: one sort control for every list, in the style of the Tasks sort select, shown at every width.
  let { options, value, onchange, label = 'Sort' }: { options: readonly { value: string; label: string }[]; value: string; onchange: (v: string) => void; label?: string } = $props();
</script>

<label class="flex items-center gap-2 text-sm text-text-muted" data-testid="proto-sort">
  <span>{label}</span>
  <select
    class="min-h-11 rounded-md border border-border bg-surface-raised px-2 text-sm text-text md:min-h-8"
    {value}
    onchange={(e) => onchange(e.currentTarget.value)}
  >
    {#each options as o (o.value)}
      <option value={o.value}>{o.label}</option>
    {/each}
  </select>
</label>
