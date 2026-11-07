<script lang="ts">
  // A confirmation inside the dialog/sheet: a heading, what the action does, and the action's own button.
  // A refusal comes in as `error` and the dialog stays open.
  let {
    heading,
    body,
    confirm,
    danger = false,
    error,
    onconfirm,
    oncancel,
  }: {
    heading: string;
    body: string;
    confirm: string;
    danger?: boolean;
    error: string | undefined;
    onconfirm: () => Promise<void>;
    oncancel: () => void;
  } = $props();

  let busy = $state(false);

  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium disabled:opacity-60';

  async function submit() {
    busy = true;
    try {
      await onconfirm();
    } finally {
      busy = false;
    }
  }
</script>

<div class="flex flex-col gap-4">
  <h2 class="text-lg font-bold">{heading}</h2>

  {#if error !== undefined}
    <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="dialog-error">{error}</p>
  {/if}

  <p class="text-sm" data-testid="dialog-body">{body}</p>

  <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
    <button type="button" data-cancel class="{button} border-border text-text" onclick={oncancel}>Cancel</button>
    <button type="button" class="{button} {danger ? 'border-danger text-danger' : 'border-accent text-accent'}" disabled={busy} onclick={submit}>{confirm}</button>
  </div>
</div>
