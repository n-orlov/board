<script lang="ts">
  import { taskHref } from './lib/taskPage';
  import type { TaskRow } from './lib/initiativePage';
  import { moreText } from './lib/needsAttention';
  import { toneClasses } from './lib/presentation';

  // The Needs attention section (PRD R2): one card per task that needs a person, its attention badges and its flags, in the
  // order the caller gives; nothing at all when there are none. `showInitiative` adds the task's initiative (the Tasks view).
  let { rows, more, moreHref, showInitiative }: { rows: TaskRow[]; more: number; moreHref: string; showInitiative: boolean } = $props();

  const badge = 'inline-block rounded-md border px-2 py-0.5 text-xs font-medium';
</script>

{#if rows.length > 0}
  <section aria-labelledby="needs-attention-heading" class="mb-6" data-testid="needs-attention">
    <h3 id="needs-attention-heading" class="mb-3 text-lg font-bold">Needs attention <span class="font-normal text-text-muted" data-testid="needs-count">({rows.length + more})</span></h3>
    <ul class="flex flex-col gap-3">
      {#each rows as row (row.id)}
        <li
          data-testid="needs-row"
          data-ref={row.ref}
          class="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-warning bg-surface-raised p-4"
        >
          <a class="inline-flex min-h-11 min-w-11 items-center font-mono text-sm text-accent hover:underline" href={taskHref(row.ref)} data-testid="needs-ref">{row.ref}</a>
          {#if showInitiative}
            <a class="inline-flex min-h-11 min-w-11 items-center text-sm text-accent hover:underline" href="#/i/{row.initiative}" data-testid="needs-initiative">{row.initiative}</a>
          {/if}
          <span class="order-last w-full min-w-0 font-medium wrap-anywhere md:order-none md:w-auto md:flex-1" data-testid="needs-title">{row.title}</span>
          <span class="flex flex-wrap gap-1" data-testid="needs-badges">
            {#each row.attention as reason (reason.reason)}
              <span class="{badge} {toneClasses(reason.tone)}" data-testid="attention-{reason.reason}">
                <span aria-hidden="true">{reason.label}</span><span class="sr-only">{reason.name}</span>
              </span>
            {/each}
            {#each row.badges as flag (flag.key)}
              <span class="{badge} {toneClasses(flag.tone)}" data-testid="flag-{flag.key}">{flag.label}</span>
            {/each}
          </span>
          {#if row.holder !== ''}
            <span class="text-sm text-text-muted" data-testid="needs-holder">{row.holder}</span>
          {/if}
        </li>
      {/each}
    </ul>
    {#if more > 0}
      <p class="mt-3 text-sm">
        <a class="inline-flex min-h-11 items-center text-accent hover:underline" href={moreHref} data-testid="needs-more">{moreText(more)}</a>
      </p>
    {/if}
  </section>
{/if}
