<script lang="ts">
  // UX prototype: the one chip filter. Tags combine with AND, initiatives with OR. Agent-kind tags are dashed,
  // your own tags dotted. Long lists fold behind "more".
  import { tagHue, toggle, type ChipState, type TagChoice } from './lib/proto/tags';

  let {
    tags,
    initiatives,
    value,
    onchange,
    showInitiatives = true,
  }: { tags: readonly TagChoice[]; initiatives: readonly string[]; value: ChipState; onchange: (v: ChipState) => void; showInitiatives?: boolean } = $props();

  const FOLD = 14;
  let open = $state(false);
  const shownInitiatives = $derived(open || initiatives.length <= FOLD ? initiatives : initiatives.filter((s, n) => n < FOLD || value.initiatives.includes(s)));
  const active = $derived(value.tags.length + value.initiatives.length > 0);
  // Phones fold the whole filter behind one line; wider screens show it open.
  const wide = typeof window.matchMedia !== 'function' || window.matchMedia('(min-width: 768px)').matches;
  let shown = $state(wide);
  const picked = $derived(value.tags.length + value.initiatives.length);
</script>

<button
  type="button"
  class="flex min-h-9 w-full items-center gap-2 text-left text-sm md:hidden"
  aria-expanded={shown}
  onclick={() => (shown = !shown)}
  ><span class="font-medium">Filter</span><span class="text-text-muted">{picked > 0 ? `${picked} selected` : 'tags, initiatives'}</span><span class="ml-auto text-text-muted"
    >{shown ? '▴' : '▾'}</span
  ></button
>
{#if shown}

<div class="flex flex-col gap-1.5" data-testid="chip-filter">
  {#if tags.length > 0}
    <div role="group" aria-label="Tags, all of" class="flex flex-wrap items-center gap-1.5">
      <span class="w-20 shrink-0 text-xs text-text-muted" title="A task must carry every tag selected">Tags <span class="opacity-70">all of</span></span>
      {#each tags as t (t.tag)}
        <button
          type="button"
          class="vh-tag min-h-7"
          style="--h:{tagHue(t.tag)}"
          data-source={t.source}
          aria-pressed={value.tags.includes(t.tag)}
          data-testid="chip-tag"
          title={t.source === 'actor' ? 'Agent kind of a live lease holder' : t.source === 'manual' ? 'Your tag (prototype, this browser only)' : 'Initiative tag'}
          onclick={() => onchange({ ...value, tags: toggle(value.tags, t.tag) })}>{#if t.source === 'actor'}<span aria-hidden="true">⚙</span>{/if}{t.tag}</button
        >
      {/each}
    </div>
  {/if}
  {#if showInitiatives && initiatives.length > 0}
    <div role="group" aria-label="Initiatives, any of" class="flex flex-wrap items-center gap-1.5">
      <span class="w-20 shrink-0 text-xs text-text-muted" title="A task in any initiative selected">Initiatives <span class="opacity-70">any</span></span>
      {#each shownInitiatives as slug (slug)}
        <button
          type="button"
          class="min-h-7 rounded-md border border-border bg-surface-raised px-2 text-xs text-text aria-pressed:border-accent aria-pressed:font-semibold aria-pressed:text-accent"
          aria-pressed={value.initiatives.includes(slug)}
          data-testid="chip-initiative"
          onclick={() => onchange({ ...value, initiatives: toggle(value.initiatives, slug) })}>{slug}</button
        >
      {/each}
      {#if initiatives.length > FOLD}
        <button type="button" class="min-h-7 px-1 text-xs text-accent" onclick={() => (open = !open)}>{open ? 'less' : `+${initiatives.length - shownInitiatives.length} more`}</button>
      {/if}
    </div>
  {/if}
  {#if active}
    <div>
      <button type="button" class="min-h-7 rounded-md border border-border px-2 text-xs text-text" data-testid="chip-clear" onclick={() => onchange({ tags: [], initiatives: [] })}>Clear chips</button>
    </div>
  {/if}
</div>
{/if}
