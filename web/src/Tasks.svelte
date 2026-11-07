<script lang="ts">
  import { taskHref } from './lib/taskPage';
  import FilterSelect from './FilterSelect.svelte';
  import NewTaskDialog from './NewTaskDialog.svelte';
  import NowPanel from './NowPanel.svelte';
  import Toast from './Toast.svelte';
  import { InitiativesApi } from './lib/initiatives';
  import { taskRows } from './lib/initiativePage';
  import { toneClasses } from './lib/presentation';
  import { currentBrowser } from './lib/session';
  import {
    DEFAULT_TASKS_FILTER,
    KIND_OPTIONS,
    STATUS_OPTIONS,
    filterFromHash,
    flagOptions,
    initiativeOptions,
    isFiltered,
    tasksHash,
    type FlagName,
    type TasksFilter,
  } from './lib/taskFilters';
  import { TasksApi, TasksStore, type TasksState } from './lib/tasks';

  let { csrf }: { csrf: string; param?: string } = $props();

  const browser = currentBrowser();
  const initial = filterFromHash(window.location.hash);
  let view: TasksState = $state({ tasks: [], filter: initial, loading: true, error: undefined });
  let initiatives: string[] = $state([]);
  let formOpen = $state(false);
  let toast = $state('');

  const store = new TasksStore(new TasksApi(browser, () => csrf), (s) => (view = s), initial);

  $effect(() => {
    void store.load();
    void new InitiativesApi(browser, () => csrf)
      .list('all')
      .then((list) => (initiatives = list.map((i) => i.slug)))
      .catch(() => undefined); // the select then offers only the initiative the URL names; the list shows the error
  });

  // The filter lives in the fragment: a change rewrites it in place (no history entry per choice) and fetches.
  function apply(filter: TasksFilter) {
    window.history.replaceState(null, '', tasksHash(filter));
    void store.setFilter(filter);
  }

  // The fragment edited by hand, or the back button after a link with another query.
  function hashChanged() {
    const filter = filterFromHash(window.location.hash);
    if (tasksHash(filter) !== tasksHash(view.filter)) void store.setFilter(filter);
  }

  const setFlag = (name: FlagName, value: string) => apply({ ...view.filter, [name]: value });
  const rows = $derived(taskRows(view.tasks, 'all'));
  const empty = $derived(!view.loading && view.error === undefined && rows.length === 0);

  function created(ref: string) {
    formOpen = false;
    toast = `Task ${ref} created`;
    void store.load();
  }

  const primary = 'min-h-11 rounded-md border border-accent bg-surface-raised px-4 text-sm font-medium text-accent';
  const badge = 'inline-block rounded-md border px-2 py-0.5 text-xs font-medium';
  const cell = 'md:table-cell md:px-3 md:py-3';
  const head = 'px-3 py-2 text-sm font-medium text-text-muted';
</script>

<svelte:window onhashchange={hashChanged} />

