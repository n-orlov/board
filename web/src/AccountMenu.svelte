<script lang="ts">
  import Dialog from './Dialog.svelte';
  import ThemeControl from './ThemeControl.svelte';
  import { accountRows } from './lib/accountMenu';

  // The account menu: the signed-in email, the operator handle, the theme setting and Sign out, in the one dialog/sheet component.
  let { email, actor, onclose, onsignout }: { email: string; actor: string; onclose: () => void; onsignout: () => void } = $props();
  const rows = $derived(accountRows({ email, actor }));
</script>

<Dialog label="Account" {onclose}>
  {#each rows as row (row.testid)}
    <p class="text-sm text-text-muted">{row.label}</p>
    <p class="mb-4 break-all font-semibold" data-testid={row.testid}>{row.value}</p>
  {/each}
  <ThemeControl />
  <div class="flex flex-col gap-2 md:flex-row-reverse">
    <button
      type="button"
      class="min-h-11 rounded-md border border-border bg-surface px-4 text-text hover:bg-surface-raised"
      onclick={onsignout}>Sign out</button
    >
    <button type="button" class="min-h-11 rounded-md px-4 text-text-muted hover:text-text" onclick={onclose}>Close</button>
  </div>
</Dialog>
