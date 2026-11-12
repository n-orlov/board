<script lang="ts">
  import { ariaSort, nextSort, type SortState } from './lib/tableSort';

  // A sortable column header (lib/tableSort.ts): a button inside the th, so the keyboard and a screen reader reach
  // it, with `aria-sort` on the th and a mark for the direction (never colour alone).
  let {
    column,
    label,
    sort,
    onsort,
    class: className = '',
  }: { column: string; label: string; sort: SortState; onsort: (next: SortState) => void; class?: string } = $props();

  const state = $derived(ariaSort(sort, column));
</script>

<th scope="col" class={className} aria-sort={state}>
  <button
    type="button"
    class="-mx-1 inline-flex min-h-11 items-center gap-1 rounded px-1 font-medium text-text-muted hover:text-text md:min-h-8"
    data-testid={`sort-${column}`}
    onclick={() => onsort(nextSort(sort, column))}
  >
    {label}
    <span aria-hidden="true" class="w-3 text-xs">{state === 'ascending' ? '▲' : state === 'descending' ? '▼' : ''}</span>
  </button>
</th>
