<script lang="ts">
  // The dock of the Now view on a wide screen (PRD R4, SPEC 11): the widgets in 1 to 4 columns, each moved by its handle (a
  // pointer drag, or the arrow keys on the focused handle), the columns resized by the splitters between them and a widget's
  // height by the bar under it. The layout itself is `lib/nowLayout.ts`; this draws it and turns pointer and key input into
  // its moves. What a widget holds is the `body` snippet.
  import { onDestroy, tick, type Snippet } from 'svelte';
  import { BANDS } from './lib/nowBands';
  import {
    MAX_COLUMNS,
    MAX_HEIGHT,
    MIN_HEIGHT,
    MIN_WIDTH,
    WIDGET_TITLES,
    moveWidget,
    nudgeWidget,
    positionLabel,
    pruneEmpty,
    resizeColumns,
    setHeight,
    type Direction,
    type Drop,
    type NowLayout,
    type WidgetKey,
  } from './lib/nowLayout';

  let {
    layout,
    count,
    open,
    onchange,
    ontoggle,
    body,
  }: {
    layout: NowLayout;
    count: (key: WidgetKey) => number;
    open: (key: WidgetKey) => boolean;
    /** A new layout; `final` is false while a pointer drag is still going on (nothing is saved until it ends). */
    onchange: (next: NowLayout, final: boolean) => void;
    ontoggle: (key: WidgetKey, open: boolean) => void;
    body: Snippet<[WidgetKey]>;
  } = $props();

  const SPLITTER = 12;
  const EDGE = 48;
  const COLUMN_STEP = 0.02;
  const HEIGHT_STEP = 24;
  const DRAG_START = 4;
  const KEYS: Record<string, Direction> = { ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right' };

  const isBand = (key: WidgetKey) => BANDS.some((band) => band.key === key);
  const sectionId = (key: WidgetKey) => (isBand(key) ? `band-${key}` : key === 'flight' ? 'now-flight' : 'now-recent');
  const toggleId = (key: WidgetKey) => (isBand(key) ? 'band-toggle' : 'widget-toggle');
  const countId = (key: WidgetKey) => (isBand(key) ? 'band-count' : `${key}-count`);

  let row: HTMLElement | undefined = $state();
  let measured: Partial<Record<WidgetKey, number>> = $state({});
  let status = $state('');
  interface Dragging {
    key: WidgetKey;
    drop: Drop | undefined;
    /** Where the drop lands, in viewport pixels. */
    mark: { left: number; top: number; width: number; height: number } | undefined;
  }
  let drag: Dragging | undefined = $state();
  let stop: (() => void) | undefined;
  onDestroy(() => stop?.());

  function announce(next: NowLayout, key: WidgetKey, suffix = '') {
    status = `${positionLabel(next, key)}${suffix}`;
  }

  async function refocus(key: WidgetKey) {
    await tick();
    row?.querySelector<HTMLElement>(`[data-handle="${key}"]`)?.focus();
  }

  // The drop under the pointer: a strip at either edge of the dock makes a new column (while there are fewer than four), else
  // the nearest column, at the place among its widgets the pointer is above.
  function target(x: number, y: number): Pick<Dragging, 'drop' | 'mark'> {
    if (row === undefined) return { drop: undefined, mark: undefined };
    const box = row.getBoundingClientRect();
    if (layout.columns.length < MAX_COLUMNS && (x < box.left + EDGE || x > box.right - EDGE)) {
      const side = x < box.left + EDGE ? 'left' : 'right';
      return { drop: { kind: 'new', side }, mark: { left: side === 'left' ? box.left : box.right - 6, top: box.top, width: 6, height: box.height } };
    }
    const cols = Array.from(row.querySelectorAll<HTMLElement>('[data-dock-col]'));
    let best = -1;
    let gap = Infinity;
    cols.forEach((el, i) => {
      const r = el.getBoundingClientRect();
      const d = x < r.left ? r.left - x : x > r.right ? x - r.right : 0;
      if (d < gap) {
        gap = d;
        best = i;
      }
    });
    if (best < 0) return { drop: undefined, mark: undefined };
    const col = cols[best]!.getBoundingClientRect();
    const widgets = Array.from(cols[best]!.querySelectorAll<HTMLElement>(':scope > [data-widget]'));
    let index = widgets.length;
    for (let i = 0; i < widgets.length; i++) {
      const r = widgets[i]!.getBoundingClientRect();
      if (y < r.top + r.height / 2) {
        index = i;
        break;
      }
    }
    const top = index < widgets.length ? widgets[index]!.getBoundingClientRect().top - 6 : widgets.length > 0 ? widgets[widgets.length - 1]!.getBoundingClientRect().bottom + 2 : col.top;
    return { drop: { kind: 'col', col: best, index }, mark: { left: col.left, top, width: col.width, height: 4 } };
  }

  // One pointer gesture: the move after the pointer goes down, the end at its release (or pointercancel, or Escape).
  function gesture(e: PointerEvent, move: (x: number, y: number, moved: boolean) => void, end: (commit: boolean) => void) {
    if (e.button !== 0) return;
    stop?.();
    const startX = e.clientX;
    const startY = e.clientY;
    let moved = false;
    const onmove = (ev: PointerEvent) => {
      if (!moved && Math.hypot(ev.clientX - startX, ev.clientY - startY) < DRAG_START) return;
      moved = true;
      move(ev.clientX - startX, ev.clientY - startY, true);
      if (drag !== undefined) drag = { ...drag, ...target(ev.clientX, ev.clientY) };
    };
    const finish = (commit: boolean) => {
      stop?.();
      end(commit && moved);
    };
    const onup = () => finish(true);
    const oncancel = () => finish(false);
    const onkey = (ev: KeyboardEvent) => {
      if (ev.key === 'Escape') finish(false);
    };
    const onselect = (ev: Event) => ev.preventDefault();
    window.addEventListener('pointermove', onmove);
    window.addEventListener('pointerup', onup);
    window.addEventListener('pointercancel', oncancel);
    window.addEventListener('keydown', onkey);
    window.addEventListener('selectstart', onselect);
    stop = () => {
      window.removeEventListener('pointermove', onmove);
      window.removeEventListener('pointerup', onup);
      window.removeEventListener('pointercancel', oncancel);
      window.removeEventListener('keydown', onkey);
      window.removeEventListener('selectstart', onselect);
      stop = undefined;
    };
  }

  function startMove(e: PointerEvent, key: WidgetKey) {
    gesture(
      e,
      () => {
        if (drag === undefined) drag = { key, drop: undefined, mark: undefined };
      },
      (commit) => {
        const dropped = drag?.drop;
        drag = undefined;
        if (!commit || dropped === undefined) return;
        const next = pruneEmpty(moveWidget(layout, key, dropped));
        if (next === layout) return;
        onchange(next, true);
        announce(next, key);
        void refocus(key);
      },
    );
  }

  function moveKey(e: KeyboardEvent, key: WidgetKey) {
    const dir = KEYS[e.key];
    if (dir === undefined || e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return;
    e.preventDefault();
    const next = pruneEmpty(nudgeWidget(layout, key, dir));
    if (next === layout) {
      announce(layout, key, `, cannot move ${dir}`);
      return;
    }
    onchange(next, true);
    announce(next, key);
    void refocus(key);
  }

  function startSplit(e: PointerEvent, left: number) {
    if (row === undefined) return;
    const base = layout;
    const avail = row.getBoundingClientRect().width - SPLITTER * (base.columns.length - 1);
    let last = base;
    gesture(
      e,
      (dx) => {
        last = resizeColumns(base, left, dx / avail);
        onchange(last, false);
      },
      (commit) => {
        if (last !== base) onchange(commit ? last : base, true);
      },
    );
  }

  function splitKey(e: KeyboardEvent, left: number) {
    const step = e.key === 'ArrowLeft' ? -COLUMN_STEP : e.key === 'ArrowRight' ? COLUMN_STEP : 0;
    if (step === 0 || e.ctrlKey || e.altKey || e.metaKey) return;
    e.preventDefault();
    const next = resizeColumns(layout, left, step);
    if (next === layout) return;
    onchange(next, true);
    const width = Math.round(next.columns[left]!.width * 100);
    status = `Column ${left + 1} is ${width}% wide, column ${left + 2} is ${Math.round(next.columns[left + 1]!.width * 100)}%`;
  }

  function startHeight(e: PointerEvent, key: WidgetKey) {
    const panel = row?.querySelector<HTMLElement>(`#band-panel-${key}`);
    if (panel === null || panel === undefined) return;
    const base = layout;
    const from = panel.getBoundingClientRect().height;
    let last = base;
    gesture(
      e,
      (_dx, dy) => {
        last = setHeight(base, key, from + dy);
        onchange(last, false);
      },
      (commit) => {
        if (last !== base) onchange(commit ? last : base, true);
      },
    );
  }

  function heightKey(e: KeyboardEvent, key: WidgetKey) {
    if (e.ctrlKey || e.altKey || e.metaKey) return;
    let next: NowLayout;
    if (e.key === 'Home') next = setHeight(layout, key, undefined);
    else if (e.key === 'ArrowUp' || e.key === 'ArrowDown') next = setHeight(layout, key, (layout.heights[key] ?? measured[key] ?? MIN_HEIGHT) + (e.key === 'ArrowUp' ? -HEIGHT_STEP : HEIGHT_STEP));
    else return;
    e.preventDefault();
    if (next === layout) return;
    onchange(next, true);
    status = e.key === 'Home' ? `${WIDGET_TITLES[key]} takes the height of its content` : `${WIDGET_TITLES[key]} is ${next.heights[key]} pixels high`;
  }

  const tracks = $derived(layout.columns.map((col) => `minmax(0, ${col.width}fr)`).join(` ${SPLITTER}px `));
</script>

{#snippet widget(key: WidgetKey)}
  {@const title = WIDGET_TITLES[key]}
  {@const isOpen = open(key)}
  {@const height = layout.heights[key]}
  <section
    class="min-w-0 rounded-lg border border-border bg-surface {drag?.key === key ? 'ring-2 ring-accent' : ''}"
    data-testid={sectionId(key)}
    data-widget={key}
    data-open={isOpen}
    data-dragging={drag?.key === key}
  >
    <div class="flex items-center">
      <button
        type="button"
        class="flex min-h-11 min-w-11 shrink-0 cursor-grab touch-none items-center justify-center rounded-lg text-text-muted active:cursor-grabbing"
        aria-label="Move {title}"
        aria-describedby="dock-help"
        data-handle={key}
        data-testid="widget-handle"
        onpointerdown={(e) => startMove(e, key)}
        onkeydown={(e) => moveKey(e, key)}
      >
        <svg viewBox="0 0 24 24" class="size-5" fill="currentColor" aria-hidden="true">
          <circle cx="9" cy="6" r="1.6" /><circle cx="15" cy="6" r="1.6" /><circle cx="9" cy="12" r="1.6" /><circle cx="15" cy="12" r="1.6" /><circle cx="9" cy="18" r="1.6" /><circle cx="15" cy="18" r="1.6" />
        </svg>
      </button>
      <h3 class="min-w-0 flex-1">
        <button
          type="button"
          id="band-toggle-{key}"
          class="flex min-h-11 w-full items-center gap-2 rounded-lg py-2 pr-4 text-left text-base font-semibold"
          aria-expanded={isOpen}
          aria-controls="band-panel-{key}"
          data-testid={toggleId(key)}
          onclick={() => ontoggle(key, !isOpen)}
        >
          <svg viewBox="0 0 24 24" class="size-4 shrink-0 transition-transform {isOpen ? 'rotate-90' : ''}" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M9 6l6 6-6 6" /></svg>
          <span>{title}</span>
          <span class="text-sm font-normal text-text-muted" data-testid={countId(key)}>{count(key)}</span>
        </button>
      </h3>
    </div>
    <div
      id="band-panel-{key}"
      role="region"
      aria-labelledby="band-toggle-{key}"
      hidden={!isOpen}
      class="px-4 pb-4 {height !== undefined ? 'overflow-auto' : ''}"
      style:height={height !== undefined ? `${height}px` : undefined}
      bind:clientHeight={measured[key]}
      data-testid="band-panel"
    >
      {@render body(key)}
    </div>
    {#if isOpen}
      <div
        role="slider"
        tabindex="0"
        aria-orientation="horizontal"
        aria-label="Resize the height of {title}"
        aria-valuenow={Math.round(height ?? measured[key] ?? MIN_HEIGHT)}
        aria-valuemin={MIN_HEIGHT}
        aria-valuemax={MAX_HEIGHT}
        class="group flex h-3 cursor-row-resize touch-none items-center justify-center"
        data-testid="height-handle"
        onpointerdown={(e) => startHeight(e, key)}
        onkeydown={(e) => heightKey(e, key)}
      >
        <span class="h-1 w-10 rounded-full bg-border group-hover:bg-text-muted"></span>
      </div>
    {/if}
  </section>
{/snippet}

<p id="dock-help" class="sr-only">Drag the handle, or press the arrow keys on it, to move this widget. Up and down move it in its column, left and right to the next column or into a new one.</p>
<p role="status" class="sr-only" data-testid="dock-status">{status}</p>

<div bind:this={row} class="grid items-start {drag !== undefined ? 'select-none' : ''}" style:grid-template-columns={tracks} data-testid="now-dock">
  {#each layout.columns as col, c (c)}
    <div class="flex min-w-0 flex-col gap-3" data-dock-col={c} data-testid="dock-column">
      {#each col.widgets as key (key)}
        {@render widget(key)}
      {/each}
    </div>
    {#if c < layout.columns.length - 1}
      {@const pair = col.width + layout.columns[c + 1]!.width}
      <div
        role="slider"
        tabindex="0"
        aria-orientation="vertical"
        aria-label="Resize columns {c + 1} and {c + 2}"
        aria-valuenow={Math.round(col.width * 100)}
        aria-valuemin={Math.round(MIN_WIDTH * 100)}
        aria-valuemax={Math.round((pair - MIN_WIDTH) * 100)}
        class="group flex h-full min-h-24 cursor-col-resize touch-none justify-center self-stretch"
        data-testid="column-splitter"
        onpointerdown={(e) => startSplit(e, c)}
        onkeydown={(e) => splitKey(e, c)}
      >
        <span class="h-full w-1 rounded-full bg-border group-hover:bg-text-muted"></span>
      </div>
    {/if}
  {/each}
</div>

{#if drag?.mark !== undefined}
  <div
    class="pointer-events-none fixed z-50 rounded bg-accent"
    style:left="{drag.mark.left}px"
    style:top="{drag.mark.top}px"
    style:width="{drag.mark.width}px"
    style:height="{drag.mark.height}px"
    data-testid="drop-indicator"
  ></div>
{/if}