<section aria-labelledby="tasks-heading" class="pb-16 md:pb-0" data-testid="tasks-view">
  <div class="mb-4 flex items-center justify-between gap-3">
    <h2 id="tasks-heading" class="text-xl font-bold">Tasks</h2>
    <button type="button" class="{primary} hidden md:inline-flex md:items-center" onclick={() => (formOpen = true)}>New task</button>
  </div>

  <NowPanel {csrf} />

  <div class="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2" data-testid="task-filters">
    <FilterSelect label="Status" options={STATUS_OPTIONS} value={view.filter.status} onchange={(v) => apply({ ...view.filter, status: v })} testid="status-filter" />
    <FilterSelect label="Kind" options={KIND_OPTIONS} value={view.filter.kind} onchange={(v) => apply({ ...view.filter, kind: v })} testid="kind-filter" />
    <FilterSelect label="Initiative" options={initiativeOptions(initiatives, view.filter.initiative)} value={view.filter.initiative} onchange={(v) => apply({ ...view.filter, initiative: v })} testid="initiative-filter" />
    <FilterSelect label="Active" options={flagOptions('active')} value={view.filter.active} onchange={(v) => setFlag('active', v)} testid="active-filter" />
    <FilterSelect label="Blocked" options={flagOptions('blocked')} value={view.filter.blocked} onchange={(v) => setFlag('blocked', v)} testid="blocked-filter" />
    <FilterSelect label="Overdue" options={flagOptions('overdue')} value={view.filter.overdue} onchange={(v) => setFlag('overdue', v)} testid="overdue-filter" />
    <label class="flex items-center gap-2 text-sm text-text-muted">
      <span>Search</span>
      <input
        type="search"
        class="min-h-11 w-56 max-w-full rounded-md border border-border bg-surface-raised px-3 text-base text-text"
        data-testid="text-filter"
        value={view.filter.text}
        onchange={(e) => apply({ ...view.filter, text: e.currentTarget.value.trim() })}
      />
    </label>
    {#if isFiltered(view.filter)}
      <button type="button" class="min-h-11 rounded-md border border-border px-4 text-sm font-medium text-text" data-testid="clear-filters" onclick={() => apply(DEFAULT_TASKS_FILTER)}>Clear filters</button>
    {/if}
  </div>

  {#if view.error !== undefined}
    <p role="alert" class="mb-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">{view.error}</p>
  {/if}

  {#if view.loading}
    <div aria-busy="true" class="flex flex-col gap-3" data-testid="tasks-skeleton">
      <span class="sr-only">Loading tasks…</span>
      {#each [0, 1, 2, 3] as n (n)}
        <div class="flex flex-col gap-2 rounded-md border border-border bg-surface-raised p-4">
          <div class="h-4 w-2/3 animate-pulse rounded-md bg-border"></div>
          <div class="h-4 w-1/3 animate-pulse rounded-md bg-border"></div>
        </div>
      {/each}
    </div>
  {:else if empty}
    <p class="rounded-md border border-dashed border-border px-4 py-8 text-center text-text-muted" data-testid="no-tasks">
      {isFiltered(view.filter) ? 'No tasks match these filters.' : 'No tasks yet.'}
    </p>
  {:else if rows.length > 0}
    <!-- One table: from 768 px a dense table, below it each row is a card. -->
    <table class="block w-full text-left wrap-anywhere md:table md:table-fixed md:border-collapse">
      <thead class="sr-only md:table-header-group md:not-sr-only">
        <tr class="md:border-b md:border-border">
          <th scope="col" class="{head} md:w-32">Ref</th>
          <th scope="col" class={head}>Title</th>
          <th scope="col" class="{head} md:w-32">Initiative</th>
          <th scope="col" class="{head} md:w-20">Kind</th>
          <th scope="col" class="{head} md:w-24">Status</th>
          <th scope="col" class="{head} md:w-1/6">Flags</th>
          <th scope="col" class="{head} md:w-28">Lease holder</th>
        </tr>
      </thead>
      <tbody class="flex flex-col gap-3 md:table-row-group">
        {#each rows as row (row.id)}
          <tr
            data-testid="task-row"
            data-ref={row.ref}
            class="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-border bg-surface-raised p-4 md:table-row md:rounded-none md:border-0 md:border-b md:bg-transparent md:p-0"
          >
            <td class="font-mono text-sm {cell}" data-testid="task-ref"><a class="inline-flex min-h-11 items-center text-accent hover:underline" href={taskHref(row.ref)}>{row.ref}</a></td>
            <td class="order-last w-full min-w-0 font-medium md:order-none {cell}" data-testid="task-title">{row.title}</td>
            <td class="text-sm {cell}"><a class="inline-flex min-h-11 items-center text-accent hover:underline" href="#/i/{row.initiative}" data-testid="task-initiative">{row.initiative}</a></td>
            <td class="text-sm {cell}" data-testid="task-kind">{row.kind}</td>
            <td class={cell} data-testid="task-status"><span class="{badge} border-border text-text-muted">{row.status}</span></td>
            <td class="flex flex-wrap gap-1 empty:hidden {cell}" data-testid="task-flags">
              {#each row.badges as flag (flag.key)}
                <span class="{badge} {toneClasses(flag.tone)}" data-testid="flag-{flag.key}">{flag.label}</span>
              {/each}
            </td>
            <td class="text-sm empty:hidden {cell}" data-testid="task-holder">{row.holder}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}

  <button
    type="button"
    class="{primary} fixed inset-x-4 bottom-[calc(3.5rem+env(safe-area-inset-bottom)+0.75rem)] z-20 shadow-lg md:hidden"
    onclick={() => (formOpen = true)}>New task</button
  >
</section>

<Toast message={toast} onclear={() => (toast = '')} />

{#if formOpen}
  <NewTaskDialog {csrf} initiative="" onclose={() => (formOpen = false)} oncreated={created} />
{/if}
