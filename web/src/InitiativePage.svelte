<script lang="ts">
  import Dialog from './Dialog.svelte';
  import FilterSelect from './FilterSelect.svelte';
  import { absoluteTime, relativeTime } from './lib/format';
  import { countBadges } from './lib/counts';
  import { TASK_FILTERS, type TaskFilter } from './lib/initiativeFilters';
  import { InitiativePageStore, liveLeaseCount, targetsText, taskCounts, taskRows, type InitiativePageState } from './lib/initiativePage';
  import InitiativeConfirm from './InitiativeConfirm.svelte';
  import InitiativeEditForm from './InitiativeEditForm.svelte';
  import Toast from './Toast.svelte';
  import { closedToast, closeBody, closeLabel, notClosedCount, REOPEN_BODY } from './lib/initiativeDialogs';
  import { InitiativesApi, toEditForm, type InitiativeEdit } from './lib/initiatives';
  import { renderMarkdown } from './lib/markdown';
  import { initiativeTone, tagLabels, toneClasses } from './lib/presentation';
  import { currentBrowser } from './lib/session';
  import { leasesText } from './lib/counts';

  let { csrf, param }: { csrf: string; param: string } = $props();

  let view: InitiativePageState = $state({ initiative: undefined, tasks: [], filter: 'not-closed', loading: true, error: undefined, dialogError: undefined });
  let dialog: 'edit' | 'close' | 'reopen' | undefined = $state(undefined);
  let toast = $state('');

  const store = new InitiativePageStore(new InitiativesApi(currentBrowser(), () => csrf), (s) => (view = s));

  $effect(() => {
    void store.load(param);
  });

  function open(which: 'edit' | 'close' | 'reopen') {
    store.clearDialogError();
    dialog = which;
  }

  async function finish(done: Promise<boolean>, message: string) {
    if (await done) {
      dialog = undefined;
      toast = message;
    }
  }

  const save = (form: InitiativeEdit) => finish(store.edit(param, form), 'Initiative saved');
  const closeIt = (dropped: number) => finish(store.close(param, dropped > 0), closedToast(param, dropped));
  const reopenIt = () => finish(store.reopen(param), `Initiative ${param} reopened`);

  const now = new Date();
  const pending = $derived(notClosedCount(view.tasks));
  const rows = $derived(taskRows(view.tasks, view.filter));
  const badge = 'inline-block rounded-md border px-2 py-0.5 text-xs font-medium';
  const cell = 'md:table-cell md:px-3 md:py-3';
  const head = 'px-3 py-2 text-sm font-medium text-text-muted';
  const action = 'min-h-11 rounded-md border border-border bg-surface-raised px-4 text-sm font-medium text-text';
  const back = 'inline-flex min-h-11 items-center text-sm font-medium text-accent hover:underline';
</script>

