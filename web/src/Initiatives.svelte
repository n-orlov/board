<script lang="ts">
  import { replaceHash } from './lib/historyEntry';
  import { untrack } from 'svelte';
  import Dialog from './Dialog.svelte';
  import FilterSelect from './FilterSelect.svelte';
  import InitiativeForm from './InitiativeForm.svelte';
  import Toast from './Toast.svelte';
  import { absoluteTime, relativeTime } from './lib/format';
  import { InitiativesApi, InitiativesStore, affectsInitiatives, attentionChip, orderInitiatives, type InitiativeForm as FormValues, type InitiativesState } from './lib/initiatives';
  import { countsText, leasesText } from './lib/counts';
  import { DEFAULT_INITIATIVE_FILTER, INITIATIVE_FILTERS, emptyInitiativesText, type InitiativeFilter } from './lib/initiativeFilters';
  import { initiativeTone, tagLabels, toneClasses } from './lib/presentation';
  import { live } from './lib/live';
  import { currentBrowser } from './lib/session';
  import SortHeader from './SortHeader.svelte';
  import SortSelect from './SortSelect.svelte';
  import { sortRows, type SortState } from './lib/tableSort';
  import { INITIATIVES_VIEW, INITIATIVE_COLUMNS, INITIATIVE_SORT_LABELS, isCustomised, viewFromQuery, viewHash, viewToQuery, type InitiativesViewState } from './lib/initiativesView';
  import { clearViewQuery, currentViewEnv, openView, saveViewQuery } from './lib/viewState';

  let { csrf }: { csrf: string } = $props();

  const viewEnv = currentViewEnv();
  const queryOf = (hash: string) => (hash.includes('?') ? hash.slice(hash.indexOf('?') + 1) : '');
  // A fragment with a query wins and is saved; a bare one restores the saved query (in place, before the first fetch).
  const opened = viewFromQuery(openView(viewEnv, INITIATIVES_VIEW, '#/initiatives', queryOf(window.location.hash)));
  let sort: SortState = $state(opened.sort);
  let view: InitiativesState = $state({ initiatives: [], filter: opened.filter, loading: true, error: undefined, formError: undefined });
  let formOpen = $state(false);
  let toast = $state('');

  const store = new InitiativesStore(new InitiativesApi(currentBrowser(), () => csrf), (s) => (view = s));

  $effect(() => {
    // The store starts at the default filter: a restored one is set with its single fetch. Nothing reactive is read here.
    void untrack(() => (opened.filter === DEFAULT_INITIATIVE_FILTER ? store.load() : store.setFilter(opened.filter)));
    // A change elsewhere refreshes the rows in place; a new fetch of the filter shown follows the store's own filter.
    return live().subscribe(affectsInitiatives, () => store.refresh());
  });

  function openForm() {
    store.clearFormError();
    formOpen = true;
  }

  async function create(values: FormValues) {
    if (await store.create(values)) {
      formOpen = false;
      toast = `Initiative ${values.slug.trim()} created`;
    }
  }

  // The filter and the sort live in the fragment and in the saved query: a change rewrites it in place (no history entry per choice).
  function persist(state: InitiativesViewState) {
    replaceHash(viewHash(state));
    saveViewQuery(viewEnv.storage, INITIATIVES_VIEW, viewToQuery(state));
  }

  function filterChanged(filter: InitiativeFilter) {
    persist({ filter, sort });
    void store.setFilter(filter);
  }

  // The sort only reorders the rows the view holds: no request.
  function setSort(next: SortState) {
    sort = next;
    persist({ filter: view.filter, sort });
  }

  // Clear filters: the defaults, and nothing saved.
  function clear() {
    sort = null;
    clearViewQuery(viewEnv.storage, INITIATIVES_VIEW);
    replaceHash('#/initiatives');
    void store.setFilter(DEFAULT_INITIATIVE_FILTER);
  }

  // The fragment edited by hand, or the back button after a link with another query. A bare '#/initiatives' restores the saved query.
  function hashChanged() {
    const hash = window.location.hash;
    if (hash !== '#/initiatives' && !hash.startsWith('#/initiatives?')) return;
    const next = viewFromQuery(openView(viewEnv, INITIATIVES_VIEW, '#/initiatives', queryOf(hash)));
    sort = next.sort;
    if (next.filter !== view.filter) void store.setFilter(next.filter);
  }

  const now = new Date();
  // The default order puts the initiatives with tasks that need a person first; a chosen sort replaces it.
  const ordered = $derived(sortRows(orderInitiatives(view.initiatives), sort, INITIATIVE_COLUMNS));
  const empty = $derived(!view.loading && view.error === undefined && view.initiatives.length === 0);
  const primary = 'min-h-11 rounded-md border border-accent bg-surface-raised px-4 text-sm font-medium text-accent';
  const skeletonBar = 'h-4 animate-pulse rounded-md bg-border';
</script>

<svelte:window onhashchange={hashChanged} />

