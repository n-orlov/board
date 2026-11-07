<script lang="ts">
  import Dialog from './Dialog.svelte';
  import { InitiativesApi } from './lib/initiatives';
  import { addLink, emptyNewTask, pickerOptions, removeLink, toggleBlocker, type NewTaskForm } from './lib/newTask';
  import { currentBrowser } from './lib/session';
  import { NewTaskStore, openInitiatives, TasksApi, type NewTaskState } from './lib/tasks';

  // The New task dialog (PRD R10): one form in the one Dialog (a bottom sheet on the phone). `initiative` presets the
  // initiative select (the initiative page passes its own slug; the Tasks view passes none). A refusal shows inline and
  // keeps what was typed; success calls `oncreated` with the new ref.
  let {
    csrf,
    initiative,
    onclose,
    oncreated,
  }: { csrf: string; initiative: string; onclose: () => void; oncreated: (ref: string) => void } = $props();

  const browser = currentBrowser();
  const tasksApi = new TasksApi(browser, () => csrf);
  const store = new NewTaskStore({ openTasks: () => tasksApi.openTasks(), create: (body) => tasksApi.create(body), initiatives: openInitiatives(new InitiativesApi(browser, () => csrf)) }, (s) => (view = s));

  let view: NewTaskState = $state({ initiatives: [], openTasks: [], loading: true, error: undefined });
  // svelte-ignore state_referenced_locally -- the preset is read once, when the dialog opens
  let form: NewTaskForm = $state(emptyNewTask(initiative));
  let pick = $state('');
  let busy = $state(false);

  $effect(() => {
    void store.load();
  });

  // The preset initiative is always offered, even before (or without) the open list naming it.
  const slugs = $derived([...new Set([...(initiative === '' ? [] : [initiative]), ...view.initiatives.map((i) => i.slug)])]);
  const options = $derived(pickerOptions(view.openTasks, pick, form.blockedBy));
  const chosen = $derived(form.blockedBy.map((ref) => ({ ref, title: view.openTasks.find((t) => t.ref === ref)?.title ?? '' })));

  const input = 'mt-1 block min-h-11 w-full rounded-md border border-border bg-surface px-3 py-2 text-base text-text';
  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium disabled:opacity-60';
  const chip = 'inline-flex min-h-11 max-w-full items-center gap-2 rounded-md border border-border bg-surface px-3 text-sm wrap-anywhere';

  async function submit(event: Event) {
    event.preventDefault();
    busy = true;
    try {
      const ref = await store.create(form);
      if (ref !== undefined) oncreated(ref);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog label="New task" {onclose} initialFocus={initiative === '' ? 'select[name=initiative]' : 'input[name=title]'}>
  <form onsubmit={submit} class="flex flex-col gap-4" data-testid="new-task-form">
    <h2 class="text-lg font-bold">New task</h2>

    {#if view.error !== undefined}
      <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="dialog-error">{view.error}</p>
    {/if}

    <div class="text-sm font-medium">
      <label for="new-task-initiative">Initiative</label>
      <select id="new-task-initiative" name="initiative" class={input} bind:value={form.initiative}>
        {#if form.initiative === ''}<option value="">Choose an initiative</option>{/if}
        {#each slugs as slug (slug)}<option value={slug}>{slug}</option>{/each}
      </select>
    </div>
    <label class="block text-sm font-medium">Title <input name="title" class={input} bind:value={form.title} /></label>
    <label class="block text-sm font-medium">Description <textarea name="description" rows="3" class={input} bind:value={form.description}></textarea></label>
    <div class="text-sm font-medium">
      <label for="new-task-kind">Kind</label>
      <select id="new-task-kind" name="kind" class={input} bind:value={form.kind}>
        <option value="work">Work</option>
        <option value="watch">Watch</option>
      </select>
    </div>
    {#if form.kind === 'watch'}
      <label class="block text-sm font-medium"
        >Cadence <input name="cadence" class={input} bind:value={form.cadence} placeholder="15m, 2h or 1d" autocomplete="off" spellcheck="false" />
      </label>
    {/if}
    <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <label class="block text-sm font-medium">Planned start <input name="planned_start" type="date" class={input} bind:value={form.plannedStart} /></label>
      <label class="block text-sm font-medium">Planned end <input name="planned_end" type="date" class={input} bind:value={form.plannedEnd} /></label>
    </div>

    <fieldset class="flex flex-col gap-2">
      <legend class="text-sm font-medium">Blocked by</legend>
      {#if chosen.length > 0}
        <ul class="flex flex-wrap gap-2" data-testid="blockers">
          {#each chosen as blocker (blocker.ref)}
            <li class={chip}>
              <code class="font-mono">{blocker.ref}</code>
              <button type="button" class="min-h-11 min-w-11 text-text-muted" aria-label="Remove blocker {blocker.ref}" onclick={() => (form = toggleBlocker(form, blocker.ref))}>×</button>
            </li>
          {/each}
        </ul>
      {/if}
      <input name="blocker_search" type="search" class={input} bind:value={pick} placeholder="Find an open task" aria-label="Find an open task" autocomplete="off" />
      {#if options.length > 0}
        <ul class="flex flex-col gap-1" data-testid="blocker-options">
          {#each options as task (task.id)}
            <li>
              <button type="button" class="flex min-h-11 w-full items-center gap-2 rounded-md border border-border px-3 text-left text-sm wrap-anywhere" onclick={() => (form = toggleBlocker(form, task.ref))}>
                <code class="font-mono">{task.ref}</code><span>{task.title}</span>
              </button>
            </li>
          {/each}
        </ul>
      {:else if !view.loading}
        <p class="text-sm text-text-muted">No open task matches.</p>
      {/if}
    </fieldset>

    <fieldset class="flex flex-col gap-2">
      <legend class="text-sm font-medium">Links</legend>
      {#if form.links.length > 0}
        <ul class="flex flex-col gap-2" data-testid="links">
          {#each form.links as link, n (n)}
            <li class={chip}>
              <span>{link.label}</span>
              <span class="text-text-muted">{link.url}</span>
              <button type="button" class="min-h-11 min-w-11 text-text-muted" aria-label="Remove link {link.label}" onclick={() => (form = removeLink(form, n))}>×</button>
            </li>
          {/each}
        </ul>
      {/if}
      <input name="link_label" class={input} bind:value={form.linkLabel} placeholder="Label" aria-label="Link label" autocomplete="off" />
      <input name="link_url" type="url" class={input} bind:value={form.linkUrl} placeholder="https://" aria-label="Link URL" autocomplete="off" spellcheck="false" />
      <button type="button" class="{button} self-start border-border text-text" onclick={() => (form = addLink(form))}>Add link</button>
    </fieldset>

    <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
      <button type="button" class="{button} border-border text-text" onclick={onclose}>Cancel</button>
      <button type="submit" class="{button} border-accent text-accent" disabled={busy}>Create task</button>
    </div>
  </form>
</Dialog>
