<script lang="ts">
  // UX prototype: a task's tags as hash-coloured chips. With `editable`, manual tags get a remove button and a
  // small "+ tag" field adds one (kept in this browser only).
  import { addManualTag, removeManualTag } from './lib/proto/prefs.svelte';
  import { tagHue, type TaskTag } from './lib/proto/tags';

  let { tags, taskRef = '', editable = false }: { tags: readonly TaskTag[]; taskRef?: string; editable?: boolean } = $props();
  let adding = $state(false);
  let text = $state('');

  const title = (t: TaskTag) =>
    t.source === 'initiative' ? `Initiative tag ${t.tag}` : t.source === 'actor' ? `A live lease holder is a ${t.tag} agent` : `Your tag ${t.tag} (prototype: this browser only)`;

  function submit(e: SubmitEvent) {
    e.preventDefault();
    addManualTag(taskRef, text);
    text = '';
    adding = false;
  }
</script>

{#if tags.length > 0 || editable}
  <span class="inline-flex flex-wrap items-center gap-1" data-testid="tag-chips">
    {#each tags as t (t.tag)}
      <span class="vh-tag" style="--h:{tagHue(t.tag)}" data-source={t.source} title={title(t)}>
        {#if t.source === 'actor'}<span aria-hidden="true">⚙</span>{/if}{t.tag}
        {#if editable && t.source === 'manual'}
          <button type="button" class="-mr-1 px-0.5 leading-none opacity-70 hover:opacity-100" aria-label="Remove tag {t.tag}" onclick={() => removeManualTag(taskRef, t.tag)}>×</button>
        {/if}
      </span>
    {/each}
    {#if editable}
      {#if adding}
        <form class="inline-flex items-center gap-1" onsubmit={submit}>
          <!-- svelte-ignore a11y_autofocus -->
          <input
            class="h-6 w-24 rounded border border-border bg-surface-raised px-1 text-xs text-text"
            bind:value={text}
            placeholder="new tag"
            aria-label="New tag for {taskRef}"
            autofocus
            onkeydown={(e) => e.key === 'Escape' && (adding = false)}
          />
          <button type="submit" class="text-xs text-accent">add</button>
        </form>
      {:else}
        <button
          type="button"
          class="rounded-full border border-dashed border-border px-1.5 text-xs leading-5 text-text-muted opacity-40 hover:text-accent hover:opacity-100 focus-visible:opacity-100"
          title="Add a tag (prototype: stored in this browser only)"
          onclick={() => (adding = true)}>+ tag</button
        >
      {/if}
    {/if}
  </span>
{/if}
