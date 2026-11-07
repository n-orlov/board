<script lang="ts">
  import { depOptions } from './lib/taskPage';
  import type { TaskListItem } from './lib/initiatives';
  import type { TaskDetail } from './lib/tasks';

  // The Add dependency form inside the dialog/sheet: find an open task by ref or title and pick it; the pick adds the
  // dependency. A refusal (a cycle, say) comes in as `error` and the dialog stays open.
  let {
    task,
    candidates,
    error,
    onpick,
    oncancel,
  }: { task: Pick<TaskDetail, 'ref' | 'deps'>; candidates: TaskListItem[]; error: string | undefined; onpick: (ref: string) => Promise<void>; oncancel: () => void } = $props();

  let text = $state('');
  let busy = $state(false);

  const options = $derived(depOptions(candidates, task, text));
  const input = 'mt-1 block min-h-11 w-full rounded-md border border-border bg-surface px-3 py-2 text-base text-text';
  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium disabled:opacity-60';

  async function pick(ref: string) {
    busy = true;
    try {
      await onpick(ref);
    } finally {
      busy = false;
    }
  }
</script>

<div class="flex flex-col gap-4" data-testid="task-dep-form">
  <h2 class="text-lg font-bold">Add dependency</h2>

  {#if error !== undefined}
    <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="dialog-error">{error}</p>
  {/if}

  <p class="text-sm">Choose the open task that must finish before {task.ref}.</p>
  <input name="blocker_search" type="search" class={input} bind:value={text} placeholder="Find an open task" aria-label="Find an open task" autocomplete="off" />
  {#if options.length > 0}
    <ul class="flex flex-col gap-1" data-testid="dep-options">
      {#each options as option (option.id)}
        <li>
          <button
            type="button"
            class="flex min-h-11 w-full items-center gap-2 rounded-md border border-border px-3 text-left text-sm wrap-anywhere disabled:opacity-60"
            data-ref={option.ref}
            disabled={busy}
            onclick={() => pick(option.ref)}
          >
            <code class="font-mono">{option.ref}</code><span>{option.title}</span>
          </button>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="text-sm text-text-muted">No open task matches.</p>
  {/if}

  <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
    <button type="button" class="{button} border-border text-text" onclick={oncancel}>Cancel</button>
  </div>
</div>
