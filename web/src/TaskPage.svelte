<script lang="ts">
  import Dialog from './Dialog.svelte';
  import TaskCloseForm from './TaskCloseForm.svelte';
  import TaskEditForm from './TaskEditForm.svelte';
  import Toast from './Toast.svelte';
  import { absoluteTime, relativeTime } from './lib/format';
  import { taskBadges } from './lib/initiativePage';
  import { isSafeUrl, renderMarkdown } from './lib/markdown';
  import { toneClasses } from './lib/presentation';
  import { currentBrowser } from './lib/session';
  import { taskActions } from './lib/taskActions';
  import { toTaskEditForm, type TaskEditForm as EditForm } from './lib/taskEdit';
  import { eventRows, kindText, leaseRows, plannedText, taskHref, TaskPageStore, type TaskPageState } from './lib/taskPage';
  import { TasksApi, type Resolution } from './lib/tasks';

  let { csrf, param }: { csrf: string; param: string } = $props();

  let view: TaskPageState = $state({ task: undefined, events: [], loading: true, error: undefined, actionError: undefined });
  let dialog: 'edit' | 'close' | undefined = $state(undefined);
  let editInitial: EditForm | undefined = $state(undefined);
  let toast = $state('');

  const store = new TaskPageStore(new TasksApi(currentBrowser(), () => csrf), (s) => (view = s));

  $effect(() => {
    void store.load(param);
  });

  // The refs of the task page are written in either letter case; the API answers with the canonical ref.
  const ref = $derived(view.task?.ref ?? param);

  function openEdit(initial: EditForm) {
    store.clearActionError();
    editInitial = initial;
    dialog = 'edit';
  }

  function openClose() {
    store.clearActionError();
    dialog = 'close';
  }

  async function finish(done: Promise<boolean>, message: string) {
    if (await done) {
      dialog = undefined;
      toast = message;
    }
  }

  const done = () => finish(store.done(ref), `Task ${ref} marked done`);
  const reopen = () => finish(store.reopen(ref), `Task ${ref} reopened`);
  const save = (form: EditForm, kind: string) => finish(store.edit(ref, form, editInitial ?? form, kind), `Task ${ref} saved`);
  const closeIt = (resolution: Resolution) => finish(store.close(ref, resolution), `Task ${ref} closed as ${resolution}`);

  const now = new Date();
  const actions = $derived(view.task === undefined ? undefined : taskActions(view.task.status, view.task.kind));
  const leases = $derived(view.task === undefined ? [] : leaseRows(view.task, now));
  const timeline = $derived(eventRows(view.events, now));
  const badge = 'inline-block rounded-md border px-2 py-0.5 text-xs font-medium';
  const action = 'min-h-11 flex-1 rounded-md border border-border bg-surface-raised px-4 text-sm font-medium text-text md:flex-none';
  const back = 'inline-flex min-h-11 items-center text-sm font-medium text-accent hover:underline';
  const link = 'inline-flex min-h-11 flex-wrap items-center gap-x-2 text-accent hover:underline';
  const heading = 'mb-2 mt-6 text-lg font-bold';
</script>

