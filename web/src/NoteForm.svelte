<script lang="ts">
  import type { NoteForm } from './lib/notes';

  // The Post note dialog's body (PRD R10): a one-line summary, an optional Markdown body, a resource and the handles it
  // mentions. A refusal comes in as `error`; the dialog stays open and the text stays so it can be corrected.
  let {
    heading,
    error,
    onsubmit,
    oncancel,
  }: { heading: string; error: string | undefined; onsubmit: (form: NoteForm) => Promise<void>; oncancel: () => void } = $props();

  let form: NoteForm = $state({ summary: '', body: '', resource: '', mentions: '' });
  let busy = $state(false);

  const input = 'mt-1 block min-h-11 w-full rounded-md border border-border bg-surface px-3 py-2 text-base text-text';
  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium disabled:opacity-60';

  async function submit(event: Event) {
    event.preventDefault();
    busy = true;
    try {
      await onsubmit({ ...form });
    } finally {
      busy = false;
    }
  }
</script>

<form onsubmit={submit} class="flex flex-col gap-4" data-testid="note-form">
  <h2 class="text-lg font-bold wrap-anywhere">{heading}</h2>

  {#if error !== undefined}
    <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="dialog-error">{error}</p>
  {/if}

  <label class="block text-sm font-medium">Summary <input name="summary" class={input} bind:value={form.summary} autocomplete="off" /></label>
  <label class="block text-sm font-medium">Body (optional, Markdown) <textarea name="body" rows="5" class={input} bind:value={form.body}></textarea></label>
  <label class="block text-sm font-medium">Resource (optional) <input name="resource" class={input} bind:value={form.resource} autocomplete="off" placeholder="host:build-1" /></label>
  <label class="block text-sm font-medium">Mentions (optional) <input name="mentions" class={input} bind:value={form.mentions} autocomplete="off" placeholder="alice bob" /></label>

  <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
    <button type="button" class="{button} border-border text-text" onclick={oncancel}>Cancel</button>
    <button type="submit" class="{button} border-accent text-accent" disabled={busy}>Post note</button>
  </div>
</form>
