<script lang="ts">
  import { DataSet } from 'vis-data/peer';
  import { Timeline } from 'vis-timeline/peer';
  import 'vis-timeline/styles/vis-timeline-graph2d.min.css';
  import { currentBrowser } from './lib/session';
  import { filterFromHash } from './lib/timeline';
  import { mountChart, TimelineApi, TimelineViewStore, type Chart, type ChartLib, type TimelineViewState } from './lib/timelineView';

  // The Timeline view (PRD R6): initiatives and their tasks over time, drawn by vis-timeline (bundled with the app,
  // no CDN). The data layer and the chart glue are lib/timelineView.ts, the mapping is lib/timeline.ts.
  let { csrf }: { csrf: string; param?: string } = $props();

  const lib = { DataSet, Timeline } as unknown as ChartLib;
  const store = new TimelineViewStore(new TimelineApi(currentBrowser(), () => csrf), (s) => (view = s), filterFromHash(window.location.hash));
  let view: TimelineViewState = $state(store.state);
  let frame: HTMLDivElement | undefined = $state();
  let chart: Chart | undefined;

  $effect(() => {
    void store.load();
    return () => chart?.destroy();
  });

  $effect(() => {
    if (frame === undefined || !view.loaded) return;
    if (chart === undefined) chart = mountChart(lib, frame, view.model, view.window);
    else chart.show(view.model);
  });
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
    <!-- vis-timeline scrolls inside this frame; the page itself never scrolls sideways -->
    <div class="max-w-full overflow-hidden" data-testid="timeline-chart" bind:this={frame}></div>
  {/if}
</section>
