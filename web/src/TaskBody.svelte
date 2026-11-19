<script lang="ts" module>
  import type { Ask } from './lib/asks';
  import type { TaskDetail } from './lib/tasks';

  // What the full task page does with the body's buttons. The preview passes none, and the body is read-only.
  export interface TaskBodyActions {
    edit: (task: TaskDetail) => void;
    done: () => void;
    reopen: () => void;
    close: () => void;
    copyWayBack: (holder: string, wayBack: string) => void;
    ack: (lease: string) => void;
    endLease: (lease: string) => void;
    answer: (ask: Ask) => void;
    withdraw: (askRef: string) => void;
    addDep: () => void;
    removeDep: (blocker: string) => void;
    note: () => void;
    milestone: () => void;
    check: () => void;
  }
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';
  import AskList from './AskList.svelte';
  import NoteList from './NoteList.svelte';
  import TagChips from './TagChips.svelte';
  import type { TagEditor } from './lib/taskTags';
  import { taskConflictText, taskConflicts } from './lib/conflicts';
  import { nextCheckText } from './lib/durations';
  import { blockedReasons, orderAsks } from './lib/asks';
  import { attentionBadges } from './lib/attention';
  import { absoluteTime, relativeTime } from './lib/format';
  import { taskBadges } from './lib/initiativePage';
  import { isSafeUrl, renderMarkdown } from './lib/markdown';
  import { newestFirst } from './lib/notes';
  import { toneClasses } from './lib/presentation';
  import { taskActions } from './lib/taskActions';
  import { copyWayBackLabel, eventRows, expiredRows, kindText, leaseRows, plannedText, strandedPanel, taskHref, type TaskPageState } from './lib/taskPage';

  // The body of a task: header, leases and asks, description, links, dependencies, notes and events. The task page
  // draws it with its actions and the 2xl two-column grid (`wide`); the timeline's preview draws it read-only in a dialog.
  let {
    view,
    headingId,
    testid,
    on,
    actionError,
    wide = false,
    nav,
    tags,
    onexpand,
  }: {
    view: TaskPageState;
    headingId: string;
    testid: string;
    on?: TaskBodyActions;
    actionError?: string;
    wide?: boolean;
    nav?: Snippet;
    /** What makes the task's tag chips editable; none for the read-only preview. */
    tags?: TagEditor;
    onexpand: (noteRef: string) => void;
  } = $props();

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
  const link = 'inline-flex min-h-11 min-w-11 flex-wrap items-center gap-x-2 text-accent hover:underline';
  const heading = 'text-lg font-bold';
  const headingRow = 'mb-2 mt-6 flex flex-wrap items-center justify-between gap-2';
</script>

<section
  aria-labelledby={headingId}
  class={wide ? 'mx-auto w-full max-w-[2560px] pb-20 md:pb-0 2xl:grid 2xl:grid-cols-[minmax(0,1fr)_420px] 2xl:gap-x-6' : 'w-full'}
  data-testid={testid}
