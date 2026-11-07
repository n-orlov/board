<script lang="ts">
  import { addEditLink, removeEditLink, type TaskEditForm } from './lib/taskEdit';

  // The edit form inside the dialog/sheet. It starts from the task as it is now; a refusal comes in as `error` and the
  // input stays so it can be corrected. The cadence and the next check are for a watch task only; the links are replaced
  // as a whole.
  let {
    initial,
    watch,
    error,
    onsave,
    oncancel,
  }: { initial: TaskEditForm; watch: boolean; error: string | undefined; onsave: (form: TaskEditForm) => Promise<void>; oncancel: () => void } = $props();

  // The dialog is mounted per open, so the form takes its starting values once.
  // svelte-ignore state_referenced_locally
  let form: TaskEditForm = $state({ ...initial, links: initial.links.map((link) => ({ ...link })) });
  let busy = $state(false);

  const input = 'mt-1 block min-h-11 w-full rounded-md border border-border bg-surface px-3 py-2 text-base text-text';
  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium disabled:opacity-60';

  async function submit(event: Event) {
    event.preventDefault();
    busy = true;
    try {
      await onsave({ ...form });
    } finally {
      busy = false;
    }
  }
</script>

<form onsubmit={submit} class="flex flex-col gap-4" data-testid="edit-task-form">
  <h2 class="text-lg font-bold">Edit task</h2>

  {#if error !== undefined}
    <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="dialog-error">{error}</p>
  {/if}

  <label class="block text-sm font-medium">Title <input name="title" class={input} bind:value={form.title} /></label>
  <label class="block text-sm font-medium">Description <textarea name="description" rows="5" class={input} bind:value={form.description}></textarea></label>
  {#if watch}
    <label class="block text-sm font-medium"
      >Cadence <input name="cadence" class={input} bind:value={form.cadence} placeholder="15m, 2h or 1d" autocomplete="off" spellcheck="false" />
    </label>
    <label class="block text-sm font-medium">Next check (UTC) <input name="next_check_at" type="datetime-local" class={input} bind:value={form.nextCheck} /></label>
  {/if}
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
    <label class="block text-sm font-medium">Planned start <input name="planned_start" type="date" class={input} bind:value={form.plannedStart} /></label>
    <label class="block text-sm font-medium">Planned end <input name="planned_end" type="date" class={input} bind:value={form.plannedEnd} /></label>
  </div>

  <fieldset class="flex flex-col gap-2">
    <legend class="text-sm font-medium">Links</legend>
    {#each form.links as link, n (n)}
      <div class="flex flex-col gap-2 sm:flex-row sm:items-end" data-testid="edit-link">
        <input name="link_label_{n}" class={input} bind:value={link.label} aria-label="Label of link {n + 1}" autocomplete="off" />
        <input name="link_url_{n}" type="url" class={input} bind:value={link.url} aria-label="URL of link {n + 1}" autocomplete="off" spellcheck="false" />
        <button type="button" class="{button} shrink-0 border-border text-text" aria-label="Remove link {n + 1}" onclick={() => (form = removeEditLink(form, n))}>Remove</button>
      </div>
    {/each}
    <input name="link_label" class={input} bind:value={form.linkLabel} placeholder="Label" aria-label="New link label" autocomplete="off" />
    <input name="link_url" type="url" class={input} bind:value={form.linkUrl} placeholder="https://" aria-label="New link URL" autocomplete="off" spellcheck="false" />
    <button type="button" class="{button} self-start border-border text-text" onclick={() => (form = addEditLink(form))}>Add link</button>
  </fieldset>

  <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
    <button type="button" class="{button} border-border text-text" onclick={oncancel}>Cancel</button>
    <button type="submit" class="{button} border-accent text-accent" disabled={busy}>Save</button>
  </div>
</form>
