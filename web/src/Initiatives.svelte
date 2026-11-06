<script lang="ts">
  import Dialog from './Dialog.svelte';
  import InitiativeForm from './InitiativeForm.svelte';
  import Toast from './Toast.svelte';
  import { absoluteTime, relativeTime } from './lib/format';
  import { InitiativesApi, InitiativesStore, type InitiativeForm as FormValues, type InitiativesState } from './lib/initiatives';
  import { initiativeTone, tagLabels, toneClasses } from './lib/presentation';
  import { currentBrowser } from './lib/session';

  let { csrf }: { csrf: string } = $props();

  let view: InitiativesState = $state({ initiatives: [], loading: true, error: undefined, formError: undefined });
  let formOpen = $state(false);
  let toast = $state('');

  const store = new InitiativesStore(new InitiativesApi(currentBrowser(), () => csrf), (s) => (view = s));

  $effect(() => {
    void store.load();
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

  const now = new Date();
  const empty = $derived(!view.loading && view.error === undefined && view.initiatives.length === 0);
  const primary = 'min-h-11 rounded-md border border-accent bg-surface-raised px-4 text-sm font-medium text-accent';
  const skeletonBar = 'h-4 animate-pulse rounded-md bg-border';
</script>

<section aria-labelledby="initiatives-heading" class="pb-16 md:pb-0">
  <div class="mb-4 flex items-center justify-between gap-3">
    <h2 id="initiatives-heading" class="text-xl font-bold">Initiatives</h2>
    {#if !empty}
      <button type="button" class="{primary} hidden md:inline-flex md:items-center" onclick={openForm}>New initiative</button>
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
      <p class="text-lg font-medium">No initiatives yet</p>
      <p class="text-sm text-text-muted">An initiative groups the tasks of one piece of work.</p>
      <button type="button" class={primary} onclick={openForm}>New initiative</button>
    </div>
  {:else if view.initiatives.length > 0}
    <!-- One table: from 768 px a dense table, below it each row is a card. -->
    <table class="block w-full text-left wrap-anywhere md:table md:table-fixed md:border-collapse">
      <thead class="sr-only md:table-header-group md:not-sr-only">
        <tr class="md:border-b md:border-border">
          <th scope="col" class="px-3 py-2 text-sm font-medium text-text-muted">Title</th>
          <th scope="col" class="px-3 py-2 text-sm font-medium text-text-muted md:w-24">Status</th>
          <th scope="col" class="px-3 py-2 text-sm font-medium text-text-muted md:w-1/4">Tags</th>
          <th scope="col" class="px-3 py-2 text-sm font-medium text-text-muted md:w-32">Created</th>
        </tr>
      </thead>
      <tbody class="flex flex-col gap-3 md:table-row-group">
        {#each view.initiatives as initiative (initiative.id)}
          <tr
            data-testid="initiative-row"
            data-slug={initiative.slug}
            class="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-border bg-surface-raised p-4 md:table-row md:rounded-none md:border-0 md:border-b md:bg-transparent md:p-0"
          >
            <td class="w-full min-w-0 md:table-cell md:w-auto md:px-3 md:py-3">
              <div class="font-medium">{initiative.title}</div>
              <code class="font-mono text-sm text-text-muted">{initiative.slug}</code>
            </td>
            <td class="md:table-cell md:px-3 md:py-3" data-testid="initiative-status">
              <span class="inline-block rounded-md border px-2 py-0.5 text-xs font-medium {toneClasses(initiativeTone(initiative.status))}">{initiative.status}</span>
            </td>
            <td class="flex min-w-0 flex-wrap gap-1 empty:hidden md:table-cell md:px-3 md:py-3" data-testid="initiative-tags">
              {#each tagLabels(initiative.tags) as tag (tag)}
                <span class="mr-1 inline-block rounded-md border border-border bg-surface px-2 py-0.5 text-xs text-text-muted" data-testid="tag">{tag}</span>
              {/each}
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
