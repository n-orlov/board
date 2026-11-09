<script lang="ts">
  import Dialog from './Dialog.svelte';
  import NoteForm from './NoteForm.svelte';
  import NoteList from './NoteList.svelte';
  import Toast from './Toast.svelte';
  import { currentBrowser } from './lib/session';
  import { filterFromHash, isFiltered, notesHash, DEFAULT_NOTES_FILTER, type NoteForm as PostForm, type NotesFilter } from './lib/notes';
  import { moreCount, NotesApi, NotesViewStore, visibleNotes, type NoteScope, type NotesViewState } from './lib/notesView';

  // The Notes view (PRD R10): every note newest first, the six filters in the fragment's query, a page at a time and
  // Post note. The data layer is lib/notesView.ts.
  let { csrf }: { csrf: string; param?: string } = $props();

  const browser = currentBrowser();
  const initial = filterFromHash(window.location.hash);
  let draft: NotesFilter = $state({ ...initial });
  let posting = $state(false);
  let toast = $state('');

  const store = new NotesViewStore(new NotesApi(browser, () => csrf), (s) => (view = s), initial);
  let view: NotesViewState = $state(store.state);

  $effect(() => {
    void store.load();
  });

  // The filter lives in the fragment: applying rewrites it in place (no history entry per choice) and reads again.
  function apply(filter: NotesFilter) {
    draft = { ...filter };
    window.history.replaceState(null, '', notesHash(filter));
    void store.setFilter(filter);
  }

  // The fragment edited by hand, or the back button after a link with another query.
  function hashChanged() {
    const filter = filterFromHash(window.location.hash);
    if (notesHash(filter) === notesHash(view.filter)) return;
    draft = { ...filter };
    void store.setFilter(filter);
  }

  async function post(form: PostForm, scope: NoteScope) {
    if (await store.post(form, scope)) {
      posting = false;
      toast = 'Note posted';
    }
  }

  const shown = $derived(visibleNotes(view));
  const more = $derived(moreCount(view));
  const input = 'mt-1 block min-h-11 w-full rounded-md border border-border bg-surface px-3 py-2 text-base text-text';
  const chip =
    'min-h-11 min-w-11 rounded-md border border-border bg-surface-raised px-3 py-1 text-sm text-text aria-pressed:border-accent aria-pressed:font-semibold aria-pressed:text-accent';
  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium';
</script>

<svelte:window onhashchange={hashChanged} />

<section aria-labelledby="notes-heading" class="pb-16 md:pb-0" data-testid="notes-view">
  <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
    <h2 id="notes-heading" class="text-xl font-bold">Notes</h2>
    <button
      type="button"
      class="{button} border-accent text-accent"
      onclick={() => {
        store.clearActionError();
        posting = true;
      }}>Post note</button
    >
  </div>

  <form
    class="mb-4 grid grid-cols-1 gap-3 md:grid-cols-3"
    aria-label="Filter notes"
    data-testid="notes-filters"
    onsubmit={(event) => {
      event.preventDefault();
      apply(draft);
    }}
  >
    <div class="md:col-span-3">
      <button
        type="button"
        class="{chip}"
        aria-pressed={draft.broadcastOnly}
        data-testid="broadcast-filter"
        onclick={() => (draft.broadcastOnly = !draft.broadcastOnly)}>Broadcast only</button
      >
    </div>
    <label class="block text-sm font-medium">Task <input name="task" class={input} bind:value={draft.task} autocomplete="off" placeholder="phone-onboard-3" /></label>
    <label class="block text-sm font-medium">Initiative <input name="initiative" class={input} bind:value={draft.initiative} autocomplete="off" placeholder="phone-onboard" /></label>
    <label class="block text-sm font-medium">Resource <input name="resource" class={input} bind:value={draft.resource} autocomplete="off" placeholder="host:build-1" /></label>
    <label class="block text-sm font-medium">Author <input name="author" class={input} bind:value={draft.author} autocomplete="off" /></label>
    <label class="block text-sm font-medium">Mention <input name="mention" class={input} bind:value={draft.mention} autocomplete="off" /></label>
    <div class="flex items-end gap-2">
      <button type="submit" class="{button} border-accent text-accent" data-testid="apply-filters">Apply filters</button>
      {#if isFiltered(view.filter) || isFiltered(draft)}
        <button type="button" class="{button} border-border text-text" data-testid="clear-filters" onclick={() => apply(DEFAULT_NOTES_FILTER)}>Clear filters</button>
      {/if}
    </div>
  </form>

  {#if view.error !== undefined}
    <p role="alert" class="mb-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">{view.error}</p>
  {:else if view.loading && view.notes.length === 0}
    <p class="text-text-muted" role="status">Loading…</p>
  {:else if view.notes.length === 0}
    <p class="text-sm text-text-muted" data-testid="no-notes">{isFiltered(view.filter) ? 'No notes match these filters.' : 'No notes.'}</p>
  {:else}
    <NoteList notes={shown} bodies={view.bodies} testid="notes-list" showScope onexpand={(ref) => void store.loadBody(ref)} />
    {#if more > 0}
      <button type="button" class="{button} mt-3 border-border bg-surface-raised text-text" data-testid="load-more" onclick={() => store.loadMore()}>Load more ({more} next)</button>
    {/if}
  {/if}
</section>

{#if posting}
  <Dialog label="Post note" onclose={() => (posting = false)} initialFocus="input[name=task]">
    <NoteForm heading="Post a note" withScope error={view.actionError} onsubmit={post} oncancel={() => (posting = false)} />
  </Dialog>
{/if}

<Toast message={toast} onclear={() => (toast = '')} />
