<script lang="ts">
  import AnswerForm from './AnswerForm.svelte';
  import Dialog from './Dialog.svelte';
  import InitiativeConfirm from './InitiativeConfirm.svelte';
  import TaskCloseForm from './TaskCloseForm.svelte';
  import TaskNoteForm from './TaskNoteForm.svelte';
  import Toast from './Toast.svelte';
  import {
    ActionsApi,
    AttentionActions,
    NO_ACTIONS,
    actionLabel,
    actionName,
    actionsFor,
    initiativeHref,
    type ActionKind,
    type ActionsState,
  } from './lib/attentionActions';
  import { SECTIONS, attentionHash, emptyText, filterFromHash, isFiltered, sectionCount, toggled, type AttentionFilter } from './lib/attention';
  import {
    AttentionApi,
    AttentionStore,
    affectsAttention,
    copyLabel,
    filterChoices,
    rowsFor,
    type AttentionState,
    type ItemActor,
  } from './lib/attentionView';
  import { copyText } from './lib/clipboard';
  import { InitiativesApi } from './lib/initiatives';
  import { live } from './lib/live';
  import { currentBrowser } from './lib/session';
  import type { Resolution } from './lib/tasks';

  let { csrf }: { csrf: string; param?: string } = $props();

  const browser = currentBrowser();
  const initial = filterFromHash(window.location.hash);
  let view: AttentionState = $state({ attention: undefined, filter: initial, loading: true, error: undefined });
  let known: { slug: string; tags: string[] }[] = $state([]);
  let toast = $state('');

  const store = new AttentionStore(new AttentionApi(browser), (s) => (view = s), initial);
  let acting: ActionsState = $state(NO_ACTIONS);
  const actions = new AttentionActions(new ActionsApi(browser, () => csrf), (s) => (acting = s));

  $effect(() => {
    void store.load();
    void new InitiativesApi(browser, () => csrf)
      .list('all')
      .then((list) => (known = list.map((i) => ({ slug: i.slug, tags: i.tags }))))
      .catch(() => undefined); // the chips then offer only what the URL names; the sections show the error
    return live().subscribe(affectsAttention, () => store.load());
  });

  // The filter lives in the fragment: a change rewrites it in place (no history entry per choice) and fetches.
  function apply(filter: AttentionFilter) {
    window.history.replaceState(null, '', attentionHash(filter));
    void store.setFilter(filter);
  }

  // The fragment edited by hand, or the back button after a link with another query.
  function hashChanged() {
    const filter = filterFromHash(window.location.hash);
    if (attentionHash(filter) !== attentionHash(view.filter)) void store.setFilter(filter);
  }

  async function copy(actor: ItemActor) {
    const ok = await copyText(navigator.clipboard, actor.wayBack ?? '');
    toast = ok ? `Copied the way back of ${actor.handle}` : 'Copying failed. Select the way back and copy it by hand.';
  }

  // An action's success shows a toast; the item itself leaves its section when the stream's event refreshes the view.
  async function perform(kind: ActionKind, target: string, input = '') {
    const done = await actions.run(kind, target, input);
    if (done !== undefined) toast = done;
  }

  const ACK_BODY = 'The lease ran out without being ended. Acknowledging it records that you have seen it and takes it off Expired leases.';
  const choices = $derived(filterChoices(known, view.filter));
  const now = new Date();
  const chip =
    'min-h-11 min-w-11 max-w-full rounded-md border border-border bg-surface-raised px-3 py-1 text-sm wrap-anywhere text-text aria-pressed:border-accent aria-pressed:font-semibold aria-pressed:text-accent';
  const action = 'min-h-11 min-w-11 rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text';
</script>

<svelte:window onhashchange={hashChanged} />

