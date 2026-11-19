<script lang="ts">
  import { replaceHash } from './lib/historyEntry';
  import AnswerForm from './AnswerForm.svelte';
  import AskList from './AskList.svelte';
  import NoteForm from './NoteForm.svelte';
  import NoteList from './NoteList.svelte';
  import TaskNoteForm from './TaskNoteForm.svelte';
  import { orderAsks, type Ask } from './lib/asks';
  import { newestFirst, type NoteForm as PostForm } from './lib/notes';
  import { ThreadApi, ThreadStore, type ThreadState } from './lib/threads';
  import { taskHref } from './lib/taskPage';
  import { untrack } from 'svelte';
  import Dialog from './Dialog.svelte';
  import FilterSelect from './FilterSelect.svelte';
  import { absoluteTime, relativeTime } from './lib/format';
  import { countBadges } from './lib/counts';
  import { DEFAULT_TASK_FILTER, TASK_FILTERS, type TaskFilter } from './lib/initiativeFilters';
  import { InitiativePageStore, concerns, liveLeaseCount, pageNeedsHref, pageNeedsQuery, targetsText, taskCounts, taskRows, type InitiativePageState } from './lib/initiativePage';
  import InitiativeConfirm from './InitiativeConfirm.svelte';
  import InitiativeEditForm from './InitiativeEditForm.svelte';
  import NeedsAttention from './NeedsAttention.svelte';
  import NewTaskDialog from './NewTaskDialog.svelte';
  import Toast from './Toast.svelte';
  import { closedToast, closeBody, closeLabel, notClosedCount, REOPEN_BODY } from './lib/initiativeDialogs';
  import { InitiativesApi, toEditForm, type InitiativeEdit } from './lib/initiatives';
  import { renderMarkdown } from './lib/markdown';
  import { NO_NEEDS, NeedsStore, withoutNeeds, type NeedsState } from './lib/needsAttention';
  import { initiativeTone, tagLabels, toneClasses } from './lib/presentation';
  import { live } from './lib/live';
  import { currentBrowser } from './lib/session';
  import SortHeader from './SortHeader.svelte';
  import SortSelect from './SortSelect.svelte';
  import DensitySwitch from './DensitySwitch.svelte';
  import TagChips from './TagChips.svelte';
  import { manualTagsOf, prefs } from './lib/proto/prefs.svelte';
  import { taskTags, type TaskTag } from './lib/proto/tags';
  import { sortRows, type SortState } from './lib/tableSort';
  import { clearViewQuery, currentViewEnv, openView, saveViewQuery } from './lib/viewState';
  import {
    INITIATIVE_VIEW,
    PAGE_TASK_COLUMNS,
    PAGE_TASK_SORT_LABELS,
    isCustomised,
    isPageHash,
    pagePath,
    viewFromQuery,
    viewHash,
    viewToQuery,
    type PageViewState,
  } from './lib/initiativePageView';
  import { leasesText } from './lib/counts';

  let { csrf, param }: { csrf: string; param: string } = $props();

  let view: InitiativePageState = $state({ initiative: undefined, tasks: [], filter: 'not-closed', loading: true, error: undefined, dialogError: undefined });
  let thread: ThreadState = $state({ asks: [], notes: [], bodies: {}, error: undefined, actionError: undefined });
  let dialog: 'edit' | 'close' | 'reopen' | 'task' | 'note' | { answer: Ask } | { withdraw: string } | undefined = $state(undefined);
  let toast = $state('');

  const api = new InitiativesApi(currentBrowser(), () => csrf);
  const store = new InitiativePageStore(api, (s) => (view = s));

  // The task filter and the sort live in the fragment and in ONE saved query shared by every initiative. A fragment with a
  // query wins and is saved; a bare one restores the saved query in place, before the first fetch.
  const viewEnv = currentViewEnv();
  const queryOf = (hash: string) => (hash.includes('?') ? hash.slice(hash.indexOf('?') + 1) : '');
  const slug = untrack(() => param);
  const opened = viewFromQuery(openView(viewEnv, INITIATIVE_VIEW, pagePath(slug), queryOf(window.location.hash)));
  let sort: SortState = $state(opened.sort);
  store.setFilter(opened.filter);

  // Rewrites the fragment in place (no history entry per choice) and saves the query under the shared key.
  function persist(state: PageViewState) {
    replaceHash(viewHash(slug, state));
    saveViewQuery(viewEnv.storage, INITIATIVE_VIEW, viewToQuery(state));
  }

  function filterChanged(filter: TaskFilter) {
    persist({ filter, sort });
    store.setFilter(filter);
  }

  // The sort only reorders the rows the page holds: no request.
  function setSort(next: SortState) {
    sort = next;
    persist({ filter: view.filter, sort });
  }

  // Clear filters: the defaults, and nothing saved.
  function clear() {
    sort = null;
    clearViewQuery(viewEnv.storage, INITIATIVE_VIEW);
    replaceHash(pagePath(slug));
    store.setFilter(DEFAULT_TASK_FILTER);
  }

  // The fragment edited by hand, or the back button after a link with another query. A bare page fragment restores the saved query.
  function hashChanged() {
    const hash = window.location.hash;
    if (!isPageHash(slug, hash)) return;
    const next = viewFromQuery(openView(viewEnv, INITIATIVE_VIEW, pagePath(slug), queryOf(hash)));
    sort = next.sort;
    if (next.filter !== view.filter) store.setFilter(next.filter);
  }
  let needsView: NeedsState = $state({ needs: NO_NEEDS, error: undefined });
  const needsStore = new NeedsStore((query) => api.needs(query), (s) => (needsView = s));
  // The section reads this initiative's tasks that need attention under the page's task filter, again whenever the filter changes.
  const reloadNeeds = () => needsStore.load(pageNeedsQuery(param, view.filter));

  const threads = new ThreadStore(new ThreadApi(currentBrowser(), () => csrf), (s) => (thread = s));

  $effect(() => {
    void store.load(param);
    void threads.load({ initiative: param });
    // An event naming this initiative reads the page again in place: no skeleton, so scroll, expanded notes and open dialogs stay.
    return live().subscribe(concerns(param), () => Promise.all([store.reload(param), threads.load({ initiative: param }), reloadNeeds()]));
  });

  // Only a change of the filter's value (not every update of the page state) reads the section again.
  const filterNow = $derived(view.filter);
  $effect(() => {
    void needsStore.load(pageNeedsQuery(param, filterNow));
  });

  function open(which: 'edit' | 'close' | 'reopen' | 'task' | 'note' | { answer: Ask } | { withdraw: string }) {
    store.clearDialogError();
    threads.clearActionError();
    dialog = which;
  }

  async function finish(done: Promise<boolean>, message: string) {
    if (await done) {
      dialog = undefined;
      toast = message;
    }
  }

  function taskCreated(ref: string) {
    dialog = undefined;
    toast = `Task ${ref} created`;
    void store.reload(param);
    void reloadNeeds();
  }

  const save = (form: InitiativeEdit) => finish(store.edit(param, form), 'Initiative saved');
  const closeIt = (dropped: number) => finish(store.close(param, dropped > 0), closedToast(param, dropped));
  const reopenIt = () => finish(store.reopen(param), `Initiative ${param} reopened`);

  const scope = $derived({ initiative: param });
  // An answer or a withdrawal can unblock tasks, so the tasks are read again with the asks.
  async function settle(done: Promise<boolean>, message: string) {
    if (await done) {
      dialog = undefined;
      toast = message;
      void store.reload(param);
      void reloadNeeds();
    }
  }
  const answer = (ask: Ask, text: string) => settle(threads.answer(scope, ask.ref, text), `Answered ${ask.ref}`);
  const withdraw = (askRef: string, text: string) => settle(threads.withdraw(scope, askRef, text), `Withdrew ${askRef}`);
  const postNote = async (form: PostForm) => {
    if (await threads.postNote(scope, form)) {
      dialog = undefined;
      toast = `Note posted on ${param}`;
    }
  };

  const now = new Date();
  const asks = $derived(orderAsks(thread.asks));
  const notes = $derived(newestFirst(thread.notes));
  const small = 'min-h-11 rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text';
  const pending = $derived(notClosedCount(view.tasks));
  const needs = $derived(needsView.needs);
  const needsRows = $derived(taskRows(needs.items, 'all'));
  // The table sorts by the chosen column; the Needs attention section above it keeps its own order.
  // Prototype: each task's tags (the initiative's, live agent kinds, this browser's own).
  const tagsFor = $derived.by(() => {
    void prefs.manual;
    const own = new Map([[view.initiative?.slug ?? '', view.initiative?.tags ?? []]]);
    const byRef = new Map([...view.tasks, ...needs.items].map((t) => [t.ref, t] as const));
    return (ref: string): TaskTag[] => {
      const t = byRef.get(ref);
      return t === undefined ? [] : taskTags(t, own, manualTagsOf(ref));
    };
  });
  const rows = $derived(sortRows(taskRows(withoutNeeds(view.tasks, needs), view.filter), sort, PAGE_TASK_COLUMNS));
  const badge = 'inline-block rounded-md border px-2 py-0.5 text-xs font-medium';
  const cell = 'md:table-cell md:px-3 md:py-3';
  const head = 'px-3 py-2 text-sm font-medium text-text-muted';
  const action = 'min-h-11 rounded-md border border-border bg-surface-raised px-4 text-sm font-medium text-text';
  const back = 'inline-flex min-h-11 items-center text-sm font-medium text-accent hover:underline';
