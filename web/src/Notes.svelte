<script lang="ts">
  import Dialog from './Dialog.svelte';
  import NoteForm from './NoteForm.svelte';
  import NoteList from './NoteList.svelte';
  import Toast from './Toast.svelte';
  import { untrack } from 'svelte';
  import { live } from './lib/live';
  import { InitiativesApi } from './lib/initiatives';
  import { TasksApi } from './lib/tasks';
  import { resolveRoute, ROUTES } from './lib/routes';
  import { currentBrowser } from './lib/session';
  import { currentViewEnv, FilterTurns } from './lib/viewState';
  import { isFiltered, notesHash, DEFAULT_NOTES_FILTER, type NoteForm as PostForm, type NotesFilter } from './lib/notes';
  import {
    affectsNotes,
    forgetNotes,
    moreCount,
    notesLookup,
    NotesApi,
    NotesViewStore,
    openNotes,
    saveNotes,
    startNotesFilter,
    visibleNotes,
    type NoteScope,
    type NotesViewState,
  } from './lib/notesView';

  // The Notes view (PRD R10): every note newest first, the six filters in the fragment's query, a page at a time and
  // Post note. The data layer is lib/notesView.ts.
  let { csrf }: { csrf: string; param?: string } = $props();

  const browser = currentBrowser();
  const viewEnv = currentViewEnv();
  // A fragment with a query wins and is saved; a bare one restores the saved filters (in place, before the first fetch).
  const opened = openNotes(viewEnv, window.location.hash);
  const initial = opened.filter;
  let draft: NotesFilter = $state({ ...initial });
  // A restored filter may name an initiative or a task the board no longer has: it is checked before the notes are read.
  const lookup = notesLookup(new InitiativesApi(browser, () => csrf), new TasksApi(browser, () => csrf));
  let posting = $state(false);
  let toast = $state('');

  const store = new NotesViewStore(new NotesApi(browser, () => csrf), (s) => (view = s), initial);
  let view: NotesViewState = $state(store.state);

  // The first read waits for the check of a restored filter; what the board no longer knows is dropped and saved again.
  // A link, an applied filter or Clear filters while the check runs is newer and wins: the late answer is dropped.
  const turns = new FilterTurns();
  async function start() {
    const current = turns.begin();
    const kept = await startNotesFilter(opened, lookup);
    if (!current()) return;
    turns.reading();
    if (kept === initial) {
      void store.load();
    } else {
      draft = { ...kept };
      persist(kept);
      void store.setFilter(kept);
    }
  }

  $effect(() => {
    // Read nothing reactive here: a filter change must not run the first load again.
    void untrack(start);
    // A note posted elsewhere shows under the filter in place: the page drawn, the bodies read and an open sheet stay.
    // Nothing is read before the first read: the filter may still be under its check.
    return live().subscribe(affectsNotes, () => (turns.hasRead() ? store.refresh() : undefined));
  });

  // The filter lives in the fragment and in the saved query: applying rewrites both in place (no history entry per
  // choice) and reads again. Clear filters (the defaults) forgets the saved query.
  function persist(filter: NotesFilter) {
    window.history.replaceState(null, '', notesHash(filter));
    saveNotes(viewEnv.storage, filter);
  }

  function apply(filter: NotesFilter) {
    turns.begin();
    turns.reading();
    draft = { ...filter };
    persist(filter);
    void store.setFilter(filter);
  }

  function clear() {
    forgetNotes(viewEnv.storage);
    apply(DEFAULT_NOTES_FILTER);
  }

  // The fragment edited by hand, or the back button after a link with another query. A bare fragment restores the saved filters.
  async function hashChanged() {
    const hash = window.location.hash;
    if (resolveRoute(hash, ROUTES).id !== 'notes') return;
    const next = openNotes(viewEnv, hash);
    const current = turns.begin();
    const filter = await startNotesFilter(next, lookup);
    if (!current()) return; // the fragment (or the filter) changed again while the check ran
    if (filter !== next.filter) persist(filter);
    if (!turns.mustRead(notesHash(filter) === notesHash(view.filter))) return;
    turns.reading();
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

<section aria-labelledby="notes-heading" class="mx-auto w-full max-w-[1280px] pb-16 md:pb-0" data-testid="notes-view">
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
        <button type="button" class="{button} border-border text-text" data-testid="clear-filters" onclick={clear}>Clear filters</button>
      {/if}
    </div>
  </form>

  {#if view.error !== undefined}
    <p role="alert" class="mb-4 rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="notes-error">{view.error}</p>
  {/if}
  {#if view.error !== undefined && view.notes.length === 0}
    <!-- the error above is the whole answer: nothing was read to draw -->
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
