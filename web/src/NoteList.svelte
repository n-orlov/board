<script lang="ts">
  import { absoluteTime, relativeTime } from './lib/format';
  import { renderMarkdown } from './lib/markdown';
  import type { Note } from './lib/notes';
  import { taskHref } from './lib/taskPage';

  // A list of notes as the task page and the initiative page draw them (PRD R10): summary, author, time, resource and
  // an expand-to-body button for a note that has a body; the body is read by `onexpand` and arrives in `bodies`.
  // `showTask` names the task a note is on (the initiative page lists the notes of all its tasks).
  let { notes, bodies, testid, showTask = false, onexpand }: { notes: Note[]; bodies: Record<string, string>; testid: string; showTask?: boolean; onexpand: (ref: string) => void } = $props();

  let expanded: Record<string, boolean> = $state({});
  const now = new Date();
  const small = 'min-h-11 rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text';

  function toggle(ref: string) {
    expanded[ref] = !expanded[ref];
    if (expanded[ref]) onexpand(ref);
  }
</script>

<ul class="flex flex-col gap-2" data-testid={testid}>
  {#each notes as note (note.ref)}
    <li class="rounded-md border border-border p-3 text-sm wrap-anywhere" data-testid="note" data-ref={note.ref}>
      <p class="font-medium" data-testid="note-summary">{note.summary}</p>
      <p class="text-text-muted">
        <span data-testid="note-author">{note.author}</span> ·
        <time datetime={note.created_at} title={absoluteTime(note.created_at)} data-testid="note-when">{relativeTime(note.created_at, now)}</time>
        {#if showTask && note.task !== undefined}· <a class="inline-flex min-h-11 items-center text-accent hover:underline" href={taskHref(note.task)} data-testid="note-task">{note.task}</a>{/if}
        {#if note.resource !== undefined}· <span class="font-mono" data-testid="note-resource">{note.resource}</span>{/if}
      </p>
      {#if note.has_body}
        <button
          type="button"
          class="{small} mt-2"
          aria-expanded={expanded[note.ref] === true}
          aria-controls="note-body-{note.ref}"
          aria-label="{expanded[note.ref] === true ? 'Hide' : 'Show'} body of {note.ref}"
          onclick={() => toggle(note.ref)}>{expanded[note.ref] === true ? 'Hide body' : 'Show body'}</button
        >
        <div id="note-body-{note.ref}" hidden={expanded[note.ref] !== true}>
          {#if expanded[note.ref] === true}
            {#if bodies[note.ref] === undefined}
              <p class="text-text-muted">Loading…</p>
            {:else}
              <!-- renderMarkdown escapes the source first and writes only its own markup (lib/markdown.ts). -->
              <div class="description wrap-anywhere mt-2" data-testid="note-body">{@html renderMarkdown(bodies[note.ref] ?? '')}</div>
            {/if}
          {/if}
        </div>
      {/if}
    </li>
  {/each}
</ul>