<section aria-labelledby="initiative-heading" class="pb-16 md:pb-0" data-testid="initiative-page">
  <a class={back} href="#/" data-testid="back-link">← Initiatives</a>

  {#if view.error !== undefined}
    <p role="alert" class="my-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">{view.error}</p>
  {/if}

  {#if view.loading}
    <div aria-busy="true" class="mt-4 flex flex-col gap-3" data-testid="initiative-skeleton">
      <span class="sr-only">Loading initiative…</span>
      <div class="h-6 w-2/3 animate-pulse rounded-md bg-border"></div>
      <div class="h-4 w-1/3 animate-pulse rounded-md bg-border"></div>
    </div>
  {:else if view.initiative !== undefined}
    {@const initiative = view.initiative}
    <header class="mb-4">
      <h2 id="initiative-heading" class="wrap-anywhere text-xl font-bold" data-testid="initiative-title">{initiative.title}</h2>
      <div class="mt-1 flex flex-wrap items-center gap-2">
        <code class="wrap-anywhere font-mono text-sm text-text-muted" data-testid="initiative-slug">{initiative.slug}</code>
        <span class="{badge} {toneClasses(initiativeTone(initiative.status))}" data-testid="initiative-status">{initiative.status}</span>
        {#each tagLabels(initiative.tags) as tag (tag)}
          <span class="wrap-anywhere rounded-md border border-border bg-surface px-2 py-0.5 text-xs text-text-muted" data-testid="tag">{tag}</span>
        {/each}
      </div>
      <p class="mt-2 text-sm text-text-muted">
        {#if targetsText(initiative) !== ''}
          <span data-testid="initiative-targets">Targets: {targetsText(initiative)}</span> ·
        {/if}
        Created <time datetime={initiative.created_at} title={absoluteTime(initiative.created_at)}>{relativeTime(initiative.created_at, now)}</time>
        by {initiative.created_by}
      </p>
      <div class="mt-3 flex flex-wrap gap-2" data-testid="initiative-actions">
        <button type="button" class={action} onclick={() => open('edit')}>Edit</button>
        {#if initiative.status === 'open'}
          <button type="button" class={action} onclick={() => open('close')}>Close</button>
        {:else}
          <button type="button" class={action} onclick={() => open('reopen')}>Reopen</button>
        {/if}
      </div>
    </header>

    {#if initiative.description !== ''}
      <!-- renderMarkdown escapes the source first and writes only its own markup (lib/markdown.ts). -->
      <div class="description mb-4 wrap-anywhere" data-testid="initiative-description">{@html renderMarkdown(initiative.description)}</div>
    {/if}

    <div class="mb-6 flex flex-wrap items-center gap-2" data-testid="initiative-counts">
      {#each countBadges(taskCounts(view.tasks)) as count (count.key)}
        <span class="{badge} {toneClasses(count.tone)}" data-testid="count-{count.key}">{count.count} {count.label}</span>
      {/each}
      <span class="text-sm text-text-muted" data-testid="initiative-leases">{leasesText(liveLeaseCount(view.tasks))}</span>
    </div>

    <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
      <h3 class="text-lg font-bold">Tasks</h3>
      <FilterSelect label="Tasks" options={TASK_FILTERS} value={view.filter} onchange={(f: TaskFilter) => store.setFilter(f)} testid="task-filter" />
    </div>

    {#if rows.length === 0}
      <p class="rounded-md border border-dashed border-border px-4 py-8 text-center text-text-muted" data-testid="no-tasks">No tasks match this filter.</p>
    {:else}
      <!-- One table: from 768 px a dense table, below it each row is a card. -->
      <table class="block w-full text-left wrap-anywhere md:table md:table-fixed md:border-collapse">
        <thead class="sr-only md:table-header-group md:not-sr-only">
          <tr class="md:border-b md:border-border">
            <th scope="col" class="{head} md:w-32">Ref</th>
            <th scope="col" class="{head}">Title</th>
            <th scope="col" class="{head} md:w-20">Kind</th>
            <th scope="col" class="{head} md:w-24">Status</th>
            <th scope="col" class="{head} md:w-1/5">Flags</th>
            <th scope="col" class="{head} md:w-32">Lease holder</th>
          </tr>
        </thead>
        <tbody class="flex flex-col gap-3 md:table-row-group">
          {#each rows as row (row.id)}
            <tr
              data-testid="task-row"
              data-ref={row.ref}
              class="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-border bg-surface-raised p-4 md:table-row md:rounded-none md:border-0 md:border-b md:bg-transparent md:p-0"
            >
              <td class="font-mono text-sm text-text-muted {cell}" data-testid="task-ref">{row.ref}</td>
              <td class="order-last w-full min-w-0 font-medium md:order-none {cell}" data-testid="task-title">{row.title}</td>
              <td class="text-sm {cell}" data-testid="task-kind">{row.kind}</td>
              <td class={cell} data-testid="task-status">
                <span class="{badge} border-border text-text-muted">{row.status}</span>
              </td>
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
  {/if}
</section>

<Toast message={toast} onclear={() => (toast = '')} />

{#if dialog !== undefined && view.initiative !== undefined}
  {@const initiative = view.initiative}
  {#if dialog === 'edit'}
    <Dialog label="Edit initiative" onclose={() => (dialog = undefined)} initialFocus="input[name=title]">
      <InitiativeEditForm initial={toEditForm(initiative)} error={view.dialogError} onsave={save} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if dialog === 'close'}
    <Dialog label="Close initiative" onclose={() => (dialog = undefined)} initialFocus="button[data-cancel]">
      <InitiativeConfirm
        heading="Close initiative"
        body={closeBody(pending)}
        confirm={closeLabel(pending)}
        danger={pending > 0}
        error={view.dialogError}
        onconfirm={() => closeIt(pending)}
        oncancel={() => (dialog = undefined)}
      />
    </Dialog>
  {:else}
    <Dialog label="Reopen initiative" onclose={() => (dialog = undefined)} initialFocus="button[data-cancel]">
      <InitiativeConfirm heading="Reopen initiative" body={REOPEN_BODY} confirm="Reopen initiative" error={view.dialogError} onconfirm={reopenIt} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {/if}
{/if}