</script>

<svelte:window onhashchange={hashChanged} />

<!-- From 1536 px (2xl) the page is a grid: the main column (header, Needs attention, Tasks) and a 420 px side column (Open asks, Notes). Below it the side column follows the main one, in flow. -->
<section
  aria-labelledby="initiative-heading"
  class="vh-dense mx-auto w-full max-w-[2560px] pb-16 md:pb-0 2xl:grid 2xl:grid-cols-[minmax(0,1fr)_420px] 2xl:gap-x-6"
  data-testid="initiative-page"
>
  <div class="min-w-0 2xl:col-start-1" data-testid="initiative-main">
  <a class={back} href="#/initiatives" data-testid="back-link">← Initiatives</a>

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
      <div class="flex flex-wrap items-center gap-3">
        <h2 id="initiative-heading" class="wrap-anywhere text-xl font-bold" data-testid="initiative-title">{initiative.title}</h2>
        <span class="ml-auto"><DensitySwitch /></span>
      </div>
      <div class="mt-1 flex flex-wrap items-center gap-2">
        <code class="wrap-anywhere font-mono text-sm text-text-muted" data-testid="initiative-slug">{initiative.slug}</code>
        <span class="{badge} {toneClasses(initiativeTone(initiative.status))}" data-testid="initiative-status">{initiative.status}</span>
        <TagChips tags={tagLabels(initiative.tags).map((tag) => ({ tag, source: 'initiative' as const }))} />
      </div>
      <p class="mt-2 text-sm text-text-muted wrap-anywhere">
        {#if targetsText(initiative) !== ''}
          <span data-testid="initiative-targets">Targets: {targetsText(initiative)}</span> ·
        {/if}
        Created <time datetime={initiative.created_at} title={absoluteTime(initiative.created_at)}>{relativeTime(initiative.created_at, now)}</time>
        by {initiative.created_by}
      </p>
      <div class="mt-3 flex flex-wrap gap-2" data-testid="initiative-actions">
        {#if initiative.status === 'open'}
          <button type="button" class="{action} border-accent text-accent" onclick={() => open('task')}>New task</button>
        {/if}
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

    {#if needsView.error !== undefined}
      <p role="alert" class="mb-4 rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="needs-error">{needsView.error}</p>
    {/if}
    <NeedsAttention rows={needsRows} more={needs.more} moreHref={pageNeedsHref(param, view.filter)} showInitiative={false} tagsFor={(ref) => tagsFor(ref)} />

    <div class="mb-3 flex flex-wrap items-center justify-between gap-3" data-testid="tasks-heading">
      <h3 class="text-lg font-bold">Tasks</h3>
      <div class="flex min-w-0 max-w-full flex-wrap items-center gap-x-4 gap-y-2">
        <FilterSelect label="Tasks" options={TASK_FILTERS} value={view.filter} onchange={filterChanged} testid="task-filter" />
        <SortSelect columns={PAGE_TASK_SORT_LABELS} {sort} onsort={setSort} />
        {#if isCustomised({ filter: view.filter, sort })}
          <button type="button" class="min-h-11 rounded-md border border-border px-4 text-sm font-medium text-text" data-testid="clear-filters" onclick={clear}>Clear filters</button>
        {/if}
      </div>
    </div>

    {#if rows.length === 0 && needsRows.length === 0}
      <p class="rounded-md border border-dashed border-border px-4 py-8 text-center text-text-muted" data-testid="no-tasks">No tasks match this filter.</p>
    {:else if rows.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-other-tasks">No other tasks match this filter.</p>
    {:else}
      <!-- One table: from 768 px a dense table, below it each row is a card. -->
      <table class="block w-full text-left wrap-anywhere md:table md:table-fixed md:border-collapse">
        <thead class="sr-only max-md:[&_th]:block max-md:[&_tr]:block md:table-header-group md:not-sr-only">
          <tr class="md:border-b md:border-border">
            <SortHeader column="ref" label="Ref" {sort} onsort={setSort} class="{head} md:w-32" />
            <SortHeader column="title" label="Title" {sort} onsort={setSort} class={head} />
            <SortHeader column="kind" label="Kind" {sort} onsort={setSort} class="{head} md:w-20" />
            <SortHeader column="status" label="Status" {sort} onsort={setSort} class="{head} md:w-24" />
            <SortHeader column="flags" label="Flags" {sort} onsort={setSort} class="{head} md:w-1/5" />
            <SortHeader column="holder" label="Lease holder" {sort} onsort={setSort} class="{head} md:w-32" />
          </tr>
        </thead>
        <tbody class="flex flex-col gap-3 md:table-row-group">
          {#each rows as row (row.id)}
            <tr
              data-testid="task-row"
              data-ref={row.ref}
              class="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-border bg-surface-raised p-4 md:table-row md:rounded-none md:border-0 md:border-b md:bg-transparent md:p-0"
            >
              <td class="font-mono text-sm {cell}" data-testid="task-ref"><a class="inline-flex min-h-11 min-w-11 items-center text-accent hover:underline" href={taskHref(row.ref)}>{row.ref}</a></td>
              <td class="order-last w-full min-w-0 font-medium md:order-none {cell}" data-testid="task-title"
                >{row.title} <TagChips tags={tagsFor(row.ref)} taskRef={row.ref} editable /></td
              >
              <td class="text-sm {cell}" data-testid="task-kind">{row.kind}</td>
              <td class={cell} data-testid="task-status">
                <span class="{badge} border-border text-text-muted">{row.status}</span>
              </td>
              <td class="flex flex-wrap gap-1 empty:hidden {cell}" data-testid="task-flags">
                {#each row.attention as reason (reason.reason)}
                  <span class="{badge} {toneClasses(reason.tone)}" data-testid="attention-{reason.reason}">
                    <span aria-hidden="true">{reason.label}</span><span class="sr-only">{reason.name}</span>
                  </span>
                {/each}
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
  </div>

  {#if !view.loading && view.initiative !== undefined}
    <aside
      aria-label="Open asks and notes"
      class="mt-6 min-w-0 2xl:sticky 2xl:top-6 2xl:col-start-2 2xl:row-start-1 2xl:mt-0 2xl:max-h-[calc(100vh-3rem)] 2xl:self-start 2xl:overflow-y-auto"
      data-testid="initiative-side"
    >
      {#if thread.error !== undefined}
        <p role="alert" class="mb-6 rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="thread-error">{thread.error}</p>
      {/if}

      <div data-testid="open-asks-section">
        <div class="mb-3 flex flex-wrap items-center justify-between gap-3"><h3 class="text-lg font-bold">Open asks</h3></div>
        {#if asks.length === 0}
          <p class="text-sm text-text-muted" data-testid="no-asks">No open asks.</p>
        {:else}
          <AskList {asks} testid="initiative-asks" showTask actions={{ onanswer: (ask) => open({ answer: ask }), onwithdraw: (askRef) => open({ withdraw: askRef }) }} />
        {/if}
      </div>

      <div class="mt-6" data-testid="notes-section">
        <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
          <h3 class="text-lg font-bold">Notes</h3>
          <button type="button" class={small} onclick={() => open('note')}>Post note</button>
        </div>
        {#if notes.length === 0}
          <p class="text-sm text-text-muted" data-testid="no-notes">No notes.</p>
        {:else}
          <NoteList {notes} bodies={thread.bodies} testid="initiative-notes" showTask onexpand={(noteRef) => void threads.loadBody(noteRef)} />
        {/if}
      </div>
    </aside>
  {/if}
</section>

<Toast message={toast} onclear={() => (toast = '')} />

{#if dialog !== undefined && view.initiative !== undefined}
  {@const initiative = view.initiative}
  {#if dialog === 'task'}
    <NewTaskDialog {csrf} initiative={initiative.slug} onclose={() => (dialog = undefined)} oncreated={taskCreated} />
  {:else if dialog === 'note'}
    <Dialog label="Post note" onclose={() => (dialog = undefined)} initialFocus="input[name=summary]">
      <NoteForm heading="Post a note on {initiative.slug}" error={thread.actionError} onsubmit={postNote} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if typeof dialog === 'object' && 'answer' in dialog}
    {@const target = dialog.answer}
    <Dialog label="Answer ask" onclose={() => (dialog = undefined)} initialFocus="textarea[name=answer]">
      <AnswerForm ask={target} error={thread.actionError} onsubmit={(text) => answer(target, text)} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if typeof dialog === 'object'}
    {@const askRef = dialog.withdraw}
    <Dialog label="Withdraw ask" onclose={() => (dialog = undefined)} initialFocus="input[name=text]">
      <TaskNoteForm heading="Withdraw {askRef}" label="Reason (optional)" hint="Why the ask is no longer needed, in up to 200 characters." submitLabel="Withdraw" error={thread.actionError} onsubmit={(text) => withdraw(askRef, text)} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if dialog === 'edit'}
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
