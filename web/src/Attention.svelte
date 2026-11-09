<script lang="ts">
  import Toast from './Toast.svelte';
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

  let { csrf }: { csrf: string; param?: string } = $props();

  const browser = currentBrowser();
  const initial = filterFromHash(window.location.hash);
  let view: AttentionState = $state({ attention: undefined, filter: initial, loading: true, error: undefined });
  let known: { slug: string; tags: string[] }[] = $state([]);
  let toast = $state('');

  const store = new AttentionStore(new AttentionApi(browser), (s) => (view = s), initial);

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
