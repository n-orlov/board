<script lang="ts">
  // UX prototype: "Now" (was Attention). Left, what needs the operator, in bands; right, what is in flight
  // (one row per task with a live lease, leases nested) and, dimmed, what was touched lately.
  // Every capability of the Attention view stays: chip filter in the fragment and saved, per-section actions
  // with their dialogs, copy way back, initiative links, live refresh, skeleton, errors, toast.
  import { replaceHash } from './lib/historyEntry';
  import AnswerForm from './AnswerForm.svelte';
  import ChipFilter from './ChipFilter.svelte';
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
  import {
    defaultLayout,
    isDefaultLayout,
    locate,
    MAX_COLUMNS,
    moveWidget,
    normalizeLayout,
    nudgeWidget,
    resizeColumns,
    setHeight,
    WIDGET_TITLES,
    type Drop,
    type NowLayout,
    type WidgetKey,
  } from './lib/proto/nowLayout';
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

  // Dock layout (desktop only): columns of widgets you can drag between, columns you can resize, widgets
  // you can cap in height. Phones keep the stacked accordion in the default order.
  const lgQuery = typeof window.matchMedia === 'function' ? window.matchMedia('(min-width: 1024px)') : undefined;
  let lg = $state(lgQuery?.matches ?? false);
  $effect(() => {
    if (lgQuery === undefined) return;
    const on = (e: MediaQueryListEvent) => (lg = e.matches);
    lgQuery.addEventListener('change', on);
    return () => lgQuery.removeEventListener('change', on);
  });
  let layout: NowLayout = $state(normalizeLayout(loadPref<unknown>('now.layout', null)));
  const layoutIsDefault = $derived(isDefaultLayout(layout));
  const bandByKey = $derived(new Map(bands.map((b) => [b.band.key as WidgetKey, b] as const)));
  let dockEl: HTMLElement | undefined = $state(undefined);

  function commit(next: NowLayout) {
    layout = next;
    savePref('now.layout', next);
  }
  function resetLayout() {
    commit(defaultLayout());
  }

  /** Follows one pointer gesture on the window until release, cancel or Escape. */
  function track(e: PointerEvent, cursor: string, move: (ev: PointerEvent) => void, end: (ok: boolean) => void) {
    e.preventDefault();
    const root = document.documentElement;
    root.style.cursor = cursor;
    root.style.userSelect = 'none';
    const onMove = (ev: PointerEvent) => move(ev);
    const onUp = () => finish(true);
    const onCancel = () => finish(false);
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === 'Escape') finish(false);
    };
    function finish(ok: boolean) {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', onCancel);
      window.removeEventListener('keydown', onKey);
      root.style.cursor = '';
      root.style.userSelect = '';
      end(ok);
    }
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', onCancel);
    window.addEventListener('keydown', onKey);
  }

  // Dragging a widget by its grip: a ghost follows the pointer, a line shows where it lands.
  let drag: { key: WidgetKey; x: number; y: number; active: boolean; drop: Drop | undefined } | undefined = $state(undefined);
  const EDGE = 36; // px of the dock's left and right edge that create a new column

  function dropAt(key: WidgetKey, x: number, y: number): Drop | undefined {
    if (dockEl === undefined) return undefined;
    const box = dockEl.getBoundingClientRect();
    const from = locate(layout, key);
    if (from === undefined) return undefined;
    const alone = layout.columns[from.col]!.widgets.length === 1;
    if (layout.columns.length < MAX_COLUMNS && y >= box.top - 40 && y <= box.bottom + 40) {
      if (x < box.left + EDGE && !(alone && from.col === 0)) return { kind: 'new', side: 'left' };
      if (x > box.right - EDGE && !(alone && from.col === layout.columns.length - 1)) return { kind: 'new', side: 'right' };
    }
    const cols = [...dockEl.querySelectorAll<HTMLElement>(':scope > [data-dock-col]')];
    let col = -1;
    let best = Infinity;
    cols.forEach((el, i) => {
      const r = el.getBoundingClientRect();
      const d = x < r.left ? r.left - x : x > r.right ? x - r.right : 0;
      if (d < best) [best, col] = [d, i];
    });
    if (col < 0) return undefined;
    const items = [...cols[col]!.querySelectorAll<HTMLElement>(':scope > [data-widget]')];
    let index = items.findIndex((el) => {
      const r = el.getBoundingClientRect();
      return y < r.top + Math.min(r.height / 2, 60);
    });
    if (index < 0) index = items.length;
    if (col === from.col && (index === from.index || index === from.index + 1)) return undefined; // lands where it is
    return { kind: 'col', col, index };
  }

  function startDrag(e: PointerEvent, key: WidgetKey) {
    if (e.button !== 0) return;
    const x0 = e.clientX;
    const y0 = e.clientY;
    drag = { key, x: x0, y: y0, active: false, drop: undefined };
    track(
      e,
      'grabbing',
      (ev) => {
        if (drag === undefined) return;
        const active = drag.active || Math.hypot(ev.clientX - x0, ev.clientY - y0) > 4;
        drag = { key, x: ev.clientX, y: ev.clientY, active, drop: active ? dropAt(key, ev.clientX, ev.clientY) : undefined };
      },
      (ok) => {
        if (ok && drag?.active && drag.drop !== undefined) commit(moveWidget(layout, key, drag.drop));
        drag = undefined;
      },
    );
  }

  function gripKey(e: KeyboardEvent, key: WidgetKey) {
    const dir = ({ ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right' } as const)[e.key as 'ArrowUp'];
    if (dir === undefined) return;
    e.preventDefault();
    commit(nudgeWidget(layout, key, dir));
    requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-testid="grip-${key}"]`)?.focus());
  }

  // The splitter between two columns moves their shared edge.
  let splitting: number | undefined = $state(undefined);
  function startSplit(e: PointerEvent, left: number) {
    if (e.button !== 0 || dockEl === undefined) return;
    const avail = [...dockEl.querySelectorAll<HTMLElement>(':scope > [data-dock-col]')].reduce((n, el) => n + el.getBoundingClientRect().width, 0);
    const start = layout;
    const x0 = e.clientX;
    splitting = left;
    track(
      e,
      'col-resize',
      (ev) => (layout = resizeColumns(start, left, (ev.clientX - x0) / avail)),
      (ok) => {
        splitting = undefined;
        if (ok) commit(layout);
        else layout = start;
      },
    );
  }
  function evenSplit(left: number) {
    const a = layout.columns[left]!.width;
    const b = layout.columns[left + 1]!.width;
    commit(resizeColumns(layout, left, (b - a) / 2));
  }

  // The bottom edge of a widget caps its height; the body scrolls. Double-click goes back to auto.
  let sizing: WidgetKey | undefined = $state(undefined);
  function startHeight(e: PointerEvent, key: WidgetKey) {
    if (e.button !== 0) return;
    const body = (e.currentTarget as HTMLElement).closest('details')?.querySelector<HTMLElement>('[data-body]');
    if (body === null || body === undefined) return;
    const h0 = body.getBoundingClientRect().height;
    const y0 = e.clientY;
    const start = layout;
    sizing = key;
    track(
      e,
      'ns-resize',
      (ev) => (layout = setHeight(start, key, h0 + ev.clientY - y0)),
      (ok) => {
        sizing = undefined;
        if (ok) commit(layout);
        else layout = start;
      },
    );
  }
  const bodyStyle = (key: WidgetKey) => {
    const h = lg ? layout.heights[key] : undefined;
    return h === undefined ? undefined : `max-height:${h}px;overflow:auto`;
  };
</script>

<svelte:window onhashchange={hashChanged} />

{#snippet chevron(open: boolean)}
  <svg viewBox="0 0 24 24" class="size-4 shrink-0 text-text-muted transition-transform {open ? 'rotate-90' : ''}" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M9 6l6 6-6 6" /></svg>
{/snippet}

{#snippet grip(key: WidgetKey)}
  {#if lg}
    <button
      type="button"
      class="-ml-1.5 inline-flex h-7 w-5 shrink-0 cursor-grab touch-none items-center justify-center rounded text-text-muted hover:bg-border/60 hover:text-text focus-visible:outline-2 focus-visible:outline-accent active:cursor-grabbing"
      aria-label="Move {WIDGET_TITLES[key]} (arrow keys)"
      title="Drag to move or dock beside another widget"
      data-testid="grip-{key}"
      onpointerdown={(e) => startDrag(e, key)}
      onkeydown={(e) => gripKey(e, key)}
      onclick={(e) => e.preventDefault()}
    >
      <svg viewBox="0 0 10 16" class="h-4 w-2.5" fill="currentColor" aria-hidden="true">
        {#each [2, 6, 10, 14] as cy (cy)}<circle cx="2.5" {cy} r="1.3" /><circle cx="7.5" {cy} r="1.3" />{/each}
      </svg>
    </button>
  {/if}
{/snippet}

{#snippet sizer(key: WidgetKey)}
  {#if lg}
    {@const capped = layout.heights[key] !== undefined}
    <div
      role="separator"
      aria-orientation="horizontal"
      aria-label="Resize {WIDGET_TITLES[key]}"
      title={capped ? 'Drag to resize; double-click for auto height' : 'Drag to cap the height'}
      class="group flex h-2.5 cursor-ns-resize touch-none items-center justify-center rounded-b-lg border-t border-border/60 hover:bg-accent/10"
      data-testid="sizer-{key}"
      onpointerdown={(e) => startHeight(e, key)}
      ondblclick={() => commit(setHeight(layout, key, undefined))}
    >
      <span class="h-0.5 w-10 rounded-full {sizing === key ? 'bg-accent' : capped ? 'bg-text-muted/70' : 'bg-border'} group-hover:bg-accent"></span>
    </div>
  {/if}
{/snippet}

{#snippet skeleton(key: WidgetKey)}
  <div aria-busy="true" class="flex flex-col gap-2" data-testid={key === 'asks' ? 'attention-skeleton' : undefined}>
    <span class="sr-only">Loading {WIDGET_TITLES[key]}…</span>
    <div class="h-12 animate-pulse rounded-md bg-border"></div>
  </div>
{/snippet}

{#snippet widget(key: WidgetKey)}
  {#if key === 'flight'}
    {#if !tasksLoaded}
      {#if flightError === undefined}{@render skeleton(key)}{/if}
    {:else}
      {@const openF = opens['flight'] ?? (wide || flight.length > 0)}
      <details class={tile} open={openF} ontoggle={(e) => toggleBand('flight', e)} data-testid="band-flight">
        <summary class={summary}>
          {@render grip(key)}
          {@render chevron(openF)}
          <span class="shrink-0 font-semibold whitespace-nowrap">Live leases</span>
          <span class="rounded-full bg-success px-2 text-xs font-semibold text-surface-raised">{flight.length}</span>
          <span class="hidden truncate text-xs text-text-muted sm:inline">one row per task, its leases nested</span>
        </summary>
        <div class="border-t border-border px-2 py-2" data-body style={bodyStyle(key)}>
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
        {@render sizer(key)}
      </details>
    {/if}
  {:else if key === 'recent'}
    {#if !tasksLoaded}
      {#if flightError === undefined}{@render skeleton(key)}{/if}
    {:else}
      {@const openR = opens['recent'] ?? wide}
      <details class="{tile} opacity-80" open={openR} ontoggle={(e) => toggleBand('recent', e)} data-testid="band-recent">
        <summary class={summary}>
          {@render grip(key)}
          {@render chevron(openR)}
          <span class="shrink-0 font-semibold whitespace-nowrap">Recently touched</span>
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
        <div class="border-t border-border px-2 py-2" data-body style={bodyStyle(key)}>
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
        {@render sizer(key)}
      </details>
    {/if}
  {:else}
    {@const entry = bandByKey.get(key)}
    {#if entry === undefined}
      {#if view.error === undefined}{@render skeleton(key)}{/if}
    {:else}
      {@const { band, rows, total } = entry}
      {@const open = isOpen(band.key, rows.length)}
      <details class="{tile} {band.muted ? 'opacity-70' : ''} {total > 0 && !band.muted ? 'border-l-4 border-l-warning' : ''}" {open} ontoggle={(e) => toggleBand(band.key, e)} data-testid="band-{band.key}">
        <summary class={summary}>
          {@render grip(key)}
          {@render chevron(open)}
          <span class="shrink-0 font-semibold whitespace-nowrap">{band.title}</span>
          <span
            class="rounded-full px-2 text-xs font-semibold {total > 0 && !band.muted ? 'bg-warning text-surface-raised' : 'border border-border text-text-muted'}"
            data-testid="section-count">{rows.length === total ? total : `${rows.length}/${total}`}</span
          >
          <span class="hidden truncate text-xs text-text-muted sm:inline">{band.hint}</span>
        </summary>
        <div class="border-t border-border px-2 py-2" data-body style={bodyStyle(key)}>
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
        {@render sizer(key)}
      </details>
    {/if}
  {/if}
{/snippet}

<section aria-labelledby="now-heading" class="vh-dense mx-auto w-full max-w-[2560px] pb-16 md:pb-0" data-testid="attention-view">
  <div class="mb-3 flex flex-wrap items-center gap-x-4 gap-y-2">
    <h2 id="now-heading" class="text-xl font-bold">Now</h2>
    <span class="text-sm text-text-muted" data-testid="now-summary">
      {needsTotal} need{needsTotal === 1 ? 's' : ''} you · {flight.length} in flight
    </span>
    <div class="ml-auto flex flex-wrap items-center gap-3">
      {#if lg}
        <button
          type="button"
          class="inline-flex min-h-8 items-center gap-1.5 rounded-md border border-border bg-surface-raised px-2.5 text-sm text-text-muted enabled:hover:border-accent enabled:hover:text-text disabled:cursor-default disabled:opacity-50"
          disabled={layoutIsDefault}
          title={layoutIsDefault ? 'The layout is the default' : 'Back to two columns, default order, auto heights'}
          data-testid="reset-layout"
          onclick={resetLayout}
        >
          <svg viewBox="0 0 24 24" class="size-4" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M3 12a9 9 0 1 0 3-6.7L3 8" /><path d="M3 3v5h5" /></svg>
          Reset layout
        </button>
      {/if}
      <ProtoSort options={NOW_SORTS} value={sort} onchange={setSort} />
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
  {#if flightError !== undefined}
    <p role="alert" class="mb-3 rounded-md border border-danger px-3 py-2 text-sm text-danger">{flightError}</p>
  {/if}

  {#if lg}
    <div class="relative flex items-stretch" bind:this={dockEl} data-testid="now-columns">
      {#each layout.columns as col, ci (ci)}
        {#if ci > 0}
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize columns"
            title="Drag to resize; double-click to split evenly"
            class="group relative flex w-4 shrink-0 cursor-col-resize touch-none justify-center"
            data-testid="splitter-{ci - 1}"
            onpointerdown={(e) => startSplit(e, ci - 1)}
            ondblclick={() => evenSplit(ci - 1)}
          >
            <span class="w-0.5 rounded-full transition-colors {splitting === ci - 1 ? 'bg-accent' : 'bg-transparent group-hover:bg-accent/60'}"></span>
          </div>
        {/if}
        <div class="flex min-w-0 flex-col gap-2" style="flex: {col.width} 1 0px" data-dock-col={ci} data-testid="now-col-{ci}">
          {#each col.widgets as key (key)}
            {@const dropHere = drag?.drop?.kind === 'col' && drag.drop.col === ci ? drag.drop.index : -1}
            <div class="relative transition-opacity {drag?.active && drag.key === key ? 'opacity-35' : ''}" data-widget={key}>
              {#if dropHere === col.widgets.indexOf(key)}
                <div class="pointer-events-none absolute inset-x-0 -top-[5px] z-10 h-1 rounded-full bg-accent shadow-[0_0_0_2px_var(--color-surface)]" data-testid="drop-line"></div>
              {/if}
              {@render widget(key)}
              {#if dropHere === col.widgets.length && col.widgets.indexOf(key) === col.widgets.length - 1}
                <div class="pointer-events-none absolute inset-x-0 -bottom-[5px] z-10 h-1 rounded-full bg-accent shadow-[0_0_0_2px_var(--color-surface)]" data-testid="drop-line"></div>
              {/if}
            </div>
          {/each}
        </div>
      {/each}
      {#if drag?.active && layout.columns.length < MAX_COLUMNS}
        {#each ['left', 'right'] as const as side (side)}
          {@const hot = drag.drop?.kind === 'new' && drag.drop.side === side}
          <div
            class="pointer-events-none absolute inset-y-0 {side === 'left' ? '-left-2' : '-right-2'} z-20 flex w-7 items-start justify-center rounded-md border-2 border-dashed transition-colors {hot
              ? 'border-accent bg-accent/15 text-accent'
              : 'border-border bg-surface/80 text-text-muted'}"
            data-testid="new-column-{side}"
          >
            <span class="sticky top-[40vh] mt-6 text-[0.7rem] font-semibold tracking-wide whitespace-nowrap uppercase [writing-mode:vertical-rl]">New column</span>
          </div>
        {/each}
      {/if}
    </div>
  {:else}
    <div class="grid grid-cols-1 gap-4" data-testid="now-columns">
      {#each defaultLayout().columns as col, ci (ci)}
        <div class="flex min-w-0 flex-col gap-2" data-testid={ci === 0 ? 'needs-me' : 'in-flight'}>
          <h3 class="text-sm font-semibold tracking-wide text-text-muted uppercase">{ci === 0 ? 'Needs me' : 'In flight'}</h3>
          {#each col.widgets as key (key)}
            {@render widget(key)}
          {/each}
        </div>
      {/each}
    </div>
  {/if}

  {#if drag?.active}
    <div
      class="pointer-events-none fixed z-50 flex items-center gap-2 rounded-md border border-accent bg-surface-raised px-3 py-1.5 text-sm font-semibold text-text shadow-lg"
      style="left: {Math.min(drag.x + 14, window.innerWidth - 320)}px; top: {drag.y + 10}px"
      data-testid="drag-ghost"
    >
      {WIDGET_TITLES[drag.key]}
      <span class="text-xs font-normal text-text-muted">{drag.drop === undefined ? 'stays here' : drag.drop.kind === 'new' ? `new column ${drag.drop.side}` : `column ${drag.drop.col + 1}`}</span>
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
