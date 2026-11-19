<script lang="ts">
  // UX prototype: "Now" (was Attention). Left, what needs the operator, in bands; right, what is in flight
  // (one row per task with a live lease, leases nested) and, dimmed, what was touched lately.
  // Every capability of the Attention view stays: chip filter in the fragment and saved, per-section actions
  // with their dialogs, copy way back, initiative links, live refresh, skeleton, errors, toast.
  import { replaceHash } from './lib/historyEntry';
  import AnswerForm from './AnswerForm.svelte';
  import ChipFilter from './ChipFilter.svelte';
  import DensitySwitch from './DensitySwitch.svelte';
  import Dialog from './Dialog.svelte';
  import InitiativeConfirm from './InitiativeConfirm.svelte';
  import ProtoSort from './ProtoSort.svelte';
  import TagChips from './TagChips.svelte';
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
  import { onDestroy, untrack } from 'svelte';
  import { attentionHash, attentionBadges, sectionCount, type AttentionFilter } from './lib/attention';
  import {
    AttentionApi,
    AttentionStore,
    affectsAttention,
    copyLabel,
    forgetAttention,
    openAttention,
    saveAttention,
    startFilter,
    type AttentionState,
    type ItemActor,
  } from './lib/attentionView';
  import { copyText } from './lib/clipboard';
  import { InitiativesApi, type TaskListItem } from './lib/initiatives';
  import { live } from './lib/live';
  import { toneClasses } from './lib/presentation';
  import { eventsSince, isWindow, lastTouches, shortSpan, WINDOW_MS, WINDOWS, type RecentWindow } from './lib/proto/activity';
  import { BANDS, bandRows, flightAt, isNowSort, leaseStarts, NOW_SORTS, sortNow, type BandKey, type NowSort } from './lib/proto/now';
  import { isChipState, loadPref, manualTagsOf, prefs, savePref } from './lib/proto/prefs.svelte';
  import { actorKind, passesChips, tagChoices, tagHue, taskTags, type ChipState, type TaskTag } from './lib/proto/tags';
  import { resolveRoute, ROUTES } from './lib/routes';
  import { currentBrowser } from './lib/session';
  import { TasksApi, type Resolution } from './lib/tasks';
  import { currentViewEnv, FilterTurns } from './lib/viewState';
  import type { components } from './lib/api/schema';

  type BoardEvent = components['schemas']['Event'];

  let { csrf }: { csrf: string; param?: string } = $props();

  const browser = currentBrowser();
  const viewEnv = currentViewEnv();
  const opened = openAttention(viewEnv, window.location.hash);
  const initial = opened.filter;
  let view: AttentionState = $state({ attention: undefined, filter: initial, loading: true, error: undefined });
  let known: { slug: string; tags: string[] }[] = $state([]);
  let looked: { slug: string; tags: string[] }[] | undefined;
  let toast = $state('');

  // Tags that are not initiative tags (agent kinds, your own tags) are filtered here, not by the server.
  let clientTags: string[] = $state(loadPref('now.clientTags', [] as string[], (v) => Array.isArray(v)));
  let sort: NowSort = $state(loadPref('now.sort', 'newest' as NowSort, isNowSort));
  let recentWindow: RecentWindow = $state(loadPref('now.window', '24h' as RecentWindow, isWindow));

  const store = new AttentionStore(new AttentionApi(browser), (s) => (view = s), initial);
  let acting: ActionsState = $state(NO_ACTIONS);
  const actions = new AttentionActions(new ActionsApi(browser, () => csrf), (s) => (acting = s));

  // In flight and recently touched: every task (the board is small) and the window's events.
  const tasksApi = new TasksApi(browser, () => csrf);
  let tasks: TaskListItem[] = $state([]);
  let tasksLoaded = $state(false);
  let events: BoardEvent[] = $state([]);
  let eventsFrom = 0; // the window start the events were read from (ms)
  let flightError: string | undefined = $state(undefined);
  let now = $state(new Date());

  async function loadTasks() {
    try {
      tasks = await tasksApi.listQuery(new URLSearchParams({ status: 'all' }));
      tasksLoaded = true;
      flightError = undefined;
    } catch (e) {
      flightError = e instanceof Error ? e.message : String(e);
    }
  }

  async function loadEvents() {
    const from = Date.now() - WINDOW_MS[recentWindow];
    try {
      if (from >= eventsFrom && eventsFrom > 0 && events.length > 0) {
        // Only what was written since the last event read.
        const more = await eventsSince(browser, events[events.length - 1]!.id);
        if (more.length > 0) events = [...events, ...more];
      } else {
        events = await eventsSince(browser, new Date(from));
        eventsFrom = from;
      }
    } catch (e) {
      flightError = e instanceof Error ? e.message : String(e);
    }
  }

  const turns = new FilterTurns();
  onDestroy(() => turns.end());
  let listed: Promise<void> = Promise.resolve();
  async function start() {
    const current = turns.begin();
    listed = new InitiativesApi(browser, () => csrf)
      .list('all')
      .then((list) => {
        looked = list.map((i) => ({ slug: i.slug, tags: i.tags }));
        known = looked;
      })
      .catch(() => undefined);
    void loadTasks();
    void loadEvents();
    if (opened.restored) await listed;
    if (!current()) return;
    turns.reading();
    const kept = startFilter(opened, looked);
    if (kept === initial) {
      void store.load();
    } else {
      persist(kept);
      void store.setFilter(kept);
    }
  }

  $effect(() => {
    void untrack(start);
    const offA = live().subscribe(affectsAttention, () => (turns.hasRead() ? store.load() : undefined));
    const offB = live().subscribe(
      () => true,
      () => {
        now = new Date();
        void loadTasks();
        void loadEvents();
      },
    );
    // Time left on leases ticks.
    const tick = setInterval(() => (now = new Date()), 30_000);
    return () => {
      offA();
      offB();
      clearInterval(tick);
    };
  });

  function persist(filter: AttentionFilter) {
    replaceHash(attentionHash(filter));
    saveAttention(viewEnv.storage, filter);
  }

  function apply(filter: AttentionFilter) {
    turns.begin();
    turns.reading();
    persist(filter);
    void store.setFilter(filter);
  }

  function clear() {
    turns.begin();
    turns.reading();
    forgetAttention(viewEnv.storage);
    replaceHash('#/now');
    clientTags = [];
    savePref('now.clientTags', []);
    void store.setFilter({ initiatives: [], tags: [] });
  }

  async function hashChanged() {
    if (resolveRoute(window.location.hash, ROUTES).id !== 'now') return;
    const next = openAttention(viewEnv, window.location.hash);
    const current = turns.begin();
    if (next.restored) await listed;
    if (!current()) return;
    const filter = startFilter(next, looked);
    if (filter !== next.filter) persist(filter);
    if (!turns.mustRead(attentionHash(filter) === attentionHash(view.filter))) return;
    turns.reading();
    void store.setFilter(filter);
  }

  async function copy(actor: ItemActor) {
    const ok = await copyText(navigator.clipboard, actor.wayBack ?? '');
    toast = ok ? `Copied the way back of ${actor.handle}` : 'Copying failed. Select the way back and copy it by hand.';
  }

  async function perform(kind: ActionKind, target: string, input = '') {
    const done = await actions.run(kind, target, input);
    if (done !== undefined) toast = done;
  }

  // The chips: initiative tags go to the server filter, the rest are filtered here.
  const initiativeTags = $derived(new Map(known.map((i) => [i.slug, i.tags] as const)));
  const serverTagSet = $derived(new Set(known.flatMap((i) => i.tags)));
  const tagsOf = (task: TaskListItem): TaskTag[] => taskTags(task, initiativeTags, manualTagsOf(task.ref));
  const byRef = $derived(new Map(tasks.map((t) => [t.ref, t] as const)));
  const chipValue: ChipState = $derived({ tags: [...view.filter.tags, ...clientTags], initiatives: view.filter.initiatives });
  const choices = $derived.by(() => {
    void prefs.manual;
    return tagChoices(
      tasks.filter((t) => t.status !== 'closed').map(tagsOf),
      known.map((i) => i.tags),
      chipValue.tags,
    );
  });
  const initiativeChoices = $derived([...new Set([...known.map((i) => i.slug), ...view.filter.initiatives])].sort());

  function setChips(next: ChipState) {
    if (next.tags.length + next.initiatives.length === 0) return clear();
    const server = next.tags.filter((t) => serverTagSet.has(t));
    clientTags = next.tags.filter((t) => !serverTagSet.has(t));
    savePref('now.clientTags', clientTags);
    apply({ tags: server, initiatives: next.initiatives });
  }

  const tagNames = (task: TaskListItem | undefined) => (task === undefined ? [] : tagsOf(task).map((t) => t.tag));
  // A Needs-me row passes the client tags when one of its tasks carries them all.
  const rowPasses = (refs: readonly string[]) =>
    clientTags.length === 0 || refs.some((ref) => passesChips(tagNames(byRef.get(ref)), undefined, { tags: clientTags, initiatives: [] }));
  const taskPasses = (task: TaskListItem) => passesChips(tagNames(task), task.initiative, chipValue);

  const starts = $derived(leaseStarts(tasks));
  const bands = $derived.by(() => {
    void prefs.manual;
    const data = view.attention;
    if (data === undefined) return [];
    return BANDS.map((band) => {
      const rows = sortNow(
        bandRows(band, data, now, starts).filter((r) => rowPasses(r.refs)),
        sort,
      );
      const total = band.sections.reduce((n, s) => n + sectionCount(data.counts, s), 0);
      return { band, rows, total };
    });
  });

  const flight = $derived.by(() => {
    void prefs.manual;
    const rows = tasks.filter((t) => t.live_leases.length > 0 && taskPasses(t)).map((t) => ({ task: t, at: flightAt(t), actor: t.live_leases[0]?.actor ?? '' }));
    return sortNow(rows, sort);
  });

  const touches = $derived(lastTouches(events, now.getTime() - WINDOW_MS[recentWindow]));
  const recent = $derived.by(() => {
    void prefs.manual;
    const rows = tasks
      .filter((t) => t.live_leases.length === 0 && touches.has(t.ref) && taskPasses(t))
      .map((t) => {
        const touch = touches.get(t.ref)!;
        return { task: t, at: touch.at, actor: touch.actor, touch };
      });
    return sortNow(rows, sort);
  });

  function setWindow(w: RecentWindow) {
    recentWindow = w;
    savePref('now.window', w);
    void loadEvents();
  }

  function setSort(v: string) {
    if (!isNowSort(v)) return;
    sort = v as NowSort;
    savePref('now.sort', sort);
  }

  // Phones: every band is an accordion; asks and expired open by default when they have rows. Desktop: all open.
  const wide = typeof window.matchMedia === 'function' && window.matchMedia('(min-width: 1024px)').matches;
  let opens: Record<string, boolean | undefined> = $state({});
  const isOpen = (key: string, count: number) => opens[key] ?? ((wide && key !== 'done') || ((key === 'asks' || key === 'stuck') && count > 0));
  const toggleBand = (key: string, e: Event) => (opens = { ...opens, [key]: (e.currentTarget as HTMLDetailsElement).open });

  const timeLeft = (expires: string) => Date.parse(expires) - now.getTime();
  const ACK_BODY = 'The lease ran out without being ended. Acknowledging it records that you have seen it and takes it off Expired leases.';
  const action = 'min-h-11 min-w-11 rounded-md border border-border bg-surface-raised px-2.5 text-xs font-medium text-text hover:border-accent';
  const tile = 'rounded-lg border border-border bg-surface-raised';
  const summary = 'flex min-h-11 cursor-pointer list-none items-center gap-2 px-3 py-1.5 select-none [&::-webkit-details-marker]:hidden';
  const sectionLabel: Record<string, string> = { expired_leases: 'expired', conflicts: 'conflict' };
  const needsTotal = $derived(bands.reduce((n, b) => n + (b.band.muted ? 0 : b.total), 0));
