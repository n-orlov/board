<script lang="ts">
  import { replaceHash } from './lib/historyEntry';
  import AnswerForm from './AnswerForm.svelte';
  import ChipFilter from './ChipFilter.svelte';
  import Dialog from './Dialog.svelte';
  import NowDock from './NowDock.svelte';
  import InitiativeConfirm from './InitiativeConfirm.svelte';
  import TaskCloseForm from './TaskCloseForm.svelte';
  import TaskNoteForm from './TaskNoteForm.svelte';
  import SegmentedRadio from './SegmentedRadio.svelte';
  import SortControl from './SortControl.svelte';
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
  import { onDestroy, untrack } from 'svelte';
  import { nowHash, type AttentionFilter } from './lib/attention';
  import { chipsActive, initiativeChoices, mergeChoices, type ChipState, type TagChoice } from './lib/chipFilter';
  import {
    AttentionApi,
    AttentionStore,
    affectsAttention,
    copyLabel,
    nowTagChoices,
    forgetAttention,
    openAttention,
    saveAttention,
    startFilter,
    type Attention,
    type AttentionState,
    type ItemActor,
  } from './lib/attentionView';
  import { copyText } from './lib/clipboard';
  import { InitiativesApi } from './lib/initiatives';
  import { TasksApi } from './lib/tasks';
  import { carriedTags, type Carried } from './lib/tagCarry';
  import {
    BANDS,
    WIDE_QUERY,
    bandCount,
    bandRows,
    isOpen,
    layoutOf,
    loadBandPrefs,
    saveBandPrefs,
    withChoice,
    type BandInfo,
    type FoldKey,
    type Layout,
  } from './lib/nowBands';
  import {
    DOCK_QUERY,
    isDefaultLayout,
    loadLayout,
    resetLayout,
    saveLayout,
    setOpen,
    widgetOpen,
    type NowLayout,
    type WidgetKey,
  } from './lib/nowLayout';
  import { live } from './lib/live';
  import { NOW_CHOICES, NOW_COLUMNS, loadOrder, orderToSort, saveOrder, sortByOrder, sortToOrder, type NowOrder } from './lib/nowSort';
  import { WINDOWS, flightRows, loadWindow, recentRows, saveWindow, type NowWindow } from './lib/nowFlight';
  import { absoluteTime } from './lib/format';
  import { resolveRoute, ROUTES } from './lib/routes';
  import { currentBrowser } from './lib/session';
  import type { Resolution } from './lib/tasks';
  import TagChips from './TagChips.svelte';
  import type { TagMark } from './lib/chipFilter';
  import { marksOf, taskOfHref } from './lib/nowTags';
  import type { SortState } from './lib/tableSort';
  import { currentViewEnv, FilterTurns } from './lib/viewState';

  let { csrf }: { csrf: string; param?: string } = $props();

  const browser = currentBrowser();
  const viewEnv = currentViewEnv();
  // A fragment with a query wins and is saved; a bare one restores the saved filter (in place, before the first fetch).
  const opened = openAttention(viewEnv, window.location.hash);
  const initial = opened.filter;
  const initialWindow = loadWindow(viewEnv.storage);
  let view: AttentionState = $state({ attention: undefined, flight: undefined, recent: undefined, marks: {}, filter: initial, window: initialWindow, loading: true, error: undefined });
  let known: { slug: string; tags: string[] }[] = $state([]);
  // The lookup's answer, or undefined until it answers (or when it failed): an empty answer still drops restored values.
  let looked: { slug: string; tags: string[] }[] | undefined;
  let toast = $state('');

  const store = new AttentionStore(new AttentionApi(browser), (s) => (view = s), initial, initialWindow);
  let acting: ActionsState = $state(NO_ACTIONS);
  const actions = new AttentionActions(new ActionsApi(browser, () => csrf), (s) => (acting = s));

  // A restored filter may name an initiative or a tag the board no longer has: the first fetch waits for the list to drop them.
  // A link, an applied filter or Clear filters while the check runs is newer and wins: the late answer is dropped.
  const turns = new FilterTurns();
  // Leaving the view drops a check still running: its late answer must not rewrite the view navigated to, nor read.
  onDestroy(() => turns.end());
  let listed: Promise<void> = Promise.resolve();
  // The restored tags no initiative lists are valid when some task carries them (own, or a derived actor kind): the server says.
  const tasksApi = new TasksApi(browser, () => csrf);
  async function carriedOf(filter: AttentionFilter): Promise<Carried | undefined> {
    return looked === undefined ? undefined : carriedTags(filter.tags, looked, (tag) => tasksApi.tagMarks(tag));
  }
  // A link's own tags are kept as they are, but their chips still name the sources the tasks give them: read in the background,
  // so the board's first read does not wait for it. A failed read leaves the chip as the view's own rows name it.
  function offerSources(filter: AttentionFilter) {
    if (filter.tags.length === 0) return;
    void carriedTags(filter.tags, [], (tag) => tasksApi.tagMarks(tag)).then((c) => {
      seen = mergeChoices(seen, c.marks, []);
    });
  }
  async function start() {
    const current = turns.begin();
    listed = new InitiativesApi(browser, () => csrf)
      .list('all')
      .then((list) => {
        looked = list.map((i) => ({ slug: i.slug, tags: i.tags }));
        known = looked;
      })
      .catch(() => undefined); // the chips then offer only what the URL names; the sections show the error
    let carried: Carried | undefined;
    if (opened.restored) {
      await listed;
      carried = await carriedOf(opened.filter);
    } else {
      offerSources(opened.filter);
    }
    if (!current()) return;
    // The restored chips name the sources the tasks give them, whatever the combined filter then returns.
    if (carried !== undefined) seen = mergeChoices(seen, carried.marks, []);
    turns.reading();
    const kept = startFilter(opened, looked, carried?.tags);
    if (kept === initial) {
      void store.load();
    } else {
      persist(kept);
      void store.setFilter(kept);
    }
  }

  $effect(() => {
    // Read nothing reactive here: a filter change must not run the first load again.
    void untrack(start);
    return live().subscribe(affectsAttention, () => (turns.hasRead() ? store.load() : undefined));
  });

  // The filter lives in the fragment and in the saved query: a change rewrites both in place (no history entry per choice).
  function persist(filter: AttentionFilter) {
    replaceHash(nowHash(filter));
    saveAttention(viewEnv.storage, filter);
  }

  function apply(filter: AttentionFilter) {
    turns.begin();
    turns.reading();
    persist(filter);
    void store.setFilter(filter);
  }

  // A chip change: the chips as the fragment and the saved query, and the board read again under them. No chip left is Clear
  // chips: the defaults, and nothing saved.
  function setChips(next: ChipState) {
    if (chipsActive(next)) apply(next);
    else clear();
  }

  // Clear chips: the defaults, and nothing saved.
  function clear() {
    turns.begin();
    turns.reading();
    forgetAttention(viewEnv.storage);
    replaceHash('#/now');
    void store.setFilter({ initiatives: [], tags: [] });
  }

  // The fragment edited by hand, or the back button after a link with another query. A bare fragment restores the saved filter.
  async function hashChanged() {
    if (resolveRoute(window.location.hash, ROUTES).id !== 'now') return;
    const next = openAttention(viewEnv, window.location.hash);
    const current = turns.begin();
    // A restored filter waits for the board's list like the first one does; a link's own query does not.
    let carried: Carried | undefined;
    if (next.restored) {
      await listed;
      carried = await carriedOf(next.filter);
    } else {
      offerSources(next.filter);
    }
    if (!current()) return; // the fragment (or the filter) changed again while the list was read
    if (carried !== undefined) seen = mergeChoices(seen, carried.marks, []);
    const filter = startFilter(next, looked, carried?.tags);
    if (filter !== next.filter) persist(filter);
    if (!turns.mustRead(nowHash(filter) === nowHash(view.filter))) return;
    turns.reading();
    void store.setFilter(filter);
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

  // The bands' open state: remembered apart for wide and narrow screens, and followed live as the window is resized.
  let prefs = $state(loadBandPrefs(viewEnv.storage));
  const wideQuery = typeof window.matchMedia === 'function' ? window.matchMedia(WIDE_QUERY) : undefined;
  let layout: Layout = $state(layoutOf(wideQuery?.matches ?? true));
  $effect(() => {
    if (wideQuery === undefined) return;
    const follow = () => (layout = layoutOf(wideQuery.matches));
    wideQuery.addEventListener('change', follow);
    return () => wideQuery.removeEventListener('change', follow);
  });

  // From lg (1024 px) the widgets are docked in columns the person arranges (the layout, saved under its own key, with its own
  // open states); below it they are stacked, the bands as an accordion that keeps the open state above. The stored layout is
  // never touched on a narrow screen.
  const dockQuery = typeof window.matchMedia === 'function' ? window.matchMedia(DOCK_QUERY) : undefined;
  let docked = $state(dockQuery?.matches ?? true);
  $effect(() => {
    if (dockQuery === undefined) return;
    const follow = () => (docked = dockQuery.matches);
    dockQuery.addEventListener('change', follow);
    return () => dockQuery.removeEventListener('change', follow);
  });
  let dock: NowLayout = $state(loadLayout(viewEnv.storage));
  function arrange(next: NowLayout, final: boolean) {
    dock = next;
    if (final) saveLayout(viewEnv.storage, next);
  }
  function toggleWidget(key: WidgetKey, open: boolean) {
    arrange(setOpen(dock, key, open), true);
  }
  function reset() {
    arrange(resetLayout(), true);
  }
  const arranged = $derived(!isDefaultLayout(dock));

  // The window of Recently touched: remembered under its own key, and a change reads the board again.
  function chooseWindow(next: NowWindow) {
    saveWindow(viewEnv.storage, next);
    void store.setWindow(next);
  }

  // The one sort of the page: it orders the rows inside every band and both right-hand lists, and is remembered per browser.
  let order: NowOrder = $state(loadOrder(viewEnv.storage));
  function chooseOrder(sort: SortState) {
    order = sortToOrder(sort);
    saveOrder(viewEnv.storage, order);
  }

  function toggle(key: FoldKey, open: boolean) {
    prefs = withChoice(prefs, key, layout, open);
    saveBandPrefs(viewEnv.storage, prefs);
  }

  const ACK_BODY = 'The lease ran out without being ended. Acknowledging it records that you have seen it and takes it off Expired leases.';
  // The tag chips: the initiatives' tags and every tag of the tasks the board read shows, kept while a narrower read follows.
  let seen: TagChoice[] = $state([]);
  $effect(() => {
    const read = view.marks;
    const chosen = view.filter.tags;
    seen = untrack(() => nowTagChoices([], chosen, read, seen));
  });
  const tagChoices = $derived(nowTagChoices(known, view.filter.tags, {}, seen));
  const initiativeSlugs = $derived(initiativeChoices(known.map((i) => i.slug), view.filter.initiatives));
  // The ages and the time left on a lease are phrased against this instant: it moves with every answer and every half minute.
  let now = $state(new Date());
  $effect(() => {
    void view.attention;
    now = new Date();
  });
  $effect(() => {
    const timer = setInterval(() => (now = new Date()), 30_000);
    return () => clearInterval(timer);
  });
  // The tags of every task shown come with the board read (`task_marks`): Now makes no read of the tasks for them.
  const marks = $derived(view.marks);
  const flightList = $derived(sortByOrder(flightRows(view.flight ?? [], now), order, (r) => r));
  const recentList = $derived(sortByOrder(recentRows(view.recent ?? [], now), order, (r) => r));
  function widgetCount(key: WidgetKey, data: Attention): number {
    if (key === 'flight') return flightList.length;
    if (key === 'recent') return recentList.length;
    return bandCount(data.counts, BANDS.find((band) => band.key === key)!);
  }
  const action = 'min-h-11 min-w-11 rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text';
</script>

<svelte:window onhashchange={hashChanged} />

{#snippet bandItems(band: BandInfo, data: Attention)}
  {@const rows = sortByOrder(bandRows(band, data, now), order, (r) => ({ at: r.row.at, by: r.row.actors[0]?.handle }))}
  {#if rows.length === 0}
    <p class="rounded-md border border-dashed border-border px-4 py-4 text-text-muted" data-testid="band-empty">{band.empty}</p>
  {:else}
    <ul class="flex flex-col gap-3">
      {#each rows as { section, row } (`${section}:${row.id}`)}
        {@const initiative = row.initiative}
        <li class="vh-card flex flex-col gap-2 rounded-md border border-border bg-surface-raised p-4 wrap-anywhere" data-testid="attention-item" data-section={section}>
          <p class="font-medium">
            {#if row.href !== undefined}
              <a class="inline-flex min-h-11 items-center text-accent hover:underline" href={row.href} data-testid="item-link">{row.title}</a>
            {:else}
              <span data-testid="item-title">{row.title}</span>
            {/if}
          </p>
          {#if marksOf(marks, taskOfHref(row.href)).length > 0}
            <TagChips marks={marksOf(marks, taskOfHref(row.href))} ref={taskOfHref(row.href) ?? ''} />
          {/if}
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
                  <button type="button" class={action} aria-label={copyLabel(actor)} data-testid="copy-way-back" onclick={() => void copy(actor)}>Copy way back</button>
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
          {#if actionsFor(section).length > 0}
            <div class="flex flex-wrap gap-2" data-testid="item-actions">
              {#each actionsFor(section) as kind (kind)}
                <button type="button" class={action} aria-label={actionName(kind, row.id)} data-testid="action-{kind}" onclick={() => void actions.open(kind, row.id)}>{actionLabel(kind)}</button>
              {/each}
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
{/snippet}

{#snippet flightBody()}
  {#if flightList.length === 0}
    <p class="rounded-md border border-dashed border-border px-4 py-4 text-text-muted" data-testid="flight-empty">Nothing is in flight. No task has a live lease.</p>
  {:else}
    <ul class="flex flex-col gap-3" data-testid="flight-list">
      {#each flightList as row (row.task)}
        <li class="vh-card flex flex-col gap-2 rounded-md border border-border bg-surface p-4 wrap-anywhere" data-testid="flight-row" data-task={row.task}>
          <p class="font-medium">
            <a class="inline-flex min-h-11 items-center text-accent hover:underline" href={row.href} data-testid="flight-task">{row.task} {row.title}</a>
          </p>
          {#if marksOf(marks, row.task).length > 0}
            <TagChips marks={marksOf(marks, row.task)} ref={row.task} />
          {/if}
          <ul class="flex flex-col gap-2">
            {#each row.leases as lease (lease.lease)}
              <li class="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-sm" data-testid="flight-lease" data-lease={lease.lease}>
                <span class="font-medium" data-testid="lease-holder">{lease.holder}</span>
                <span data-testid="lease-intent">{lease.intent}</span>
                <span class="text-text-muted" data-testid="lease-ttl">{lease.ttl}</span>
              </li>
            {/each}
          </ul>
        </li>
      {/each}
    </ul>
  {/if}
{/snippet}

{#snippet recentBody()}
  <SegmentedRadio legend="Window" name="now-window" options={WINDOWS} value={view.window} onchoose={chooseWindow} testid="now-window" />
  {#if recentList.length === 0}
    <p class="rounded-md border border-dashed border-border px-4 py-4 text-text-muted" data-testid="recent-empty">Nothing was touched in this window.</p>
  {:else}
    <ul class="flex flex-col gap-3" aria-labelledby="band-toggle-recent" data-testid="recent-list">
      {#each recentList as row (row.task)}
        <li class="vh-card flex flex-col gap-1 rounded-md border border-border bg-surface p-4 wrap-anywhere" data-testid="recent-row" data-task={row.task}>
          <p class="font-medium">
            <a class="inline-flex min-h-11 items-center text-accent hover:underline" href={row.href} data-testid="recent-task">{row.task} {row.title}</a>
          </p>
          {#if marksOf(marks, row.task).length > 0}
            <TagChips marks={marksOf(marks, row.task)} ref={row.task} />
          {/if}
          <p class="text-sm text-text-muted" title={absoluteTime(row.at)} data-testid="recent-last">{row.last}</p>
          <p class="text-sm" data-testid="recent-summary">{row.summary}</p>
        </li>
      {/each}
    </ul>
  {/if}
{/snippet}

{#snippet widgetBody(key: WidgetKey)}
  {#if key === 'flight'}
    {@render flightBody()}
  {:else if key === 'recent'}
    {@render recentBody()}
  {:else if view.attention !== undefined}
    {@render bandItems(BANDS.find((band) => band.key === key)!, view.attention)}
  {/if}
{/snippet}

<!-- One card of the narrow accordion: a Needs me band, In flight or Recently touched. Its heading is one 44 px button that
     opens and closes the panel; the open state is the person's choice for narrow screens, else the default. -->
{#snippet fold(key: FoldKey, title: string, count: number, testid: string, toggleId: string, countId: string, level: 'h3' | 'h4')}
  {@const open = isOpen(prefs, key, layout, count)}
  <section class="min-w-0 rounded-lg border border-border bg-surface" data-testid={testid} data-open={open}>
    <svelte:element this={level}>
      <button
        type="button"
        id="band-toggle-{key}"
        class="flex min-h-11 w-full items-center gap-2 rounded-lg px-4 py-2 text-left text-base font-semibold"
        aria-expanded={open}
        aria-controls="band-panel-{key}"
        data-testid={toggleId}
        onclick={() => toggle(key, !open)}
      >
        <svg viewBox="0 0 24 24" class="size-4 shrink-0 transition-transform {open ? 'rotate-90' : ''}" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M9 6l6 6-6 6" /></svg>
        <span>{title}</span>
        <span class="text-sm font-normal text-text-muted" data-testid={countId}>{count}</span>
      </button>
    </svelte:element>
    <div id="band-panel-{key}" role="region" aria-labelledby="band-toggle-{key}" hidden={!open} class="flex flex-col gap-3 px-4 pb-4" data-testid="band-panel">
      {@render widgetBody(key)}
    </div>
  </section>
{/snippet}

<section aria-labelledby="now-heading" class="vh-dense mx-auto w-full max-w-[2560px] pb-16 md:pb-0" data-testid="now-view">
  <h2 id="now-heading" class="mb-4 text-xl font-bold">Now</h2>

  <div class="mb-4" data-testid="now-filters">
    <ChipFilter tags={tagChoices} initiatives={initiativeSlugs} value={view.filter} onchange={setChips} />
  </div>

  <div class="mb-4 flex flex-wrap items-end gap-3">
    <div data-testid="now-sort">
      <SortControl columns={NOW_COLUMNS} choices={NOW_CHOICES} sort={orderToSort(order)} onsort={chooseOrder} />
    </div>
    {#if docked}
      <button
        type="button"
        class="min-h-11 rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text disabled:cursor-not-allowed disabled:opacity-60"
        disabled={!arranged}
        data-testid="reset-layout"
        onclick={reset}>Reset layout</button
      >
    {/if}
  </div>

  {#if view.error !== undefined}
    <p role="alert" class="mb-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">{view.error}</p>
  {/if}

  {#if view.attention === undefined}
    {#if view.error === undefined}
      <div aria-busy="true" class="flex flex-col gap-3" data-testid="now-skeleton">
        <span class="sr-only">Loading what needs attention…</span>
        {#each [0, 1, 2] as n (n)}
          <div class="h-16 animate-pulse rounded-md bg-border"></div>
        {/each}
      </div>
    {/if}
  {:else}
    {@const data = view.attention}
    {#if docked}
      <NowDock layout={dock} count={(key) => widgetCount(key, data)} open={(key) => widgetOpen(dock, key)} onchange={arrange} ontoggle={toggleWidget} body={widgetBody} />
    {:else}
      <div class="grid gap-6">
        <section aria-labelledby="needs-heading" class="min-w-0" data-testid="now-needs">
          <h3 id="needs-heading" class="mb-3 text-lg font-semibold">Needs me</h3>
          <div class="flex flex-col gap-3" data-testid="now-bands">
            {#each BANDS as band (band.key)}
              {@const count = bandCount(data.counts, band)}
              {@render fold(band.key, band.title, count, `band-${band.key}`, 'band-toggle', 'band-count', 'h4')}
            {/each}
          </div>
        </section>
        <section aria-label="In flight and recently touched" class="flex min-w-0 flex-col gap-3" data-testid="now-activity">
          {@render fold('flight', 'In flight', flightList.length, 'now-flight', 'widget-toggle', 'flight-count', 'h3')}
          {@render fold('recent', 'Recently touched', recentList.length, 'now-recent', 'widget-toggle', 'recent-count', 'h3')}
        </section>
      </div>
    {/if}
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