<section aria-labelledby="attention-heading" class="pb-16 md:pb-0" data-testid="attention-view">
  <h2 id="attention-heading" class="mb-4 text-xl font-bold">Attention</h2>

  {#if choices.tags.length + choices.initiatives.length > 0}
    <div class="mb-4 flex flex-col gap-3" data-testid="attention-filters">
      {#if choices.tags.length > 0}
        <div role="group" aria-label="Tags" class="flex flex-wrap items-center gap-2">
          <span class="text-sm text-text-muted">Tags</span>
          {#each choices.tags as tag (tag)}
            <button
              type="button"
              class={chip}
              aria-pressed={view.filter.tags.includes(tag)}
              data-testid="tag-filter"
              onclick={() => apply({ ...view.filter, tags: toggled(view.filter.tags, tag) })}>{tag}</button
            >
          {/each}
        </div>
      {/if}
      {#if choices.initiatives.length > 0}
        <div role="group" aria-label="Initiatives" class="flex flex-wrap items-center gap-2">
          <span class="text-sm text-text-muted">Initiatives</span>
          {#each choices.initiatives as slug (slug)}
            <button
              type="button"
              class={chip}
              aria-pressed={view.filter.initiatives.includes(slug)}
              data-testid="initiative-filter"
              onclick={() => apply({ ...view.filter, initiatives: toggled(view.filter.initiatives, slug) })}>{slug}</button
            >
          {/each}
        </div>
      {/if}
      {#if isFiltered(view.filter)}
        <div><button type="button" class={action} data-testid="clear-filters" onclick={() => apply({ initiatives: [], tags: [] })}>Clear filters</button></div>
      {/if}
    </div>
  {/if}

  {#if view.error !== undefined}
    <p role="alert" class="mb-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">{view.error}</p>
  {/if}

  {#if view.attention === undefined}
    {#if view.error === undefined}
      <div aria-busy="true" class="flex flex-col gap-3" data-testid="attention-skeleton">
        <span class="sr-only">Loading what needs attention…</span>
        {#each [0, 1, 2] as n (n)}
          <div class="h-16 animate-pulse rounded-md bg-border"></div>
        {/each}
      </div>
    {/if}
  {:else}
    {@const data = view.attention}
    <div class="flex flex-col gap-6">
      {#each SECTIONS as section (section.key)}
        {@const rows = rowsFor(section.key, data, now)}
        <section aria-labelledby="section-{section.key}" data-testid="section-{section.key}">
          <h3 id="section-{section.key}" class="mb-2 flex items-baseline gap-2 text-lg font-semibold">
            {section.title}
            <span class="text-sm font-normal text-text-muted" data-testid="section-count">{sectionCount(data.counts, section.key)}</span>
          </h3>
          {#if rows.length === 0}
            <p class="rounded-md border border-dashed border-border px-4 py-4 text-text-muted" data-testid="section-empty">{emptyText(section.key)}</p>
          {:else}
            <ul class="flex flex-col gap-3">
              {#each rows as row (row.id)}
                {@const initiative = row.initiative}
                <li class="flex flex-col gap-2 rounded-md border border-border bg-surface-raised p-4 wrap-anywhere" data-testid="attention-item">
                  <p class="font-medium">
                    {#if row.href !== undefined}
                      <a class="inline-flex min-h-11 items-center text-accent hover:underline" href={row.href} data-testid="item-link">{row.title}</a>
                    {:else}
                      <span data-testid="item-title">{row.title}</span>
                    {/if}
                  </p>
                  {#if row.details.length > 0}
                    <p class="text-sm text-text-muted" data-testid="item-details">{row.details.join(' · ')}</p>
                  {/if}
                  <ul class="flex flex-col gap-2">
                    {#each row.actors as actor, n (n)}
                      <li class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm" data-testid="item-actor">
                        <span class="text-text-muted">{actor.role}</span>
                        <span class="font-medium" data-testid="actor-handle">{actor.handle}</span>
                        {#if actor.wayBack !== undefined}
                          <code class="font-mono text-text-muted" data-testid="actor-way-back">{actor.wayBack}</code>
                          <button type="button" class={action} aria-label={copyLabel(actor)} data-testid="copy-way-back" onclick={() => void copy(actor)}
                            >Copy way back</button
                          >
                        {:else}
                          <span class="text-text-muted">no way back on record</span>
                        {/if}
                      </li>
                    {/each}
                  </ul>
                  {#if initiative !== undefined}
                    <p class="text-sm">
                      <a class="inline-flex min-h-11 items-center text-accent hover:underline" href={initiativeHref(initiative)} data-testid="initiative-link">Initiative {initiative}</a>
                    </p>
                  {/if}
                  {#if acting.itemError?.target === row.id}
                    <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="item-error">{acting.itemError.text}</p>
                  {/if}
                  {#if actionsFor(section.key).length > 0}
                    <div class="flex flex-wrap gap-2" data-testid="item-actions">
                      {#each actionsFor(section.key) as kind (kind)}
                        <button
                          type="button"
                          class={action}
                          aria-label={actionName(kind, row.id)}
                          data-testid="action-{kind}"
                          onclick={() => void actions.open(kind, row.id)}>{actionLabel(kind)}</button
                        >
                      {/each}
                    </div>
                  {/if}
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      {/each}
    </div>
  {/if}
</section>

<Toast message={toast} onclear={() => (toast = '')} />

{#if acting.open !== undefined}
  {@const target = acting.open.target}
  {#if acting.open.kind === 'answer'}
    <Dialog label="Answer {target}" onclose={() => actions.cancel()} initialFocus="textarea[name=answer]">
      <AnswerForm ask={acting.ask} error={acting.error} onsubmit={(answer) => perform('answer', target, answer)} oncancel={() => actions.cancel()} />
    </Dialog>
  {:else if acting.open.kind === 'withdraw'}
    <Dialog label="Withdraw {target}" onclose={() => actions.cancel()} initialFocus="input[name=text]">
      <TaskNoteForm heading="Withdraw {target}" label="Reason (optional)" hint="Why the ask is withdrawn, in up to 200 characters." submitLabel="Withdraw ask" error={acting.error} onsubmit={(reason) => perform('withdraw', target, reason)} oncancel={() => actions.cancel()} />
    </Dialog>
  {:else if acting.open.kind === 'check'}
    <Dialog label="Record check" onclose={() => actions.cancel()} initialFocus="input[name=text]">
      <TaskNoteForm heading="Record check on {target}" label="Summary" hint="What the check found, in up to 200 characters. The next check is due one cadence from now." submitLabel="Record check" error={acting.error} onsubmit={(summary) => perform('check', target, summary)} oncancel={() => actions.cancel()} />
    </Dialog>
  {:else if acting.open.kind === 'acknowledge'}
    <Dialog label="Acknowledge {target}" onclose={() => actions.cancel()}>
      <InitiativeConfirm heading="Acknowledge {target}" body={ACK_BODY} confirm="Acknowledge lease" error={acting.error} onconfirm={() => perform('acknowledge', target)} oncancel={() => actions.cancel()} />
    </Dialog>
  {:else if acting.open.kind === 'close'}
    <Dialog label="Close task" onclose={() => actions.cancel()} initialFocus="select[name=resolution]">
      <TaskCloseForm ref={target} error={acting.error} onconfirm={(resolution: Resolution) => perform('close', target, resolution)} oncancel={() => actions.cancel()} />
    </Dialog>
  {/if}
{/if}