<section aria-labelledby="initiatives-heading" class="vh-dense mx-auto w-full max-w-[1280px] pb-16 md:pb-0">
  <div class="mb-4 flex items-center justify-between gap-3">
    <h2 id="initiatives-heading" class="text-xl font-bold">Initiatives</h2>
    {#if !empty}
      <button type="button" class="{primary} hidden md:inline-flex md:items-center" onclick={openForm}>New initiative</button>
    {/if}
  </div>
  <div class="mb-4 flex flex-wrap items-end gap-3">
    <FilterSelect label="Status" options={INITIATIVE_FILTERS} value={view.filter} onchange={filterChanged} testid="initiative-filter" />
    <SortSelect columns={INITIATIVE_SORT_LABELS} {sort} onsort={setSort} />
    {#if isCustomised({ filter: view.filter, sort })}
      <button type="button" class="min-h-11 rounded-md border border-border px-4 text-sm font-medium text-text" data-testid="clear-filters" onclick={clear}>Clear filters</button>
    {/if}
  </div>

  {#if view.error !== undefined}
    <p role="alert" class="mb-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">{view.error}</p>
  {/if}

  {#if view.loading}
    <div aria-busy="true" class="flex flex-col gap-3" data-testid="initiatives-skeleton">
      <span class="sr-only">Loading initiatives…</span>
      {#each [0, 1, 2, 3] as n (n)}
        <div class="flex flex-col gap-2 rounded-md border border-border bg-surface-raised p-4" data-testid="skeleton-row">
          <div class="{skeletonBar} w-2/3"></div>
          <div class="{skeletonBar} w-1/3"></div>
        </div>
      {/each}
    </div>
  {:else if empty}
    <div class="flex flex-col items-center gap-3 rounded-md border border-dashed border-border px-4 py-12 text-center" data-testid="empty-state">
      <p class="text-lg font-medium" data-testid="empty-text">{emptyInitiativesText(view.filter)}</p>
      <p class="text-sm text-text-muted">An initiative groups the tasks of one piece of work.</p>
      <button type="button" class={primary} onclick={openForm}>New initiative</button>
    </div>
  {:else if view.initiatives.length > 0}
    <!-- One table: from 768 px a dense table, below it each row is a card. -->
    <table class="block w-full text-left wrap-anywhere md:table md:table-fixed md:border-collapse">
      <thead class="sr-only md:table-header-group md:not-sr-only">
        <tr class="md:border-b md:border-border">
          <SortHeader column="title" label="Title" {sort} onsort={setSort} class="px-3 py-2 text-sm font-medium text-text-muted" />
          <SortHeader column="status" label="Status" {sort} onsort={setSort} class="px-3 py-2 text-sm font-medium text-text-muted md:w-28" />
          <SortHeader column="tags" label="Tags" {sort} onsort={setSort} class="px-3 py-2 text-sm font-medium text-text-muted md:w-1/5" />
          <SortHeader column="tasks" label="Tasks" {sort} onsort={setSort} class="px-3 py-2 text-sm font-medium text-text-muted md:w-44" />
          <SortHeader column="created" label="Created" {sort} onsort={setSort} class="px-3 py-2 text-sm font-medium text-text-muted md:w-32" />
        </tr>
      </thead>
      <tbody class="flex flex-col gap-3 md:table-row-group">
        {#each ordered as initiative (initiative.id)}
          <tr
            data-testid="initiative-row"
            data-slug={initiative.slug}
            class="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-border bg-surface-raised p-4 md:table-row md:rounded-none md:border-0 md:border-b md:bg-transparent md:p-0"
          >
            <td class="w-full min-w-0 md:table-cell md:w-auto md:px-3 md:py-3">
              <a
                class="inline-flex min-h-11 min-w-11 items-center font-medium text-accent hover:underline"
                href="#/i/{initiative.slug}"
                data-testid="initiative-link">{initiative.title}</a
              >
              <br />
              <code class="font-mono text-sm text-text-muted">{initiative.slug}</code>
              {#if attentionChip(initiative) !== undefined}
                {@const chip = attentionChip(initiative)!}
                <span class="ml-2 inline-block rounded-md border px-2 py-0.5 text-xs font-medium {toneClasses('warning')}" data-testid="initiative-attention"
                  ><span data-testid="initiative-attention-count">{chip.count}</span> {chip.label}</span
                >
              {/if}
            </td>
            <td class="md:table-cell md:px-3 md:py-3" data-testid="initiative-status">
              <span class="inline-block rounded-md border px-2 py-0.5 text-xs font-medium {toneClasses(initiativeTone(initiative.status))}">{initiative.status}</span>
            </td>
            <td class="flex min-w-0 flex-wrap gap-1 empty:hidden md:table-cell md:px-3 md:py-3" data-testid="initiative-tags">
              {#each tagLabels(initiative.tags) as tag (tag)}
                <span class="mr-1 inline-block rounded-md border border-border bg-surface px-2 py-0.5 text-xs text-text-muted" data-testid="tag">{tag}</span>
              {/each}
            </td>
            <td class="w-full text-sm md:table-cell md:w-auto md:px-3 md:py-3" data-testid="initiative-tasks">
              <span data-testid="initiative-counts">{countsText(initiative.counts)}</span>
              <span class="text-text-muted" data-testid="initiative-leases">{leasesText(initiative.live_leases)}</span>
            </td>
            <td class="ml-auto text-sm text-text-muted md:ml-0 md:table-cell md:px-3 md:py-3">
              <time datetime={initiative.created_at} title={absoluteTime(initiative.created_at)}>{relativeTime(initiative.created_at, now)}</time>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}

  {#if !empty}
    <button
      type="button"
      class="{primary} fixed inset-x-4 bottom-[calc(3.5rem+env(safe-area-inset-bottom)+0.75rem)] z-20 shadow-lg md:hidden"
      onclick={openForm}>New initiative</button
    >
  {/if}
</section>

<Toast message={toast} onclear={() => (toast = '')} />

{#if formOpen}
  <Dialog label="New initiative" onclose={() => (formOpen = false)} initialFocus="input[name=slug]">
    <InitiativeForm error={view.formError} oncreate={create} oncancel={() => (formOpen = false)} />
  </Dialog>
{/if}