<section aria-labelledby="task-heading" class="pb-20 md:pb-0" data-testid="task-page">
  <div class="flex flex-wrap items-center gap-x-6">
    <a class={back} href="#/tasks" data-testid="back-link">← Tasks</a>
    {#if view.task !== undefined}
      <a class={back} href="#/i/{view.task.initiative}" data-testid="task-initiative">{view.task.initiative}</a>
    {/if}
  </div>

  {#if view.error !== undefined}
    <p role="alert" class="my-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">{view.error}</p>
  {/if}

  {#if view.loading}
    <div aria-busy="true" class="mt-4 flex flex-col gap-3" data-testid="task-skeleton">
      <span class="sr-only">Loading task…</span>
      <div class="h-6 w-2/3 animate-pulse rounded-md bg-border"></div>
      <div class="h-4 w-1/3 animate-pulse rounded-md bg-border"></div>
    </div>
  {:else if view.task !== undefined && actions !== undefined}
    {@const task = view.task}
    <header class="mb-4">
      <code class="wrap-anywhere font-mono text-sm text-text-muted" data-testid="task-ref">{task.ref}</code>
      <h2 id="task-heading" class="wrap-anywhere text-xl font-bold" data-testid="task-title">{task.title}</h2>
      <div class="mt-1 flex flex-wrap items-center gap-2">
        <span class="{badge} border-border text-text-muted" data-testid="task-status">{task.status}</span>
        {#if task.resolution !== undefined}
          <span class="{badge} border-border text-text-muted" data-testid="task-resolution">{task.resolution}</span>
        {/if}
        <span class="text-sm text-text-muted" data-testid="task-kind">{kindText(task)}</span>
        {#each taskBadges(task) as flag (flag.key)}
          <span class="{badge} {toneClasses(flag.tone)}" data-testid="flag-{flag.key}">{flag.label}</span>
        {/each}
      </div>
      <p class="mt-2 text-sm text-text-muted">
        Created <time datetime={task.created_at} title={absoluteTime(task.created_at)}>{relativeTime(task.created_at, now)}</time> by {task.created_by}
      </p>

      {#if view.actionError !== undefined && dialog === undefined}
        <p role="alert" class="mt-3 rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="action-error">{view.actionError}</p>
      {/if}

      <!-- On the phone the main actions sit in a bar above the tab bar; from 768 px they are part of the header. -->
      <div
        class="fixed inset-x-0 bottom-[calc(3.5rem+env(safe-area-inset-bottom))] z-20 flex gap-2 border-t border-border bg-surface-raised p-2 md:static md:mt-3 md:flex-wrap md:border-0 md:bg-transparent md:p-0"
        role="group"
        aria-label="Task actions"
        data-testid="task-actions"
      >
        {#if actions.edit}
          <button type="button" class={action} onclick={() => openEdit(toTaskEditForm(task))}>Edit</button>
        {/if}
        {#if actions.done}
          <button type="button" class="{action} border-accent text-accent" onclick={done}>Done</button>
        {/if}
        {#if actions.reopen}
          <button type="button" class={action} onclick={reopen}>Reopen</button>
        {/if}
        {#if actions.close}
          <button type="button" class={action} onclick={openClose}>Close</button>
        {/if}
      </div>
    </header>

    {#if task.description !== ''}
      <!-- renderMarkdown escapes the source first and writes only its own markup (lib/markdown.ts). -->
      <div class="description wrap-anywhere" data-testid="task-description">{@html renderMarkdown(task.description)}</div>
    {/if}

    <dl class="mt-4 grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-[auto_1fr]">
      {#if plannedText(task) !== ''}
        <dt class="text-text-muted">Planned</dt>
        <dd data-testid="task-planned">{plannedText(task)}</dd>
      {/if}
      {#if task.next_check_at !== undefined}
        <dt class="text-text-muted">Next check</dt>
        <dd data-testid="task-next-check"><time datetime={task.next_check_at} title={absoluteTime(task.next_check_at)}>{relativeTime(task.next_check_at, now)}</time></dd>
      {/if}
    </dl>

    <h3 class={heading}>Links</h3>
    {#if task.external_refs.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-links">No links.</p>
    {:else}
      <ul data-testid="task-links">
        {#each task.external_refs as external, n (n)}
          <li class="wrap-anywhere">
            {#if isSafeUrl(external.url)}
              <a class={link} href={external.url} rel="noopener noreferrer" target="_blank">{external.label}</a>
            {:else}
              <span>{external.label}</span>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}

    <h3 class={heading}>Blocked by</h3>
    {#if task.deps.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-deps">Nothing blocks this task.</p>
    {:else}
      <ul data-testid="task-deps">
        {#each task.deps as dep (dep.ref)}
          <li class="wrap-anywhere" data-ref={dep.ref}>
            <a class={link} href={taskHref(dep.ref)}><code class="font-mono">{dep.ref}</code><span>{dep.title}</span></a>
            <span class="{badge} border-border text-text-muted">{dep.status}</span>
          </li>
        {/each}
      </ul>
    {/if}

    <h3 class={heading}>Blocking</h3>
    {#if task.blocking.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-blocking">This task blocks nothing.</p>
    {:else}
      <ul data-testid="task-blocking">
        {#each task.blocking as dep (dep.ref)}
          <li class="wrap-anywhere" data-ref={dep.ref}>
            <a class={link} href={taskHref(dep.ref)}><code class="font-mono">{dep.ref}</code><span>{dep.title}</span></a>
            <span class="{badge} border-border text-text-muted">{dep.status}</span>
          </li>
        {/each}
      </ul>
    {/if}

    <h3 class={heading}>Live leases</h3>
    {#if leases.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-leases">No live lease.</p>
    {:else}
      <ul class="flex flex-col gap-2" data-testid="task-leases">
        {#each leases as lease (lease.ref)}
          <li class="rounded-md border border-border bg-surface-raised p-3 text-sm wrap-anywhere" data-testid="lease" data-ref={lease.ref}>
            <p><span class="font-medium" data-testid="lease-holder">{lease.holder}</span> <span class="text-text-muted" data-testid="lease-ref">{lease.ref}</span></p>
            <p data-testid="lease-intent">{lease.intent}</p>
            {#if lease.resources !== ''}<p class="font-mono text-text-muted" data-testid="lease-resources">{lease.resources}</p>{/if}
            <p class="text-text-muted" data-testid="lease-expires">{lease.expires}</p>
          </li>
        {/each}
      </ul>
    {/if}

    <h3 class={heading}>Timeline</h3>
    {#if timeline.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-events">No events.</p>
    {:else}
      <ol class="flex flex-col gap-2" data-testid="task-timeline">
        {#each timeline as event (event.id)}
          <li class="rounded-md border border-border p-3 text-sm wrap-anywhere" data-testid="event" data-type={event.type}>
            <p data-testid="event-summary">{event.summary}</p>
            <p class="text-text-muted">
              <time datetime={event.at} title={event.title} data-testid="event-when">{event.when}</time> ·
              <span data-testid="event-actor">{event.actor}</span> · <span class="font-mono">{event.type}</span>
            </p>
          </li>
        {/each}
      </ol>
    {/if}
  {/if}
</section>

<Toast message={toast} onclear={() => (toast = '')} />

{#if dialog !== undefined && view.task !== undefined}
  {@const task = view.task}
  {#if dialog === 'edit' && editInitial !== undefined}
    <Dialog label="Edit task" onclose={() => (dialog = undefined)} initialFocus="input[name=title]">
      <TaskEditForm initial={editInitial} watch={task.kind === 'watch'} error={view.actionError} onsave={(form) => save(form, task.kind)} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if dialog === 'close'}
    <Dialog label="Close task" onclose={() => (dialog = undefined)} initialFocus="select[name=resolution]">
      <TaskCloseForm ref={task.ref} error={view.actionError} onconfirm={closeIt} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {/if}
{/if}
