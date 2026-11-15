<script lang="ts">
  import { replaceHash } from './lib/historyEntry';
  import { onDestroy, untrack } from 'svelte';
  import { DataSet } from 'vis-data/peer';
  import { Timeline } from 'vis-timeline/peer';
  import 'vis-timeline/styles/vis-timeline-graph2d.min.css';
  import { domSurface } from './arrowSurface';
  import { taskHref } from './lib/taskPage';
  import { currentBrowser } from './lib/session';
  import { resolveRoute, ROUTES } from './lib/routes';
  import { currentViewEnv, FilterTurns } from './lib/viewState';
  import FilterSelect from './FilterSelect.svelte';
  import { InitiativesApi } from './lib/initiatives';
  import { live } from './lib/live';
  import {
    CLOSED_WITHIN_OPTIONS,
    DEFAULT_TIMELINE_FILTER,
    TIMELINE_LEGEND,
    laneStyle,
    affectsTimeline,
    choiceOptions,
    isFiltered,
    tagChoices,
    timelineHash,
    withClosedWithin,
    withInitiative,
    withTag,
    type TimelineFilter,
    type TimelineModel,
  } from './lib/timeline';
  import type { ArrowSurface } from './lib/timelineArrows';
  import {
    canZoomIn,
    canZoomOut,
    chartHeight,
    mountChart,
    sameRange,
    timelineViewHash,
    spanLabel,
    forgetTimeline,
    openTimeline,
    saveTimeline,
    startFilter,
    TimelineApi,
    TimelineViewStore,
    type Chart,
    type ChartLib,
    type TimeWindow,
    type TimelineViewState,
  } from './lib/timelineView';

  // The Timeline view (PRD R6): initiatives and their tasks over time, drawn by vis-timeline (bundled with the app,
  // no CDN). The data layer and the chart glue are lib/timelineView.ts, the mapping is lib/timeline.ts, the dependency
  // arrows are lib/timelineArrows.ts drawn on arrowSurface.ts.
  let { csrf }: { csrf: string; param?: string } = $props();

  const browser = currentBrowser();
  const api = new TimelineApi(browser, () => csrf);
  const lib = { DataSet, Timeline } as unknown as ChartLib;
  const viewEnv = currentViewEnv();
  // A fragment with a query wins and is saved; a bare one restores the saved filter (in place, before the first fetch).
  const opened = openTimeline(viewEnv, window.location.hash);
  // A fragment that names a window (`from`/`to`: a link, or Back to this entry) opens on it; otherwise the default one.
  const store = new TimelineViewStore(api, (s) => (view = s), opened.filter, undefined, undefined, opened.window);
  /** Whether the fragment carries the window: once it named one or the user moved the chart, so a fresh visit keeps the default. */
  let windowInHash = opened.window !== undefined;
  let view: TimelineViewState = $state.raw(store.state);
  let frame: HTMLDivElement | undefined = $state();
  let chart: Chart | undefined;
  /** The model the chart holds; a state that changes only the loading flag or the error leaves the chart alone. */
  let shown: TimelineModel | undefined;
  /** The visible range's length in milliseconds, 0 until the chart has drawn. */
  let span = $state(0);
  /** The visible window, for the readout's data attributes (the range the wheel, a pinch and a drag moved to). */
  let shownFrom = $state(0);
  let shownTo = $state(0);

  // The arrows go on whatever chart the frame holds now; the frame is bound before the chart is mounted.
  const arrows: ArrowSurface = {
    boxOf: (itemId) => domSurface(frame!)?.boxOf(itemId),
    render: (paths) => domSurface(frame!)?.render(paths),
  };

  /** What the filter selects offer: the board's initiatives, their tags and its actors. */
  let slugs: string[] = $state([]);
  let tags: string[] = $state([]);
  let actors: string[] = $state([]);

  // The lookup's answer, or undefined until it answers (or when it failed): an empty answer still drops restored values.
  let looked: { slug: string; tags: string[] }[] | undefined;

  // A restored filter may name an initiative or a tag the board no longer has: the first fetch waits for the list to drop them.
  // A link, an applied filter or Clear filters while the check runs is newer and wins: the late answer is dropped.
  const turns = new FilterTurns();
  // Leaving the view drops a check still running: its late answer must not rewrite the view navigated to, nor read.
  onDestroy(() => turns.end());
  let listed: Promise<void> = Promise.resolve();
  async function start() {
    const current = turns.begin();
    listed = new InitiativesApi(browser, () => csrf)
      .list('all')
      .then((list) => {
        looked = list.map((i) => ({ slug: i.slug, tags: i.tags }));
        slugs = list.map((i) => i.slug);
        tags = tagChoices(list);
      })
      .catch(() => undefined); // the selects then offer only what the URL names; the chart shows the error
    void api
      .actors()
      .then((handles) => (actors = handles))
      .catch(() => undefined);
    if (opened.restored) await listed;
    if (!current()) return;
    turns.reading();
    const kept = startFilter(opened, looked);
    if (kept === opened.filter) {
      void store.load();
    } else {
      persist(kept);
      void store.setFilter(kept);
    }
  }

  $effect(() => {
    // Read nothing reactive here: a filter change must not run the first load again.
    void untrack(start);
    // A lease, task, ask or milestone event refetches in place: the window and the collapsed rows stay.
    const unsubscribe = live().subscribe(affectsTimeline, () => (turns.hasRead() ? store.load() : undefined));
    return () => {
      unsubscribe();
      store.cancelPan();
      chart?.destroy();
    };
  });

  // The filter lives in the fragment and in the saved query: a choice rewrites both in place (no history entry per choice).
  function persist(filter: TimelineFilter) {
    replaceHash(timelineViewHash(filter, windowInHash ? view.window : undefined));
    saveTimeline(viewEnv.storage, filter);
  }

  function apply(filter: TimelineFilter) {
    turns.begin();
    turns.reading();
    persist(filter);
    void store.setFilter(filter);
  }

  // Clear filters: the defaults, and nothing saved.
  function clear() {
    turns.begin();
    turns.reading();
    forgetTimeline(viewEnv.storage);
    replaceHash('#/timeline');
    void store.setFilter(DEFAULT_TIMELINE_FILTER);
  }

  // The fragment edited by hand, or the back button after a link with another query. A bare fragment restores the saved filter.
  async function hashChanged() {
    if (resolveRoute(window.location.hash, ROUTES).id !== 'timeline') return;
    const next = openTimeline(viewEnv, window.location.hash);
    const current = turns.begin();
    // A restored filter waits for the board's list like the first one does; a link's own query does not.
    if (next.restored) await listed;
    if (!current()) return; // the fragment (or the filter) changed again while the list was read
    const filter = startFilter(next, looked);
    if (next.window !== undefined && !sameRange(next.window, view.window)) {
      windowInHash = true;
      chart?.moveTo(next.window);
    }
    if (filter !== next.filter) persist(filter);
    if (!turns.mustRead(timelineHash(filter) === timelineHash(view.filter))) return;
    turns.reading();
    void store.setFilter(filter);
  }

  // The chart is mounted once, then handed each new model. Only the frame and the model are dependencies: what the chart
  // does while it draws (the span, the arrows) must not run this again, or it would reset what the user collapsed.
  $effect(() => {
    const target = frame;
    const model = view.model;
    if (target === undefined || !view.loaded) return;
    untrack(() => {
      const hooks = {
        open: (ref: string) => (window.location.hash = taskHref(ref)),
        arrows,
        onSpan: (ms: number) => (span = ms),
        onRange: (range: TimeWindow) => store.panTo(range),
        onWindow: (range: TimeWindow) => {
          shownFrom = range.from.getTime();
          shownTo = range.to.getTime();
        },
      };
      if (chart === undefined) chart = mountChart(lib, target, model, view.window, hooks, heightFor(target));
      else if (model !== shown) chart.show(model);
      shown = model;
    });
  });

  // The window the store last read (a pan or zoom that settled) goes in the fragment, so Back and a shared link show it.
  let lastWindow = untrack(() => view.window);
  $effect(() => {
    const window = view.window;
    if (window === lastWindow) return;
    lastWindow = window;
    windowInHash = true;
    untrack(() => replaceHash(timelineViewHash(view.filter, window)));
  });

  /** The chart's tallest: the screen below the chart's top, less the phone's tab bar, so the axis stays in view as rows scroll. */
  function heightFor(target: HTMLElement): number {
    const top = target.getBoundingClientRect().top + window.scrollY;
    const tabBar = window.matchMedia('(min-width: 768px)').matches ? 16 : 80;
    return chartHeight(window.innerHeight, top, tabBar);
  }

  function resized() {
    if (chart !== undefined && frame !== undefined) chart.setHeight(heightFor(frame));
  }

  const button = 'inline-flex min-h-11 min-w-11 items-center justify-center rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text enabled:hover:bg-surface disabled:opacity-60';
