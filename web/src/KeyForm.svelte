<script lang="ts">
  // The create-key form inside the dialog/sheet. It holds what was typed; a refusal comes in as `error`
  // and the input stays so it can be corrected.
  let {
    error,
    oncreate,
    oncancel,
  }: { error: string | undefined; oncreate: (name: string, expiresIn: string) => Promise<void>; oncancel: () => void } = $props();

  let name = $state('');
  let expiresIn = $state('');
  let busy = $state(false);

  const input = 'mt-1 block min-h-11 w-full rounded-md border border-border bg-surface px-3 py-2 text-base text-text';
  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium disabled:opacity-60';

  async function submit(event: Event) {
    event.preventDefault();
    busy = true;
    try {
      await oncreate(name, expiresIn);
    } finally {
      busy = false;
    }
  }
</script>

<form onsubmit={submit} class="flex flex-col gap-4">
  <h2 class="text-lg font-bold">New API key</h2>

  {#if error !== undefined}
    <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger">{error}</p>
  {/if}

  <label class="block text-sm font-medium">
    Name
    <input
      name="name"
      class="{input} font-mono"
      bind:value={name}
      required
      maxlength="64"
      pattern="[A-Za-z0-9][A-Za-z0-9._\-]*"
      autocapitalize="none"
      autocomplete="off"
      spellcheck="false"
    />
  </label>
  <label class="block text-sm font-medium">
    Expires in
    <input name="expires_in" class={input} bind:value={expiresIn} placeholder="90d (optional)" autocapitalize="none" autocomplete="off" />
  </label>

  <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
    <button type="button" class="{button} border-border text-text" onclick={oncancel}>Cancel</button>
    <button type="submit" class="{button} border-accent text-accent" disabled={busy}>Create key</button>
  </div>
</form>
