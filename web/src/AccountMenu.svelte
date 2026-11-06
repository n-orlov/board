<script lang="ts">
  import Dialog from './Dialog.svelte';

  // The account menu: the signed-in email and Sign out, in the one dialog/sheet component.
  let { email, onclose, onsignout }: { email: string; onclose: () => void; onsignout: () => void } = $props();
</script>

<Dialog label="Account" {onclose}>
  <p class="text-sm text-text-muted">Signed in as</p>
  <p class="mb-4 break-all font-semibold" data-testid="menu-email">{email}</p>
  <div class="flex flex-col gap-2 md:flex-row-reverse">
    <button
      type="button"
      class="min-h-11 rounded-md border border-border bg-surface px-4 text-text hover:bg-surface-raised"
      onclick={onsignout}>Sign out</button
    >
    <button type="button" class="min-h-11 rounded-md px-4 text-text-muted hover:text-text" onclick={onclose}>Close</button>
  </div>
</Dialog>
