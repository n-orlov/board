<script lang="ts">
  import { RESOLUTIONS } from './lib/taskPage';
  import type { Resolution } from './lib/tasks';

  // The Close confirmation inside the dialog/sheet: the resolution to close with, then the action's own button. A refusal
  // comes in as `error` and the dialog stays open.
  let {
    ref,
    error,
    onconfirm,
    oncancel,
  }: { ref: string; error: string | undefined; onconfirm: (resolution: Resolution) => Promise<void>; oncancel: () => void } = $props();

  let resolution: Resolution = $state('completed');
  let busy = $state(false);

  const input = 'mt-1 block min-h-11 w-full rounded-md border border-border bg-surface px-3 py-2 text-base text-text';
  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium disabled:opacity-60';

  async function submit() {
    busy = true;
    try {
      await onconfirm(resolution);
    } finally {
      busy = false;
    }
  }
</script>

<div class="flex flex-col gap-4" data-testid="close-task-form">
  <h2 class="text-lg font-bold">Close task</h2>

  {#if error !== undefined}
    <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="dialog-error">{error}</p>
  {/if}

  <p class="text-sm" data-testid="dialog-body">Close {ref} with a resolution. A closed task can be reopened later.</p>
  <div class="text-sm font-medium">
    <label for="close-resolution">Resolution</label>
    <select id="close-resolution" name="resolution" class={input} bind:value={resolution}>
      {#each RESOLUTIONS as option (option.value)}<option value={option.value}>{option.label}</option>{/each}
    </select>
  </div>

  <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
    <button type="button" class="{button} border-border text-text" onclick={oncancel}>Cancel</button>
    <button type="button" class="{button} border-danger text-danger" disabled={busy} onclick={submit}>Close task</button>
  </div>
</div>
