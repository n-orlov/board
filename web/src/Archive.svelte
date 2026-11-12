<script lang="ts">
  import Dialog from './Dialog.svelte';
  import FilterSelect from './FilterSelect.svelte';
  import InitiativeConfirm from './InitiativeConfirm.svelte';
  import Toast from './Toast.svelte';
  import {
    ARCHIVE_WINDOWS,
    ArchiveApi,
    ArchiveStore,
    affectsArchive,
    archiveHash,
    archiveRows,
    reopenBody,
    reopenLabel,
    reopenedToast,
    windowFromHash,
    type ArchiveRow,
    type ArchiveState,
    type ArchiveWindow,
  } from './lib/archive';
  import { live } from './lib/live';
  import { hashPath } from './lib/routes';
  import { currentBrowser } from './lib/session';

  // The Archive view (PRD R7, SPEC 11): the closed tasks and closed initiatives of a window, newest closed first, each
  // with a Reopen button that confirms in the shared Dialog. The window lives in the fragment query. The data layer is
  // lib/archive.ts.
  let { csrf }: { csrf: string; param?: string } = $props();

  const store = new ArchiveStore(new ArchiveApi(currentBrowser(), () => csrf), (s) => (view = s), windowFromHash(window.location.hash));
  let view: ArchiveState = $state.raw(store.state);
  let reopening: ArchiveRow | undefined = $state(undefined);
  let toast = $state('');
  // Ages are phrased at the moment of each draw; a refetch redraws them.
  const rows = $derived(archiveRows(view.tasks, view.initiatives, new Date()));

  $effect(() => {
    void store.load();
    return live().subscribe(affectsArchive, () => store.load());
  });

  // A choice rewrites the fragment in place (no history entry per choice) and reads again.
  function choose(next: ArchiveWindow) {
    window.history.replaceState(null, '', archiveHash(next));
    void store.setWindow(next);
  }

  // The fragment edited by hand, or the back button after a link with another window.
  function hashChanged() {
    if (hashPath(window.location.hash) !== '#/archive') return;
    void store.setWindow(windowFromHash(window.location.hash));
  }

  function openReopen(row: ArchiveRow) {
    store.clearDialogError();
    reopening = row;
  }

  async function confirm() {
    const row = reopening;
    if (row !== undefined && (await store.reopen(row))) {
      reopening = undefined;
      toast = reopenedToast(row);
    }
  }

  const action = 'inline-flex min-h-11 min-w-11 items-center justify-center rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text';
  const link = 'inline-flex min-h-11 min-w-11 items-center font-medium text-accent hover:underline';
  const cell = 'md:table-cell md:px-3 md:py-3 md:align-top';
  const head = 'px-3 py-2 text-sm font-medium text-text-muted';
</script>

<svelte:window onhashchange={hashChanged} />

<section aria-labelledby="archive-heading" class="mx-auto w-full max-w-[1280px] pb-16 md:pb-0" data-testid="archive-view">
  <h2 id="archive-heading" class="mb-4 text-xl font-bold">Archive</h2>
  <div class="mb-4">
    <FilterSelect label="Closed within" options={ARCHIVE_WINDOWS} value={view.window} onchange={choose} testid="archive-window" />
  </div>
  {#if view.error !== ''}
    <p role="alert" class="mb-4 rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="archive-error">{view.error}</p>
  {/if}
  {#if !view.loaded && view.error === ''}
    <p class="text-sm text-text-muted" data-testid="archive-loading">Loading the archive…</p>
  {:else if view.loaded && rows.length === 0}
    <p class="rounded-md border border-dashed border-border px-4 py-4 text-text-muted" data-testid="archive-empty">Nothing was closed in this window.</p>
  {:else if rows.length > 0}
    <!-- One table: from 768 px a dense table, below it each closed item is a card. -->
    <table class="block w-full text-left wrap-anywhere md:table md:table-fixed md:border-collapse">
      <thead class="sr-only md:table-header-group md:not-sr-only">
        <tr class="md:border-b md:border-border">
          <th scope="col" class="{head} md:w-2/5">Item</th>
          <th scope="col" class="{head} md:w-32">Kind</th>
          <th scope="col" class="{head} md:w-36">Resolution</th>
          <th scope="col" class="{head} md:w-40">Closed</th>
          <th scope="col" class="{head} md:w-28"><span class="sr-only">Action</span></th>
        </tr>
      </thead>
      <tbody class="flex flex-col gap-3 md:table-row-group">
        {#each rows as row (row.key)}
          <tr
            data-testid="archive-row"
            data-kind={row.kind}
            data-ref={row.ref}
            class="flex flex-col gap-2 rounded-md border border-border bg-surface-raised p-4 md:table-row md:rounded-none md:border-0 md:border-b md:bg-transparent md:p-0"
          >
            <td class={cell}>
              <a class={link} href={row.href} data-testid="archive-link">{row.title}</a>
              <br />
              <code class="font-mono text-sm text-text-muted" data-testid="archive-ref">{row.ref}</code>
            </td>
            <td class="text-sm {cell}" data-testid="archive-kind">{row.kind}</td>
            <td class="text-sm {cell}" data-testid="archive-resolution">{row.resolution}</td>
            <td class="text-sm text-text-muted {cell}">
              <time datetime={row.closedAt} title={row.closedTitle} data-testid="archive-closed">{row.closed}</time>
              {#if row.closedBy !== ''}<span data-testid="archive-closed-by"> by {row.closedBy}</span>{/if}
            </td>
            <td class={cell}>
              <button type="button" class={action} aria-label="Reopen {row.kind} {row.ref}" data-testid="archive-reopen" onclick={() => openReopen(row)}>Reopen</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</section>

{#if reopening !== undefined}
  <Dialog label={reopenLabel(reopening)} onclose={() => (reopening = undefined)} initialFocus="button[data-cancel]">
    <InitiativeConfirm
      heading={reopenLabel(reopening)}
      body={reopenBody(reopening)}
      confirm={reopenLabel(reopening)}
      error={view.dialogError === '' ? undefined : view.dialogError}
      onconfirm={confirm}
      oncancel={() => (reopening = undefined)}
    />
  </Dialog>
{/if}

<Toast message={toast} onclear={() => (toast = '')} />
