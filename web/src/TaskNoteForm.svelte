<script lang="ts">
  // A one-line form inside the dialog/sheet: Milestone and Record check ask for a summary, End lease for an optional
  // outcome. A refusal comes in as `error`; the dialog stays open and the text stays so it can be corrected.
  let {
    heading,
    label,
    hint,
    submitLabel,
    error,
    onsubmit,
    oncancel,
  }: {
    heading: string;
    label: string;
    hint: string;
    submitLabel: string;
    error: string | undefined;
    onsubmit: (text: string) => Promise<void>;
    oncancel: () => void;
  } = $props();

  let text = $state('');
  let busy = $state(false);

  const input = 'mt-1 block min-h-11 w-full rounded-md border border-border bg-surface px-3 py-2 text-base text-text';
  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium disabled:opacity-60';

  async function submit(event: Event) {
    event.preventDefault();
    busy = true;
    try {
      await onsubmit(text);
    } finally {
      busy = false;
    }
  }
</script>

<form onsubmit={submit} class="flex flex-col gap-4" data-testid="task-note-form">
  <h2 class="text-lg font-bold">{heading}</h2>

  {#if error !== undefined}
    <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="dialog-error">{error}</p>
  {/if}

  <label class="block text-sm font-medium">{label} <input name="text" class={input} bind:value={text} autocomplete="off" /></label>
  <p class="text-sm text-text-muted">{hint}</p>

  <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
    <button type="button" class="{button} border-border text-text" onclick={oncancel}>Cancel</button>
    <button type="submit" class="{button} border-accent text-accent" disabled={busy}>{submitLabel}</button>
  </div>
</form>
