<script lang="ts">
  import type { InitiativeForm } from './lib/initiatives';

  // The create form inside the dialog/sheet. It holds what was typed; a refusal comes in as `error`
  // and the input stays so it can be corrected.
  let {
    error,
    oncreate,
    oncancel,
  }: { error: string | undefined; oncreate: (form: InitiativeForm) => Promise<void>; oncancel: () => void } = $props();

  let slug = $state('');
  let title = $state('');
  let description = $state('');
  let tags = $state('');
  let busy = $state(false);

  const input = 'mt-1 block min-h-11 w-full rounded-md border border-border bg-surface px-3 py-2 text-base text-text';
  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium disabled:opacity-60';

  async function submit(event: Event) {
    event.preventDefault();
    busy = true;
    try {
      await oncreate({ slug, title, description, tags });
    } finally {
      busy = false;
    }
  }
</script>

<form onsubmit={submit} class="flex flex-col gap-4">
  <h2 class="text-lg font-bold">New initiative</h2>

  {#if error !== undefined}
    <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger">{error}</p>
  {/if}

  <label class="block text-sm font-medium">Slug <input name="slug" class="{input} font-mono" bind:value={slug} autocapitalize="none" autocomplete="off" spellcheck="false" /></label>
  <label class="block text-sm font-medium">Title <input name="title" class={input} bind:value={title} /></label>
  <label class="block text-sm font-medium">Description <textarea name="description" rows="3" class={input} bind:value={description}></textarea></label>
  <label class="block text-sm font-medium">Tags <input name="tags" class={input} bind:value={tags} placeholder="comma separated (optional)" /></label>

  <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
    <button type="button" class="{button} border-border text-text" onclick={oncancel}>Cancel</button>
    <button type="submit" class="{button} border-accent text-accent" disabled={busy}>Create initiative</button>
  </div>
</form>
