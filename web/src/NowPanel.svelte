<script lang="ts">
  import { conflictText, nowPanel } from './lib/conflicts';
  import { NowStore, affectsNow, type NowState } from './lib/nowPanel';
  import { live } from './lib/live';
  import { taskHref } from './lib/taskPage';
  import { TasksApi } from './lib/tasks';
  import { currentBrowser } from './lib/session';

  let { csrf }: { csrf: string } = $props();

  let view: NowState = $state({ leases: [], loading: true, error: undefined });
  // On the phone the panel starts folded to its summary line; from 768 px it is always open.
  let open = $state(false);

  const store = new NowStore(new TasksApi(currentBrowser(), () => csrf), (s) => (view = s));

  $effect(() => {
    void store.load();
    // A lease started, ended or expired elsewhere redraws the panel in place.
    return live().subscribe(affectsNow, () => store.refresh());
  });

  const panel = $derived(nowPanel(view.leases, new Date()));
  const refresh = 'min-h-11 rounded-md border border-border bg-surface-raised px-4 text-sm font-medium text-text';
</script>

<section aria-labelledby="now-heading" class="mb-4 rounded-md border border-border bg-surface-raised" data-testid="now-panel">
  <div class="flex items-center justify-between gap-3 p-3">
    <h3 id="now-heading" class="text-lg font-bold">Now</h3>
    <p class="min-w-0 flex-1 text-sm text-text-muted" data-testid="now-summary">{view.loading && view.leases.length === 0 ? 'Loading…' : panel.summary}</p>
    <button
      type="button"
      class="{refresh} md:hidden"
      aria-expanded={open}
      aria-controls="now-body"
      data-testid="now-toggle"
      onclick={() => (open = !open)}>{open ? 'Hide' : 'Show'}</button
    >
  </div>

  <div id="now-body" class="{open ? 'block' : 'hidden'} border-t border-border p-3 md:block" data-testid="now-body">
    {#if view.error !== undefined}
      <p role="alert" class="mb-3 rounded-md border border-danger px-3 py-2 text-sm text-danger">{view.error}</p>
    {/if}

    <h4 class="mb-2 text-sm font-medium text-text-muted">Conflicts</h4>
    {#if panel.conflicts.length === 0}
      <p class="mb-3 text-sm text-text-muted" data-testid="no-conflicts">No conflicts.</p>
    {:else}
      <ul class="mb-3 flex flex-col gap-2" data-testid="now-conflicts">
        {#each panel.conflicts as conflict (conflict.resource)}
          <li class="rounded-md border border-warning px-3 py-2 text-sm wrap-anywhere" data-testid="now-conflict" data-resource={conflict.resource}>
            <span class="font-medium">Conflict</span> {conflictText(conflict)}
            <span class="mt-1 flex flex-wrap gap-x-3">
              {#each conflict.holders as holder (holder.lease)}
                <a class="inline-flex min-h-11 min-w-11 items-center text-accent hover:underline" href={taskHref(holder.task)}>{holder.task}</a>
              {/each}
            </span>
          </li>
        {/each}
      </ul>
    {/if}

    <h4 class="mb-2 text-sm font-medium text-text-muted">Live leases</h4>
    {#if panel.rows.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-now-leases">No live leases.</p>
    {:else}
      <ul class="flex flex-col gap-2" data-testid="now-leases">
        {#each panel.rows as row (row.lease)}
          <li class="rounded-md border border-border p-3 text-sm wrap-anywhere" data-testid="now-lease" data-ref={row.lease} data-task={row.task} data-conflicting={row.conflicting}>
            <p>
              <span class="font-medium" data-testid="now-holder">{row.holder}</span>
              <span class="text-text-muted">{row.lease}</span> on
              <a class="inline-flex min-h-11 min-w-11 items-center text-accent hover:underline" href={taskHref(row.task)} data-testid="now-task">{row.task}</a>
              {#if row.conflicting}<span class="rounded-md border border-warning px-2 py-0.5 text-xs font-medium" data-testid="now-flag">conflict</span>{/if}
            </p>
            <p>{row.intent}</p>
            {#if row.resources.length > 0}<p class="font-mono text-text-muted">{row.resources.join(', ')}</p>{/if}
            <p class="text-text-muted">{row.expires}</p>
          </li>
        {/each}
      </ul>
    {/if}

    <button type="button" class="{refresh} mt-3" data-testid="now-refresh" onclick={() => store.load()}>Refresh</button>
  </div>
</section>
