<script lang="ts" generics="T extends string">
  import type { FilterOption } from './lib/initiativeFilters';

  // A labelled select over a closed set of filter values (lib/initiativeFilters.ts): 44 px high and 16 px text,
  // so the phone neither misses it nor zooms on focus.
  let {
    label,
    options,
    value,
    onchange,
    testid,
  }: { label: string; options: readonly FilterOption<T>[]; value: T; onchange: (value: T) => void; testid: string } = $props();

  function changed(event: Event) {
    onchange((event.currentTarget as HTMLSelectElement).value as T);
  }
</script>

<label class="flex items-center gap-2 text-sm text-text-muted">
  <span>{label}</span>
  <select
    class="min-h-11 rounded-md border border-border bg-surface-raised px-3 text-base text-text"
    data-testid={testid}
    {value}
    onchange={changed}
  >
    {#each options as option (option.value)}
      <option value={option.value} selected={option.value === value}>{option.label}</option>
    {/each}
  </select>
</label>
