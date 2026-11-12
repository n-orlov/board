<script lang="ts">
  import { blockingLabel, canAct, modeLabel, stateLabel, stateLine, type Ask } from './lib/asks';
  import { absoluteTime, relativeTime } from './lib/format';
  import { renderMarkdown } from './lib/markdown';
  import { taskHref } from './lib/taskPage';

  // A list of asks as the task page and the initiative page draw them (PRD R10): ref, state, blocking, question, who
  // asked and how, details as Markdown, and for an answered ask the answer. An open ask carries Answer and Withdraw.
  // `showTask` names the task an ask is on (the initiative page lists the asks of all its tasks).
  let {
    asks,
    testid,
    showTask = false,
    onanswer,
    onwithdraw,
  }: { asks: Ask[]; testid: string; showTask?: boolean; onanswer: (ask: Ask) => void; onwithdraw: (ref: string) => void } = $props();

  const now = new Date();
  const small = 'min-h-11 rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text';
  const badge = 'inline-block rounded-md border px-2 py-0.5 text-xs font-medium';
</script>

<ul class="flex flex-col gap-2" data-testid={testid}>
  {#each asks as ask (ask.ref)}
    <li class="rounded-md border border-border bg-surface-raised p-3 text-sm wrap-anywhere" data-testid="ask" data-ref={ask.ref} data-state={ask.state}>
      <p>
        <code class="font-mono text-text-muted" data-testid="ask-ref">{ask.ref}</code>
        <span class="{badge} border-border text-text-muted" data-testid="ask-state">{stateLabel(ask.state)}</span>
        {#if blockingLabel(ask) !== ''}<span class="{badge} border-warning text-warning" data-testid="ask-blocking">{blockingLabel(ask)}</span>{/if}
        {#if showTask && ask.task !== undefined}<a class="inline-flex min-h-11 items-center text-accent hover:underline" href={taskHref(ask.task)} data-testid="ask-task">{ask.task}</a>{/if}
      </p>
      <p class="max-w-[75ch] font-medium" data-testid="ask-question">{ask.question}</p>
      <p class="text-text-muted">
        asked by <span data-testid="ask-asker">{ask.asker}</span>, <span data-testid="ask-mode">{modeLabel(ask.mode)}</span>,
        <time datetime={ask.created_at} title={absoluteTime(ask.created_at)}>{relativeTime(ask.created_at, now)}</time>
      </p>
      {#if ask.details !== ''}
        <!-- renderMarkdown escapes the source first and writes only its own markup (lib/markdown.ts). -->
        <div class="description wrap-anywhere" data-testid="ask-details">{@html renderMarkdown(ask.details)}</div>
      {/if}
      <p class="text-text-muted" data-testid="ask-line">{stateLine(ask)}</p>
      {#if ask.state === 'answered'}
        <p class="max-w-[75ch]" data-testid="ask-answer"><span class="text-text-muted">Answer:</span> {ask.answer ?? ''}</p>
      {/if}
      {#if canAct(ask)}
        <p class="mt-2 flex flex-wrap gap-2">
          <button type="button" class={small} aria-label="Answer {ask.ref}" onclick={() => onanswer(ask)}>Answer</button>
          <button type="button" class={small} aria-label="Withdraw {ask.ref}" onclick={() => onwithdraw(ask.ref)}>Withdraw</button>
        </p>
      {/if}
    </li>
  {/each}
</ul>
