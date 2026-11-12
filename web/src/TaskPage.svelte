<script lang="ts">
  import AnswerForm from './AnswerForm.svelte';
  import AskList from './AskList.svelte';
  import Dialog from './Dialog.svelte';
  import InitiativeConfirm from './InitiativeConfirm.svelte';
  import NoteForm from './NoteForm.svelte';
  import NoteList from './NoteList.svelte';
  import TaskCloseForm from './TaskCloseForm.svelte';
  import TaskDepForm from './TaskDepForm.svelte';
  import TaskEditForm from './TaskEditForm.svelte';
  import TaskNoteForm from './TaskNoteForm.svelte';
  import Toast from './Toast.svelte';
  import { taskConflictText, taskConflicts } from './lib/conflicts';
  import { nextCheckText } from './lib/durations';
  import { blockedReasons, orderAsks, type Ask } from './lib/asks';
  import { copyText } from './lib/clipboard';
  import { attentionBadges } from './lib/attention';
  import { absoluteTime, relativeTime } from './lib/format';
  import { taskBadges } from './lib/initiativePage';
  import { isSafeUrl, renderMarkdown } from './lib/markdown';
  import { newestFirst, type NoteForm as PostForm } from './lib/notes';
  import { toneClasses } from './lib/presentation';
  import { currentBrowser } from './lib/session';
  import { taskActions } from './lib/taskActions';
  import { toTaskEditForm, type TaskEditForm as EditForm } from './lib/taskEdit';
  import { ACK_BODY, affectsTaskPage, copyWayBackLabel, eventRows, expiredRows, forceDoneBody, kindText, leaseHolders, leaseRows, plannedText, strandedPanel, taskHref, TaskPageStore, type TaskPageState } from './lib/taskPage';
  import { TasksApi, type Resolution } from './lib/tasks';
  import { live } from './lib/live';

  let { csrf, param }: { csrf: string; param: string } = $props();

  let view: TaskPageState = $state({ task: undefined, events: [], expired: [], live: [], loading: true, error: undefined, actionError: undefined, candidates: [], asks: [], notes: [], bodies: {} });
  let dialog: 'edit' | 'close' | 'dep' | 'milestone' | 'check' | 'note' | { endLease: string } | { answer: Ask } | { withdraw: string } | { ack: string } | 'force-done' | undefined = $state(undefined);
  let editInitial: EditForm | undefined = $state(undefined);
  let toast = $state('');

  const store = new TaskPageStore(new TasksApi(currentBrowser(), () => csrf), (s) => (view = s));

  $effect(() => {
    void store.load(param);
    // An event that concerns the task reads the page again in place: no skeleton, so an open dialog keeps what was typed.
    return live().subscribe(affectsTaskPage(param), () => store.refresh(param));
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

  function openDep() {
    store.clearActionError();
    dialog = 'dep';
    void store.loadCandidates();
  }

  function openNote(kind: 'milestone' | 'check' | 'note' | 'force-done' | { endLease: string } | { answer: Ask } | { withdraw: string } | { ack: string }) {
    store.clearActionError();
    dialog = kind;
  }

  async function finish(done: Promise<boolean>, message: string) {
    if (await done) {
      dialog = undefined;
      toast = message;
    }
  }

  const done = (force = false) => finish(store.done(ref, force), `Task ${ref} marked done`);
  // Done over another actor's live lease asks first and then sends `force`; a task nobody else holds is marked done at once.
  const pressDone = () => (leaseHolders(view.task ?? { live_leases: [] }).length > 0 ? openNote('force-done') : done());
  const reopen = () => finish(store.reopen(ref), `Task ${ref} reopened`);
  const save = (form: EditForm, kind: string) => finish(store.edit(ref, form, editInitial ?? form, kind), `Task ${ref} saved`);
  const closeIt = (resolution: Resolution) => finish(store.close(ref, resolution), `Task ${ref} closed as ${resolution}`);

  const addDep = (blocker: string) => finish(store.addDep(ref, blocker), `Task ${ref} is now blocked by ${blocker}`);
  const removeDep = (blocker: string) => finish(store.removeDep(ref, blocker), `Task ${ref} no longer waits for ${blocker}`);
  const milestone = (text: string) => finish(store.milestone(ref, text), `Milestone recorded on ${ref}`);
  const check = (text: string) => finish(store.recordCheck(ref, text), `Check recorded on ${ref}`);
  const endLease = (lease: string, outcome: string) => finish(store.endLease(ref, lease, outcome), `Lease ${lease} ended`);
  const answer = (ask: Ask, text: string) => finish(store.answer(ref, ask.ref, text), `Answered ${ask.ref}`);
  const withdraw = (askRef: string, text: string) => finish(store.withdraw(ref, askRef, text), `Withdrew ${askRef}`);
  const postNote = (form: PostForm) => finish(store.postNote(ref, form), `Note posted on ${ref}`);

  const ack = (lease: string) => finish(store.ackLease(ref, lease), `Lease ${lease} acknowledged`);

  async function copyWayBack(holder: string, wayBack: string) {
    const ok = await copyText(navigator.clipboard, wayBack);
    toast = ok ? `Copied the way back of ${holder}` : 'Copying failed. Select the way back and copy it by hand.';
  }

  // The clock is read when the page's data changes, so a refresh restates "expires in" and "ago".
  const now = $derived.by(() => {
    void view;
    return new Date();
  });
  const actions = $derived(view.task === undefined ? undefined : taskActions(view.task.status, view.task.kind));
  const leases = $derived(view.task === undefined ? [] : leaseRows(view.task, now));
  const stranded = $derived(view.task === undefined ? undefined : strandedPanel(view.task, now));
  const expired = $derived(expiredRows(view.expired, now, stranded?.lease));
  const conflicts = $derived(view.task === undefined ? [] : taskConflicts(view.task.ref, view.live, new Date()));
  const asks = $derived(orderAsks(view.asks));
  const reasons = $derived(blockedReasons(view.asks));
  const notes = $derived(newestFirst(view.notes));
  const timeline = $derived(eventRows(view.events, now));
  const small = 'min-h-11 rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text';
  const badge = 'inline-block rounded-md border px-2 py-0.5 text-xs font-medium';
  const action = 'min-h-11 flex-1 rounded-md border border-border bg-surface-raised px-4 text-sm font-medium text-text md:flex-none';
  const back = 'inline-flex min-h-11 items-center text-sm font-medium text-accent hover:underline';
  const link = 'inline-flex min-h-11 min-w-11 flex-wrap items-center gap-x-2 text-accent hover:underline';
  const heading = 'text-lg font-bold';
  const headingRow = 'mb-2 mt-6 flex flex-wrap items-center justify-between gap-2';
</script>

<section
  aria-labelledby="task-heading"
  class="mx-auto w-full max-w-[2560px] pb-20 md:pb-0 2xl:grid 2xl:grid-cols-[minmax(0,1fr)_420px] 2xl:gap-x-6"
  data-testid="task-page"
>
  <div class="min-w-0 2xl:col-start-1 2xl:row-start-1">
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
          {#each attentionBadges(task.attention) as badge_ (badge_.reason)}
            <span class="{badge} {toneClasses(badge_.tone)}" role="img" aria-label={badge_.name} data-testid="attention-{badge_.reason}">{badge_.label}</span>
          {/each}
          {#each taskBadges(task) as flag (flag.key)}
            <span class="{badge} {toneClasses(flag.tone)}" data-testid="flag-{flag.key}">{flag.label}</span>
          {/each}
        </div>
        {#each reasons as reason (reason)}
          <p class="mt-1 text-sm text-warning wrap-anywhere" data-testid="blocked-reason">{reason}</p>
        {/each}
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
            <button type="button" class="{action} border-accent text-accent" onclick={pressDone}>Done</button>
          {/if}
          {#if actions.reopen}
            <button type="button" class={action} onclick={reopen}>Reopen</button>
          {/if}
          {#if actions.close}
            <button type="button" class={action} onclick={openClose}>Close</button>
          {/if}
        </div>
      </header>
  {/if}
  </div>

  {#if view.task !== undefined && actions !== undefined}
    {@const task = view.task}
    <!-- The side column (2xl): what needs doing now. Below 1536 px it sits under the header, in flow. -->
    <aside aria-label="Leases and asks" class="mt-4 min-w-0 2xl:sticky 2xl:top-6 2xl:col-start-2 2xl:row-span-2 2xl:row-start-1 2xl:mt-0 2xl:max-h-[calc(100vh-3rem)] 2xl:self-start 2xl:overflow-y-auto" data-testid="task-side">
    {#if stranded !== undefined}
      <section class="mb-4 rounded-md border border-warning p-3 text-sm wrap-anywhere" aria-labelledby="stranded-heading" data-testid="stranded-panel" data-lease={stranded.lease}>
        <h3 id="stranded-heading" class="text-lg font-bold">Stranded</h3>
        <p class="text-text-muted">Its lease ran out and nobody acknowledged it.</p>
        <dl class="mt-2 grid grid-cols-1 gap-x-6 gap-y-1 sm:grid-cols-[auto_1fr]">
          <dt class="text-text-muted">Holder</dt>
          <dd class="font-medium" data-testid="stranded-holder">{stranded.holder}</dd>
          <dt class="text-text-muted">Lease</dt>
          <dd data-testid="stranded-lease">{stranded.lease}</dd>
          <dt class="text-text-muted">Intent</dt>
          <dd data-testid="stranded-intent">{stranded.intent}</dd>
          <dt class="text-text-muted">Last renewal</dt>
          <dd data-testid="stranded-renewed"><time datetime={stranded.renewedAt} title={stranded.renewedTitle}>{stranded.renewed}</time></dd>
          <dt class="text-text-muted">Expiry</dt>
          <dd data-testid="stranded-expired"><time datetime={stranded.expiredAt} title={stranded.expiredTitle}>{stranded.expired}</time></dd>
          <dt class="text-text-muted">Way back</dt>
          <dd>
            {#if stranded.wayBack !== undefined}
              <code class="font-mono" data-testid="stranded-way-back">{stranded.wayBack}</code>
            {:else}
              <span class="text-text-muted" data-testid="stranded-no-way-back">no way back on record</span>
            {/if}
          </dd>
        </dl>
        <div class="mt-3 flex flex-wrap gap-2">
          {#if stranded.wayBack !== undefined}
            {@const wayBack = stranded.wayBack}
            <button type="button" class={small} aria-label={copyWayBackLabel(stranded)} data-testid="copy-way-back" onclick={() => void copyWayBack(stranded.holder, wayBack)}>Copy way back</button>
          {/if}
          <button type="button" class="{small} border-accent text-accent" aria-label="Acknowledge lease {stranded.lease}" data-testid="stranded-ack" onclick={() => openNote({ ack: stranded.lease })}>Acknowledge</button>
        </div>
      </section>
    {/if}

    <section data-testid="live-leases-section">
      <div class={headingRow}><h3 class={heading}>Live leases</h3></div>
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
              <button type="button" class="{small} mt-2" aria-label="End lease {lease.ref}" onclick={() => openNote({ endLease: lease.ref })}>End lease</button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    {#if conflicts.length > 0}
      <div class={headingRow}><h3 class={heading}>Conflicts</h3></div>
      <ul class="flex flex-col gap-2" data-testid="task-conflicts">
        {#each conflicts as conflict (conflict.resource)}
          <li class="rounded-md border border-warning p-3 text-sm wrap-anywhere" data-testid="conflict" data-resource={conflict.resource}>
            <p data-testid="conflict-text">{taskConflictText(conflict)}</p>
            <p class="flex flex-wrap gap-x-3">
              {#each conflict.others as other (other.lease)}
                <a class={link} href={taskHref(other.task)} data-testid="conflict-task" data-ref={other.task}>{other.task}</a>
              {/each}
            </p>
          </li>
        {/each}
      </ul>
    {/if}

    {#if expired.length > 0}
      <div class={headingRow}><h3 class={heading}>Expired leases</h3></div>
      <ul class="flex flex-col gap-2" data-testid="task-expired">
        {#each expired as lease (lease.ref)}
          <li class="rounded-md border border-border bg-surface-raised p-3 text-sm wrap-anywhere" data-testid="expired-lease" data-ref={lease.ref}>
            <p><span class="font-medium">{lease.holder}</span> <span class="text-text-muted">{lease.ref}</span></p>
            <p>{lease.intent}</p>
            {#if lease.resources !== ''}<p class="font-mono text-text-muted">{lease.resources}</p>{/if}
            <p class="text-text-muted" data-testid="expired-when">{lease.expired}</p>
            <button type="button" class="{small} mt-2" aria-label="Acknowledge lease {lease.ref}" onclick={() => openNote({ ack: lease.ref })}>Acknowledge</button>
          </li>
        {/each}
      </ul>
    {/if}

    <section data-testid="asks-section">
      <div class={headingRow}><h3 class={heading}>Asks</h3></div>
      {#if asks.length === 0}
        <p class="text-sm text-text-muted" data-testid="no-asks">No asks.</p>
      {:else}
        <AskList {asks} testid="task-asks" onanswer={(ask) => openNote({ answer: ask })} onwithdraw={(askRef) => openNote({ withdraw: askRef })} />
      {/if}
    </section>
    </aside>

    <div class="min-w-0 2xl:col-start-1 2xl:row-start-2" data-testid="task-main">
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
        <dd data-testid="task-next-check"><time datetime={task.next_check_at} title={absoluteTime(task.next_check_at)}>{nextCheckText(task.next_check_at, now)}</time></dd>
      {/if}
    </dl>

    <div class={headingRow}><h3 class={heading}>Links</h3></div>
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

    <div class={headingRow}>
      <h3 class={heading}>Blocked by</h3>
      <button type="button" class={small} onclick={openDep}>Add dependency</button>
    </div>
    {#if task.deps.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-deps">Nothing blocks this task.</p>
    {:else}
      <ul data-testid="task-deps">
        {#each task.deps as dep (dep.ref)}
          <li class="wrap-anywhere" data-ref={dep.ref}>
            <a class={link} href={taskHref(dep.ref)}><code class="font-mono">{dep.ref}</code><span>{dep.title}</span></a>
            <span class="{badge} border-border text-text-muted">{dep.status}</span>
            <button type="button" class={small} aria-label="Remove dependency on {dep.ref}" onclick={() => removeDep(dep.ref)}>Remove</button>
          </li>
        {/each}
      </ul>
    {/if}

    <div class={headingRow}><h3 class={heading}>Blocking</h3></div>
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

    <div class={headingRow}>
      <h3 class={heading}>Notes</h3>
      <button type="button" class={small} onclick={() => openNote('note')}>Post note</button>
    </div>
    {#if notes.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-notes">No notes.</p>
    {:else}
      <NoteList {notes} bodies={view.bodies} testid="task-notes" onexpand={(noteRef) => void store.loadBody(noteRef)} />
    {/if}

    <section data-testid="timeline-section">
      <div class={headingRow}>
        <h3 class={heading}>Timeline</h3>
        <div class="flex gap-2">
          <button type="button" class={small} onclick={() => openNote('milestone')}>Milestone</button>
          {#if actions.recordCheck}<button type="button" class={small} onclick={() => openNote('check')}>Record check</button>{/if}
        </div>
      </div>
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
    </section>
    </div>
  {/if}
</section>

<Toast message={toast} onclear={() => (toast = '')} />

{#if dialog !== undefined && view.task !== undefined}
  {@const task = view.task}
  {#if dialog === 'edit' && editInitial !== undefined}
    <Dialog label="Edit task" onclose={() => (dialog = undefined)} initialFocus="input[name=title]">
      <TaskEditForm initial={editInitial} watch={task.kind === 'watch'} error={view.actionError} onsave={(form) => save(form, task.kind)} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if dialog === 'dep'}
    <Dialog label="Add dependency" onclose={() => (dialog = undefined)} initialFocus="input[name=blocker_search]">
      <TaskDepForm {task} candidates={view.candidates} error={view.actionError} onpick={addDep} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if dialog === 'milestone'}
    <Dialog label="Record milestone" onclose={() => (dialog = undefined)} initialFocus="input[name=text]">
      <TaskNoteForm heading="Record milestone" label="Summary" hint="What happened, in up to 200 characters." submitLabel="Record milestone" error={view.actionError} onsubmit={milestone} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if dialog === 'check'}
    <Dialog label="Record check" onclose={() => (dialog = undefined)} initialFocus="input[name=text]">
      <TaskNoteForm heading="Record check" label="Summary" hint="What the check found, in up to 200 characters. The next check is due one cadence from now." submitLabel="Record check" error={view.actionError} onsubmit={check} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if dialog === 'note'}
    <Dialog label="Post note" onclose={() => (dialog = undefined)} initialFocus="input[name=summary]">
      <NoteForm heading="Post a note on {task.ref}" error={view.actionError} onsubmit={postNote} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if typeof dialog === 'object' && 'answer' in dialog}
    {@const target = dialog.answer}
    <Dialog label="Answer ask" onclose={() => (dialog = undefined)} initialFocus="textarea[name=answer]">
      <AnswerForm ask={target} error={view.actionError} onsubmit={(text) => answer(target, text)} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if typeof dialog === 'object' && 'withdraw' in dialog}
    {@const askRef = dialog.withdraw}
    <Dialog label="Withdraw ask" onclose={() => (dialog = undefined)} initialFocus="input[name=text]">
      <TaskNoteForm heading="Withdraw {askRef}" label="Reason (optional)" hint="Why the ask is no longer needed, in up to 200 characters." submitLabel="Withdraw" error={view.actionError} onsubmit={(text) => withdraw(askRef, text)} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if dialog === 'force-done'}
    {@const holders = leaseHolders(view.task ?? { live_leases: [] })}
    <Dialog label="Mark {ref} done" onclose={() => (dialog = undefined)}>
      <InitiativeConfirm heading="Mark {ref} done" body={forceDoneBody(holders)} confirm="Mark done anyway" danger error={view.actionError} onconfirm={() => done(true)} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if typeof dialog === 'object' && 'ack' in dialog}
    {@const lease = dialog.ack}
    <Dialog label="Acknowledge lease {lease}" onclose={() => (dialog = undefined)}>
      <InitiativeConfirm heading="Acknowledge {lease}" body={ACK_BODY} confirm="Acknowledge lease" error={view.actionError} onconfirm={() => ack(lease)} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if typeof dialog === 'object'}
    {@const lease = dialog.endLease}
    <Dialog label="End lease" onclose={() => (dialog = undefined)} initialFocus="input[name=text]">
      <TaskNoteForm heading="End lease {lease}" label="Outcome (optional)" hint="How it went, in up to 200 characters." submitLabel="End lease" error={view.actionError} onsubmit={(text) => endLease(lease, text)} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {:else if dialog === 'close'}
    <Dialog label="Close task" onclose={() => (dialog = undefined)} initialFocus="select[name=resolution]">
      <TaskCloseForm ref={task.ref} error={view.actionError} onconfirm={closeIt} oncancel={() => (dialog = undefined)} />
    </Dialog>
  {/if}
{/if}