>
  <div class="min-w-0 {wide ? '2xl:col-start-1 2xl:row-start-1' : ''}">
    {@render nav?.()}

    {#if view.error !== undefined}
      <p role="alert" class="my-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">{view.error}</p>
    {/if}

    {#if view.loading}
      <div aria-busy="true" class="mt-4 flex flex-col gap-3" data-testid="task-skeleton">
        <span class="sr-only">Loading task…</span>
        <div class="h-6 w-2/3 animate-pulse rounded-md bg-border"></div>
        <div class="h-4 w-1/3 animate-pulse rounded-md bg-border"></div>
      </div>
    {:else if view.task !== undefined}
      {@const task = view.task}
      <header class="mb-4">
        <code class="wrap-anywhere font-mono text-sm text-text-muted" data-testid="task-ref">{task.ref}</code>
        <h2 id={headingId} class="wrap-anywhere text-xl font-bold" data-testid="task-title">{task.title}</h2>
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
        {#if task.tag_marks.length > 0 || tags !== undefined}
          <div class="mt-2" data-testid="task-tags-block"><TagChips marks={task.tag_marks} ref={task.ref} editor={tags} /></div>
        {/if}
        {#each reasons as reason (reason)}
          <p class="mt-1 text-sm text-warning wrap-anywhere" data-testid="blocked-reason">{reason}</p>
        {/each}
        <p class="mt-2 text-sm text-text-muted wrap-anywhere">
          Created <time datetime={task.created_at} title={absoluteTime(task.created_at)}>{relativeTime(task.created_at, now)}</time> by {task.created_by}
        </p>

        {#if actionError !== undefined}
          <p role="alert" class="mt-3 rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="action-error">{actionError}</p>
        {/if}

        <!-- On the phone the main actions sit in a bar above the tab bar; from 768 px they are part of the header. -->
        {#if on !== undefined && actions !== undefined}
        <div
          class="fixed inset-x-0 bottom-[calc(3.5rem+env(safe-area-inset-bottom))] z-20 flex gap-2 border-t border-border bg-surface-raised p-2 md:static md:mt-3 md:flex-wrap md:border-0 md:bg-transparent md:p-0"
          role="group"
          aria-label="Task actions"
          data-testid="task-actions"
        >
          {#if actions.edit}
            <button type="button" class={action} onclick={() => on.edit(task)}>Edit</button>
          {/if}
          {#if actions.done}
            <button type="button" class="{action} border-accent text-accent" onclick={on.done}>Done</button>
          {/if}
          {#if actions.reopen}
            <button type="button" class={action} onclick={on.reopen}>Reopen</button>
          {/if}
          {#if actions.close}
            <button type="button" class={action} onclick={on.close}>Close</button>
          {/if}
        </div>
        {/if}
      </header>
  {/if}
  </div>

  {#if view.task !== undefined}
    {@const task = view.task}
    <!-- The side column (2xl): what needs doing now. Below 1536 px it sits under the header, in flow. -->
    <aside aria-label="Leases and asks" class="mt-4 min-w-0 {wide ? '2xl:sticky 2xl:top-6 2xl:col-start-2 2xl:row-span-2 2xl:row-start-1 2xl:mt-0 2xl:max-h-[calc(100vh-3rem)] 2xl:self-start 2xl:overflow-y-auto' : ''}" data-testid="task-side">
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
        {#if on !== undefined}
        <div class="mt-3 flex flex-wrap gap-2">
          {#if stranded.wayBack !== undefined}
            {@const wayBack = stranded.wayBack}
            <button type="button" class={small} aria-label={copyWayBackLabel(stranded)} data-testid="copy-way-back" onclick={() => on.copyWayBack(stranded.holder, wayBack)}>Copy way back</button>
          {/if}
          <button type="button" class="{small} border-accent text-accent" aria-label="Acknowledge lease {stranded.lease}" data-testid="stranded-ack" onclick={() => on.ack(stranded.lease)}>Acknowledge</button>
        </div>
        {/if}
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
              {#if on !== undefined}<button type="button" class="{small} mt-2" aria-label="End lease {lease.ref}" onclick={() => on.endLease(lease.ref)}>End lease</button>{/if}
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
            {#if on !== undefined}<button type="button" class="{small} mt-2" aria-label="Acknowledge lease {lease.ref}" onclick={() => on.ack(lease.ref)}>Acknowledge</button>{/if}
          </li>
        {/each}
      </ul>
    {/if}

    <section data-testid="asks-section">
      <div class={headingRow}><h3 class={heading}>Asks</h3></div>
      {#if asks.length === 0}
        <p class="text-sm text-text-muted" data-testid="no-asks">No asks.</p>
      {:else}
        <AskList {asks} testid="task-asks" actions={on === undefined ? undefined : { onanswer: on.answer, onwithdraw: on.withdraw }} />
      {/if}
    </section>
    </aside>

    <div class="min-w-0 {wide ? '2xl:col-start-1 2xl:row-start-2' : ''}" data-testid="task-main">
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
      {#if on !== undefined}<button type="button" class={small} onclick={on.addDep}>Add dependency</button>{/if}
    </div>
    {#if task.deps.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-deps">Nothing blocks this task.</p>
    {:else}
      <ul data-testid="task-deps">
        {#each task.deps as dep (dep.ref)}
          <li class="wrap-anywhere" data-ref={dep.ref}>
            <a class={link} href={taskHref(dep.ref)}><code class="font-mono">{dep.ref}</code><span>{dep.title}</span></a>
            <span class="{badge} border-border text-text-muted">{dep.status}</span>
            {#if on !== undefined}<button type="button" class={small} aria-label="Remove dependency on {dep.ref}" onclick={() => on.removeDep(dep.ref)}>Remove</button>{/if}
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
      {#if on !== undefined}<button type="button" class={small} onclick={on.note}>Post note</button>{/if}
    </div>
    {#if notes.length === 0}
      <p class="text-sm text-text-muted" data-testid="no-notes">No notes.</p>
    {:else}
      <NoteList {notes} bodies={view.bodies} testid="task-notes" {onexpand} />
    {/if}

    <section data-testid="timeline-section">
      <div class={headingRow}>
        <h3 class={heading}>Timeline</h3>
        {#if on !== undefined}
        <div class="flex gap-2">
          <button type="button" class={small} onclick={on.milestone}>Milestone</button>
          {#if actions?.recordCheck}<button type="button" class={small} onclick={on.check}>Record check</button>{/if}
        </div>
        {/if}
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
                <span data-testid="event-actor">{event.actor}</span>{#if event.via !== ''} <span data-testid="event-via">{event.via}</span>{/if} · <span class="font-mono">{event.type}</span>
              </p>
            </li>
          {/each}
        </ol>
      {/if}
    </section>
    </div>
  {/if}
</section>
