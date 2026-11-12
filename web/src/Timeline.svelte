<script lang="ts">
  import { untrack } from 'svelte';
  import { DataSet } from 'vis-data/peer';
  import { Timeline } from 'vis-timeline/peer';
  import 'vis-timeline/styles/vis-timeline-graph2d.min.css';
  import { domSurface } from './arrowSurface';
  import { taskHref } from './lib/taskPage';
  import { currentBrowser } from './lib/session';
  import { filterFromHash } from './lib/timeline';
  import type { ArrowSurface } from './lib/timelineArrows';
  import {
    canZoomIn,
    canZoomOut,
    mountChart,
    spanLabel,
    TimelineApi,
    TimelineViewStore,
    type Chart,
    type ChartLib,
    type TimelineViewState,
  } from './lib/timelineView';

  // The Timeline view (PRD R6): initiatives and their tasks over time, drawn by vis-timeline (bundled with the app,
  // no CDN). The data layer and the chart glue are lib/timelineView.ts, the mapping is lib/timeline.ts, the dependency
  // arrows are lib/timelineArrows.ts drawn on arrowSurface.ts.
  let { csrf }: { csrf: string; param?: string } = $props();

  const lib = { DataSet, Timeline } as unknown as ChartLib;
  const store = new TimelineViewStore(new TimelineApi(currentBrowser(), () => csrf), (s) => (view = s), filterFromHash(window.location.hash));
  let view: TimelineViewState = $state(store.state);
  let frame: HTMLDivElement | undefined = $state();
  let chart: Chart | undefined;
  /** The visible range's length in milliseconds, 0 until the chart has drawn. */
  let span = $state(0);

  // The arrows go on whatever chart the frame holds now; the frame is bound before the chart is mounted.
  const arrows: ArrowSurface = {
    boxOf: (itemId) => domSurface(frame!)?.boxOf(itemId),
    render: (paths) => domSurface(frame!)?.render(paths),
  };

  $effect(() => {
    void store.load();
    return () => chart?.destroy();
  });

  // The chart is mounted once, then handed each new model. Only the frame and the model are dependencies: what the chart
  // does while it draws (the span, the arrows) must not run this again, or it would reset what the user collapsed.
  $effect(() => {
    const target = frame;
    const model = view.model;
    if (target === undefined || !view.loaded) return;
    untrack(() => {
      const hooks = { open: (ref: string) => (window.location.hash = taskHref(ref)), arrows, onSpan: (ms: number) => (span = ms) };
      if (chart === undefined) chart = mountChart(lib, target, model, view.window, hooks);
      else chart.show(model);
    });
  });

  const button = 'inline-flex min-h-11 min-w-11 items-center justify-center rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text enabled:hover:bg-surface disabled:opacity-60';
</script>

<section aria-labelledby="timeline-heading" class="pb-16 md:pb-0" data-testid="timeline-view">
  <h2 id="timeline-heading" class="mb-4 text-xl font-bold">Timeline</h2>
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
      <span class="text-sm text-text-muted" data-testid="timeline-span" data-span-ms={span}>{span === 0 ? '' : `Showing ${spanLabel(span)}`}</span>
    </div>
    <!-- vis-timeline scrolls inside this frame; the page itself never scrolls sideways -->
    <div class="timeline-frame relative max-w-full overflow-hidden" data-testid="timeline-chart" bind:this={frame}></div>
  {/if}
</section>

<style>
  /* vis-timeline's own sheet is light-only and sized for a desktop: map it onto the design tokens and make rows and
     labels fit a phone. The bars carry the task's status; lease bars, markers and arrows keep their own look. */
  .timeline-frame :global(.vis-timeline) {
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
     of a phone, and 44 px rows to tap. */
  .timeline-frame :global(.vis-labelset .vis-label) {
    display: flex;
    align-items: center;
    min-height: 2.75rem;
    background: var(--surface-raised);
    color: var(--text);
    border-color: var(--border);
    cursor: pointer;
  }
  .timeline-frame :global(.vis-labelset .vis-label.vis-nested-group) {
    background: var(--surface);
  }
  .timeline-frame :global(.vis-label .vis-inner) {
    display: block;
    max-width: min(10rem, 28vw);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 0.25rem 0.5rem;
  }
  .timeline-frame :global(.vis-group-level-unknown-but-gte1) {
    border: 0;
  }
  .timeline-frame :global(.vis-item) {
    border-color: var(--border);
    background: var(--surface);
    color: var(--text);
    cursor: pointer;
  }
  .timeline-frame :global(.vis-item.tl-planned) {
    background: transparent;
    border-style: dashed;
    border-color: var(--text-muted);
  }
  .timeline-frame :global(.vis-item.tl-actual) {
    border-color: var(--accent);
  }
  .timeline-frame :global(.vis-item.tl-closed) {
    border-color: var(--success);
  }
  .timeline-frame :global(.vis-item.tl-lease) {
    border-color: var(--warning);
  }
  .timeline-frame :global(.vis-item.tl-lease-expired),
  .timeline-frame :global(.vis-item.tl-lease-ended) {
    border-color: var(--text-muted);
  }
  .timeline-frame :global(.vis-item.tl-marker) {
    background: transparent;
    border: 0;
    font-weight: 700;
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