</script>

<svelte:window onhashchange={hashChanged} />

{#snippet chevron(open: boolean)}
  <svg viewBox="0 0 24 24" class="size-4 shrink-0 text-text-muted transition-transform {open ? 'rotate-90' : ''}" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M9 6l6 6-6 6" /></svg>
{/snippet}

<section aria-labelledby="now-heading" class="vh-dense mx-auto w-full max-w-[2560px] pb-16 md:pb-0" data-testid="attention-view">
  <div class="mb-3 flex flex-wrap items-center gap-x-4 gap-y-2">
    <h2 id="now-heading" class="text-xl font-bold">Now</h2>
    <span class="text-sm text-text-muted" data-testid="now-summary">
      {needsTotal} need{needsTotal === 1 ? 's' : ''} you · {flight.length} in flight
    </span>
    <div class="ml-auto flex flex-wrap items-center gap-3">
      <ProtoSort options={NOW_SORTS} value={sort} onchange={setSort} />
      <DensitySwitch />
    </div>
  </div>

  {#if choices.length + initiativeChoices.length > 0}
    <div class="mb-3 rounded-lg border border-border bg-surface-raised px-3 py-2" data-testid="attention-filters">
      <ChipFilter tags={choices} initiatives={initiativeChoices} value={chipValue} onchange={setChips} />
    </div>
  {/if}

  {#if view.error !== undefined}
    <p role="alert" class="mb-3 rounded-md border border-danger px-3 py-2 text-sm text-danger">{view.error}</p>
  {/if}

  <div class="grid grid-cols-1 gap-4 lg:grid-cols-2 lg:items-start" data-testid="now-columns">
    <!-- Needs me -->
    <div class="flex min-w-0 flex-col gap-2" data-testid="needs-me">
      <h3 class="flex items-baseline gap-2 text-sm font-semibold tracking-wide text-text-muted uppercase">Needs me</h3>
      {#if view.attention === undefined}
        {#if view.error === undefined}
          <div aria-busy="true" class="flex flex-col gap-2" data-testid="attention-skeleton">
            <span class="sr-only">Loading what needs attention…</span>
            {#each [0, 1, 2] as n (n)}
              <div class="h-12 animate-pulse rounded-md bg-border"></div>
            {/each}
          </div>
        {/if}
      {:else}
        {#each bands as { band, rows, total } (band.key)}
          {@const open = isOpen(band.key, rows.length)}
          <details class="{tile} {band.muted ? 'opacity-70' : ''} {total > 0 && !band.muted ? 'border-l-4 border-l-warning' : ''}" {open} ontoggle={(e) => toggleBand(band.key, e)} data-testid="band-{band.key}">
            <summary class={summary}>
              {@render chevron(open)}
              <span class="font-semibold">{band.title}</span>
              <span
                class="rounded-full px-2 text-xs font-semibold {total > 0 && !band.muted ? 'bg-warning text-surface-raised' : 'border border-border text-text-muted'}"
                data-testid="section-count">{rows.length === total ? total : `${rows.length}/${total}`}</span
              >
              <span class="hidden truncate text-xs text-text-muted sm:inline">{band.hint}</span>
            </summary>
            <div class="border-t border-border px-2 py-2">
              {#if rows.length === 0}
                <p class="px-1 py-1 text-sm text-text-muted" data-testid="section-empty">{band.empty}</p>
              {:else}
                <ul class="vh-gap flex flex-col gap-2">
                  {#each rows as row (row.section + row.id)}
                    {@const initiative = row.initiative}
                    {@const task = row.refs.length === 1 ? byRef.get(row.refs[0]!) : undefined}
                    <li class="vh-card flex flex-col gap-1.5 rounded-md border border-border bg-surface p-3 wrap-anywhere" data-testid="attention-item">
                      <div class="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                        {#if band.sections.length > 1}
                          <span class="rounded border px-1 text-[0.7rem] uppercase {row.section === 'conflicts' ? 'border-danger text-danger' : 'border-warning text-warning'}">{sectionLabel[row.section]}</span>
                        {/if}
                        {#if row.href !== undefined}
                          <a class="inline-flex min-h-11 items-center font-medium text-accent hover:underline" href={row.href} data-testid="item-link">{row.title}</a>
                        {:else}
                          <span class="font-medium" data-testid="item-title">{row.title}</span>
                        {/if}
                        {#if initiative !== undefined}
                          <a class="inline-flex min-h-11 items-center text-xs text-text-muted hover:text-accent hover:underline" href={initiativeHref(initiative)} data-testid="initiative-link">{initiative}</a>
                        {/if}
                        {#if task !== undefined}<TagChips tags={tagsOf(task)} />{/if}
                      </div>
                      {#if row.details.length > 0}
                        <p class="text-xs text-text-muted" data-testid="item-details">{row.details.join(' · ')}</p>
                      {/if}
                      <ul class="flex flex-col gap-0.5">
                        {#each row.actors as actor, n (n)}
                          <li class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs" data-testid="item-actor">
                            <span class="text-text-muted">{actor.role}</span>
                            <span class="font-medium" data-testid="actor-handle">{actor.handle}</span>
                            {#if actor.wayBack !== undefined}
                              <code class="font-mono text-text-muted" data-testid="actor-way-back">{actor.wayBack}</code>
                              <button type="button" class="{action} min-h-7!" aria-label={copyLabel(actor)} data-testid="copy-way-back" onclick={() => void copy(actor)}>Copy</button>
                            {:else}
                              <span class="text-text-muted">no way back on record</span>
                            {/if}
                          </li>
                        {/each}
                      </ul>
                      {#if acting.itemError?.target === row.id}
                        <p role="alert" class="rounded-md border border-danger px-3 py-1 text-sm text-danger" data-testid="item-error">{acting.itemError.text}</p>
                      {/if}
                      {#if actionsFor(row.section).length > 0}
                        <div class="flex flex-wrap gap-1.5" data-testid="item-actions">
                          {#each actionsFor(row.section) as kind (kind)}
                            <button type="button" class={action} aria-label={actionName(kind, row.id)} data-testid="action-{kind}" onclick={() => void actions.open(kind, row.id)}>{actionLabel(kind)}</button>
                          {/each}
                        </div>
                      {/if}
                    </li>
                  {/each}
                </ul>
              {/if}
            </div>
          </details>
        {/each}
      {/if}
    </div>

    <!-- In flight -->
    <div class="flex min-w-0 flex-col gap-2" data-testid="in-flight">
      <h3 class="text-sm font-semibold tracking-wide text-text-muted uppercase">In flight</h3>
      {#if flightError !== undefined}
        <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger">{flightError}</p>
      {/if}
      {#if !tasksLoaded && flightError === undefined}
        <div aria-busy="true" class="flex flex-col gap-2"><span class="sr-only">Loading what is in flight…</span><div class="h-24 animate-pulse rounded-md bg-border"></div></div>
      {:else}
        {@const openF = opens['flight'] ?? (wide || flight.length > 0)}
        <details class={tile} open={openF} ontoggle={(e) => toggleBand('flight', e)} data-testid="band-flight">
          <summary class={summary}>
            {@render chevron(openF)}
            <span class="font-semibold">Live leases</span>
            <span class="rounded-full bg-success px-2 text-xs font-semibold text-surface-raised">{flight.length}</span>
            <span class="hidden text-xs text-text-muted sm:inline">one row per task, its leases nested</span>
          </summary>
          <div class="border-t border-border px-2 py-2">
            {#if flight.length === 0}
              <p class="px-1 py-1 text-sm text-text-muted">Nothing is in flight.</p>
            {:else}
              <ul class="vh-gap flex flex-col gap-2">
                {#each flight as { task } (task.ref)}
                  <li class="vh-card flex flex-col gap-1.5 rounded-md border border-border bg-surface p-3 wrap-anywhere" data-testid="flight-task">
                    <div class="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                      <a class="inline-flex min-h-11 items-center font-mono text-sm text-accent hover:underline" href="#/t/{encodeURIComponent(task.ref)}">{task.ref}</a>
                      <span class="font-medium">{task.title}</span>
                      <a class="inline-flex min-h-11 items-center text-xs text-text-muted hover:text-accent hover:underline" href={initiativeHref(task.initiative)}>{task.initiative}</a>
                      {#each attentionBadges(task.attention) as b (b.reason)}
                        <span class="rounded-md border px-1.5 text-[0.7rem] {toneClasses(b.tone)}">{b.label}</span>
                      {/each}
                      <TagChips tags={tagsOf(task)} taskRef={task.ref} editable />
                    </div>
                    <ul class="flex flex-col gap-1 border-l-2 border-success/60 pl-2">
                      {#each task.live_leases as lease (lease.ref)}
                        {@const left = timeLeft(lease.expires_at)}
                        {@const kind = actorKind(lease.actor)}
                        <li class="flex flex-col gap-0.5 text-xs" data-testid="flight-lease">
                          <div class="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                            <span class="font-mono text-text-muted">{lease.ref}</span>
                            {#if kind !== undefined}<span class="vh-tag" style="--h:{tagHue(kind)}" data-source="actor">{kind}</span>{/if}
                            <span class="font-medium">{lease.actor}</span>
                            <span class="ml-auto font-medium {left < 10 * 60_000 ? 'text-danger' : left < 30 * 60_000 ? 'text-warning' : 'text-text-muted'}" title="expires {lease.expires_at}"
                              >{left > 0 ? `${shortSpan(left)} left` : 'due now'}</span
                            >
                          </div>
                          <p class="text-text">{lease.intent}</p>
                          {#if lease.resources.length > 0}
                            <div class="flex flex-wrap gap-1">
                              {#each lease.resources as res (res)}
                                <code class="rounded border border-border px-1 font-mono text-[0.7rem] text-text-muted">{res}</code>
                              {/each}
                            </div>
                          {/if}
                        </li>
                      {/each}
                    </ul>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        </details>

        <details class="{tile} opacity-80" open={opens['recent'] ?? wide} ontoggle={(e) => toggleBand('recent', e)} data-testid="band-recent">
          <summary class={summary}>
            {@render chevron(opens['recent'] ?? wide)}
            <span class="font-semibold">Recently touched</span>
            <span class="rounded-full border border-border px-2 text-xs text-text-muted">{recent.length}</span>
            <span class="ml-auto inline-flex overflow-hidden rounded-md border border-border text-xs" role="group" aria-label="Window">
              {#each WINDOWS as w (w)}
                <button
                  type="button"
                  class="px-2 py-0.5 text-text-muted aria-pressed:bg-accent aria-pressed:font-semibold aria-pressed:text-surface-raised"
                  aria-pressed={recentWindow === w}
                  onclick={(e) => {
                    e.preventDefault();
                    setWindow(w);
                  }}>{w}</button
                >
              {/each}
            </span>
          </summary>
          <div class="border-t border-border px-2 py-2">
            {#if recent.length === 0}
              <p class="px-1 py-1 text-sm text-text-muted">Nothing else was touched in the last {recentWindow}.</p>
            {:else}
              <ul class="flex flex-col divide-y divide-border">
                {#each recent as { task, touch } (task.ref)}
                  <li class="flex flex-col gap-0.5 px-1 py-1.5 text-sm text-text-muted wrap-anywhere" data-testid="recent-task">
                    <div class="flex flex-wrap items-baseline gap-x-2">
                      <a class="inline-flex min-h-11 items-center font-mono text-accent hover:underline" href="#/t/{encodeURIComponent(task.ref)}">{task.ref}</a>
                      <span class="text-text">{task.title}</span>
                      <span class="rounded border border-border px-1 text-[0.7rem]">{task.status}</span>
                      <TagChips tags={tagsOf(task)} />
                      <span class="ml-auto text-xs" title={new Date(touch.at).toISOString()}>{shortSpan(now.getTime() - touch.at)} ago</span>
                    </div>
                    <p class="truncate text-xs">{touch.actor}: {touch.summary}</p>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        </details>
      {/if}
    </div>
  </div>
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
