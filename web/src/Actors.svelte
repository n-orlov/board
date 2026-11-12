<script lang="ts">
  import Toast from './Toast.svelte';
  import { ActorsApi, ActorsStore, actorRows, affectsActors, copyToast, copyWayBackLabel, type ActorRow, type ActorsState } from './lib/actorsView';
  import { copyText } from './lib/clipboard';
  import { live } from './lib/live';
  import { currentBrowser } from './lib/session';

  // The Actors view (PRD R7, SPEC 11): who is on the board. Actors with a live lease come first, each lease linking to
  // its task, then the rest by last seen; every actor with a way back has a button that copies it. The data layer is
  // lib/actorsView.ts.
  let { csrf }: { csrf: string; param?: string } = $props();

  const store = new ActorsStore(new ActorsApi(currentBrowser(), () => csrf), (s) => (view = s));
  let view: ActorsState = $state.raw(store.state);
  let toast = $state('');
  // Ages are phrased at the moment of each draw; a refetch redraws them.
  const rows = $derived(actorRows(view.actors, new Date()));

  $effect(() => {
    void store.load();
    return live().subscribe(affectsActors, () => store.load());
  });

  async function copy(row: ActorRow) {
    toast = copyToast(row.handle, await copyText(navigator.clipboard, row.wayBack ?? ''));
  }

  const action = 'inline-flex min-h-11 min-w-11 items-center justify-center rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text';
  const link = 'inline-flex min-h-11 min-w-11 items-center text-accent hover:underline';
  const cell = 'md:table-cell md:px-3 md:py-3 md:align-top';
  const head = 'px-3 py-2 text-sm font-medium text-text-muted';
</script>

<section aria-labelledby="actors-heading" class="pb-16 md:pb-0" data-testid="actors-view">
  <h2 id="actors-heading" class="mb-4 text-xl font-bold">Actors</h2>
  {#if view.error !== ''}
    <p role="alert" class="mb-4 rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="actors-error">{view.error}</p>
  {/if}
  {#if !view.loaded && view.error === ''}
    <p class="text-sm text-text-muted" data-testid="actors-loading">Loading the actors…</p>
  {:else if view.loaded && rows.length === 0}
    <p class="rounded-md border border-dashed border-border px-4 py-4 text-text-muted" data-testid="actors-empty">No actor has acted on the board yet.</p>
  {:else if rows.length > 0}
    <!-- One table: from 768 px a dense table, below it each actor is a card. -->
    <table class="block w-full text-left wrap-anywhere md:table md:table-fixed md:border-collapse">
      <thead class="sr-only md:table-header-group md:not-sr-only">
        <tr class="md:border-b md:border-border">
          <th scope="col" class="{head} md:w-1/5">Actor</th>
          <th scope="col" class={head}>Live leases</th>
          <th scope="col" class="{head} md:w-32">Last seen</th>
          <th scope="col" class="{head} md:w-1/4">Way back</th>
        </tr>
      </thead>
      <tbody class="flex flex-col gap-3 md:table-row-group">
        {#each rows as row (row.handle)}
          <tr
            data-testid="actor-row"
            data-handle={row.handle}
            data-alive={row.alive}
            class="flex flex-col gap-2 rounded-md border border-border bg-surface-raised p-4 md:table-row md:rounded-none md:border-0 md:border-b md:bg-transparent md:p-0"
          >
            <td class="flex flex-wrap items-center gap-x-2 gap-y-1 {cell}">
              <span class="font-medium" data-testid="actor-handle">{row.handle}</span>
              <span class="text-sm text-text-muted" data-testid="actor-kind">{row.kind}</span>
              {#if row.alive}
                <span class="inline-block rounded-md border border-success px-2 py-0.5 text-xs font-medium text-success" data-testid="actor-alive">alive</span>
              {/if}
              {#if row.openAsks > 0}
                <span class="text-sm text-text-muted" data-testid="actor-asks">{row.openAsks} open {row.openAsks === 1 ? 'ask' : 'asks'}</span>
              {/if}
            </td>
            <td class="empty:hidden {cell}" data-testid="actor-leases">
              {#if row.leases.length > 0}
                <ul class="flex flex-col gap-1">
                  {#each row.leases as lease (lease.ref)}
                    <li class="flex flex-wrap items-center gap-x-2 text-sm" data-testid="actor-lease" data-lease={lease.ref}>
                      <span class="font-mono text-text-muted" data-testid="lease-ref">{lease.ref}</span>
                      <a class={link} href={lease.href} data-testid="lease-task">{lease.task}</a>
                      <span data-testid="lease-intent">{lease.intent}</span>
                      <span class="text-text-muted" data-testid="lease-expires">{lease.expires}</span>
                    </li>
                  {/each}
                </ul>
              {:else}
                <span class="text-sm text-text-muted" data-testid="actor-idle">no live lease</span>
              {/if}
            </td>
            <td class="text-sm text-text-muted {cell}"><span title={row.lastSeenTitle} data-testid="actor-last-seen">{row.lastSeen}</span></td>
            <td class="{cell}">
              {#if row.wayBack !== undefined}
                <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <code class="font-mono text-sm text-text-muted" data-testid="actor-way-back">{row.wayBack}</code>
                  <button type="button" class={action} aria-label={copyWayBackLabel(row.handle)} data-testid="copy-way-back" onclick={() => void copy(row)}>Copy way back</button>
                </div>
              {:else}
                <span class="text-sm text-text-muted" data-testid="actor-no-way-back">no way back on record</span>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</section>

<Toast message={toast} onclear={() => (toast = '')} />
