<script lang="ts">
  import { optionValue, pickOption, sortOptions, type SortColumn } from './lib/sortControl';
  import type { SortState } from './lib/tableSort';

  // The shared sort control (lib/sortControl.ts): one select at every width, with the default order and each column
  // ascending and descending. 44 px high and 16 px text on a phone. A page passes its columns and its sort.
  let {
    columns,
    sort,
    onsort,
    testid = 'sort-select',
  }: { columns: readonly SortColumn[]; sort: SortState; onsort: (next: SortState) => void; testid?: string } = $props();

  const options = $derived(sortOptions(columns));
</script>

<label class="flex max-w-full items-center gap-2 text-sm text-text-muted">
  <span>Sort</span>
  <select
    class="min-h-11 min-w-0 max-w-full rounded-md border border-border bg-surface-raised px-3 text-base text-text"
    data-testid={testid}
    value={optionValue(sort)}
    onchange={(event) => onsort(pickOption(event.currentTarget.value, columns))}
  >
    {#each options as option (option.value)}
      <option value={option.value} selected={option.value === optionValue(sort)}>{option.label}</option>
    {/each}
  </select>
</label>