</script>

<svelte:window onhashchange={hashChanged} onresize={resized} />

<section aria-labelledby="timeline-heading" class="w-full pb-16 md:pb-0" data-testid="timeline-view">
  <h2 id="timeline-heading" class="mb-4 text-xl font-bold">Timeline</h2>
  <div class="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2" data-testid="timeline-filters">
    <FilterSelect label="Initiative" options={choiceOptions(slugs, view.filter.initiatives[0] ?? '', 'Any initiative')} value={view.filter.initiatives[0] ?? ''} onchange={(v) => apply(withInitiative(view.filter, v))} testid="initiative-filter" />
    <FilterSelect label="Tag" options={choiceOptions(tags, view.filter.tags[0] ?? '', 'Any tag')} value={view.filter.tags[0] ?? ''} onchange={(v) => apply(withTag(view.filter, v))} testid="tag-filter" />
    <FilterSelect label="Actor" options={choiceOptions(actors, view.filter.actor, 'Any actor')} value={view.filter.actor} onchange={(v) => apply({ ...view.filter, actor: v })} testid="actor-filter" />
    <FilterSelect
      label="Closed within"
      options={CLOSED_WITHIN_OPTIONS.map((o) => ({ value: String(o.value), label: o.label }))}
      value={String(view.filter.closedWithin)}
      onchange={(v) => apply(withClosedWithin(view.filter, v))}
      testid="closed-filter"
    />
    {#if isFiltered(view.filter)}
      <button type="button" class="min-h-11 rounded-md border border-border px-4 text-sm font-medium text-text" data-testid="clear-filters" onclick={clear}>Clear filters</button>
    {/if}
  </div>
  {#if view.error !== ''}
    <p role="alert" class="mb-4 rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="timeline-error">{view.error}</p>
  {/if}
  {#if !view.loaded && view.error === ''}
    <p class="text-sm text-text-muted" data-testid="timeline-loading">Loading the timeline…</p>
  {:else if view.loaded && view.model.groups.length === 0}
    <p class="text-sm text-text-muted" data-testid="timeline-empty">No tasks in this window.</p>
  {/if}
  {#if view.loaded}
    <div class="mb-3 flex flex-wrap items-center gap-2" role="group" aria-label="Zoom">
      <button type="button" class={button} data-testid="zoom-out" aria-label="Zoom out" disabled={span !== 0 && !canZoomOut(span)} onclick={() => chart?.zoom('out')}>−</button>
      <button type="button" class={button} data-testid="zoom-in" aria-label="Zoom in" disabled={span !== 0 && !canZoomIn(span)} onclick={() => chart?.zoom('in')}>+</button>
      <span class="text-sm text-text-muted" data-testid="timeline-span" data-span-ms={span} data-from-ms={shownFrom} data-to-ms={shownTo}>{span === 0 ? '' : `Showing ${spanLabel(span)}`}</span>
      <span class="hidden text-xs text-text-muted md:inline" data-testid="timeline-hint">Scroll to pan · Shift+scroll to zoom</span>
    </div>
    <ul class="timeline-legend mb-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-text-muted" aria-label="Legend" data-testid="timeline-legend">
      {#each TIMELINE_LEGEND as entry (entry.className)}
        <li class="flex items-center gap-1.5">
          {#if entry.glyph === undefined}
            <span class="tl-swatch {entry.className}" aria-hidden="true"></span>
          {:else}
            <span class="tl-swatch-glyph {entry.className}" aria-hidden="true">{entry.glyph}</span>
          {/if}
          {entry.label}
        </li>
      {/each}
    </ul>
    <!-- vis-timeline scrolls inside this frame; the page itself never scrolls sideways -->
    <div class="timeline-frame relative max-w-full overflow-hidden" data-testid="timeline-chart" style={laneStyle()} bind:this={frame}></div>
  {/if}
</section>

<style>
  /* vis-timeline's own sheet is light-only and sized for a desktop: map it onto the design tokens and make rows and
     labels fit a phone. The bars carry the task's status; lease bars, markers and arrows keep their own look. */
  /* The chart takes its own touch gestures (a one-finger drag pans both ways, a pinch zooms); the page scrolls outside it. */
  .timeline-frame :global(.vis-timeline) {
    touch-action: none;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface-raised);
    color: var(--text);
    font-family: var(--font-sans);
  }
  .timeline-frame :global(.vis-panel.vis-center),
  .timeline-frame :global(.vis-panel.vis-left),
  .timeline-frame :global(.vis-panel.vis-top),
  .timeline-frame :global(.vis-panel.vis-bottom) {
    border-color: var(--border);
  }
  .timeline-frame :global(.vis-time-axis .vis-text),
  .timeline-frame :global(.vis-label) {
    color: var(--text);
  }
  .timeline-frame :global(.vis-time-axis .vis-grid.vis-minor),
  .timeline-frame :global(.vis-time-axis .vis-grid.vis-major),
  .timeline-frame :global(.vis-group) {
    border-color: var(--border);
  }
  /* The label column: the rows' own colours (vis-timeline paints them light), a cap so a long title cannot take the width
     of a phone. */
  .timeline-frame :global(.vis-labelset .vis-label) {
    display: flex;
    align-items: center;
    background: var(--surface-raised);
    color: var(--text);
    border-color: var(--border);
    cursor: pointer;
  }
  .timeline-frame :global(.vis-labelset .vis-label.vis-nested-group) {
    background: var(--surface);
  }
  /* Rows of fixed height: vis-timeline makes a row as tall as its label's inner box or its items, whichever is taller,
     and every item sits at the row's top inside it (lib/timelineView.ts chartOptions: no stacking, no item margin), so
     a row is as tall wherever the chart is panned or zoomed to. An initiative row is 44 px to tap, a task row 52 px. */
  .timeline-frame :global(.vis-label .vis-inner) {
    display: block;
    box-sizing: border-box;
    height: var(--tl-row);
    line-height: var(--tl-row);
    max-width: min(10rem, 28vw);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 0 0.5rem;
  }
  .timeline-frame :global(.vis-label) {
    --tl-row: var(--tl-task-row);
  }
  .timeline-frame :global(.vis-label.tl-initiative) {
    --tl-row: var(--tl-initiative-row);
  }
  /* Each kind keeps its lane in a task row, placed by a top margin (vis-timeline measures an item without its margin, so
     the lanes never grow the row): the planned strip on top, the actual span with its title, the leases below it
     (overlapping leases share the lane, see-through), the markers on the actual span's lane. */
  .timeline-frame :global(.vis-item.tl-planned),
  .timeline-frame :global(.vis-item.tl-actual),
  .timeline-frame :global(.vis-item.tl-lease),
  .timeline-frame :global(.vis-item.tl-marker:not(.vis-dot)) {
    box-sizing: border-box;
    font-size: 0.75rem;
  }
  .timeline-frame :global(.vis-item.tl-planned) {
    margin-top: var(--tl-planned-top);
    height: var(--tl-planned-h);
  }
  .timeline-frame :global(.vis-item.tl-actual),
  .timeline-frame :global(.vis-item.tl-marker:not(.vis-dot)) {
    margin-top: var(--tl-actual-top);
    height: var(--tl-actual-h);
    line-height: calc(var(--tl-actual-h) - 2px);
  }
  .timeline-frame :global(.vis-item.tl-lease) {
    margin-top: var(--tl-lease-top);
    height: var(--tl-lease-h);
    line-height: calc(var(--tl-lease-h) - 2px);
    font-size: 0.6875rem;
    opacity: 0.85;
  }
  .timeline-frame :global(.vis-item.tl-planned .vis-item-content),
  .timeline-frame :global(.vis-item.tl-actual .vis-item-content),
  .timeline-frame :global(.vis-item.tl-lease .vis-item-content),
  .timeline-frame :global(.vis-item.tl-marker .vis-item-content) {
    padding: 0 0.25rem;
  }
  .timeline-frame :global(.vis-group-level-unknown-but-gte1) {
    border: 0;
  }
  /* Colours by kind and state (lib/timeline.ts TIMELINE_LEGEND): each item sets --tl to a design token; bars are a light
     wash of it with a solid edge, markers the token itself, so both themes read through the tokens' own dark values. */
  .timeline-frame :global(.vis-item),
  .timeline-legend .tl-swatch {
    --tl: var(--text-muted);
    border-color: var(--tl);
    background: color-mix(in srgb, var(--tl) 18%, var(--surface-raised));
    color: var(--text);
  }
  .timeline-frame :global(.vis-item) {
    cursor: pointer;
  }
  .timeline-frame :global(.tl-actual.tl-open),
  .timeline-legend .tl-actual.tl-open {
    --tl: var(--accent);
  }
  .timeline-frame :global(.tl-actual.tl-done),
  .timeline-legend .tl-actual.tl-done {
    --tl: var(--success);
  }
  .timeline-frame :global(.tl-actual.tl-closed),
  .timeline-legend .tl-actual.tl-closed {
    --tl: var(--text-muted);
  }
  .timeline-frame :global(.vis-item.tl-planned),
  .timeline-legend .tl-planned {
    --tl: var(--text-muted);
    background: transparent;
    border-style: dashed;
  }
  .timeline-frame :global(.tl-lease-live),
  .timeline-legend .tl-lease-live {
    --tl: var(--success);
    background: color-mix(in srgb, var(--tl) 32%, var(--surface-raised));
  }
  .timeline-frame :global(.tl-lease-expired),
  .timeline-legend .tl-lease-expired {
    --tl: var(--warning);
  }
  .timeline-frame :global(.tl-lease-ended),
  .timeline-legend .tl-lease-ended {
    --tl: var(--text-muted);
    background: color-mix(in srgb, var(--tl) 10%, var(--surface-raised));
  }
  .timeline-frame :global(.vis-item.tl-marker),
  .timeline-legend .tl-swatch-glyph {
    --tl: var(--text-muted);
    background: transparent;
    border: 0;
    color: var(--tl);
    font-weight: 700;
  }
  .timeline-frame :global(.vis-item.tl-marker-milestone),
  .timeline-legend .tl-marker-milestone {
    --tl: var(--accent);
  }
  .timeline-frame :global(.vis-item.tl-marker-ask),
  .timeline-legend .tl-marker-ask {
    --tl: var(--warning);
  }
  .timeline-frame :global(.vis-item.tl-marker-check),
  .timeline-frame :global(.vis-item.tl-marker-done),
  .timeline-legend .tl-marker-check,
  .timeline-legend .tl-marker-done {
    --tl: var(--success);
  }
  .timeline-frame :global(.vis-item.tl-marker-closed),
  .timeline-legend .tl-marker-closed {
    --tl: var(--text-muted);
  }
  .timeline-frame :global(.vis-item.tl-marker .vis-item-content) {
    color: var(--tl);
  }
  .timeline-legend .tl-swatch {
    display: inline-block;
    width: 1.25rem;
    height: 0.75rem;
    border-width: 1px;
    border-style: solid;
    border-radius: 2px;
  }
  .timeline-legend .tl-swatch.tl-planned {
    border-style: dashed;
  }
  .timeline-legend .tl-swatch-glyph {
    display: inline-block;
    min-width: 1rem;
    text-align: center;
  }
  .timeline-frame :global(.tl-attention) {
    color: var(--danger);
  }
  .timeline-frame :global(svg.tl-arrows) {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    overflow: hidden;
    pointer-events: none;
    z-index: 5;
  }
  .timeline-frame :global(path.tl-arrow) {
    fill: none;
    stroke: var(--text-muted);
    stroke-width: 1.5;
  }
  .timeline-frame :global(path.tl-arrowhead) {
    fill: var(--text-muted);
  }
</style>
