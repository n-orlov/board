<script lang="ts">
  import { initiativeHref } from './lib/attentionActions';
  import type { Ask } from './lib/attentionActions';
  import { renderMarkdown } from './lib/markdown';

  // The Answer dialog's body (PRD R9): the question, its details as safe Markdown, one button per suggested option that
  // fills the answer, and a free-text field. A refusal comes in as `error`; the dialog stays open and the text stays.
  let {
    ask,
    error,
    onsubmit,
    oncancel,
  }: { ask: Ask | undefined; error: string | undefined; onsubmit: (answer: string) => Promise<void>; oncancel: () => void } = $props();

  let answer = $state('');
  let busy = $state(false);

  const input = 'mt-1 block min-h-11 w-full rounded-md border border-border bg-surface px-3 py-2 text-base text-text';
  const button = 'min-h-11 rounded-md border px-4 text-sm font-medium disabled:opacity-60';

  async function submit(event: Event) {
    event.preventDefault();
    busy = true;
    try {
      await onsubmit(answer);
    } finally {
      busy = false;
    }
  }
</script>

<form onsubmit={submit} class="flex flex-col gap-4" data-testid="answer-form">
  <h2 class="text-lg font-bold wrap-anywhere" data-testid="answer-question">{ask === undefined ? 'Answer the ask' : `${ask.ref}: ${ask.question}`}</h2>

  {#if error !== undefined}
    <p role="alert" class="rounded-md border border-danger px-3 py-2 text-sm text-danger" data-testid="dialog-error">{error}</p>
  {/if}

  {#if ask !== undefined}
    {#if ask.details !== ''}
      <!-- renderMarkdown escapes the source first and writes only its own markup (lib/markdown.ts). -->
      <div class="description wrap-anywhere text-sm" data-testid="answer-details">{@html renderMarkdown(ask.details)}</div>
    {/if}
    {#if ask.options.length > 0}
      <div role="group" aria-label="Suggested answers" class="flex flex-wrap gap-2" data-testid="answer-options">
        {#each ask.options as option (option)}
          <button
            type="button"
            class="{button} max-w-full border-border text-text wrap-anywhere"
            data-testid="answer-option"
            onclick={() => (answer = option)}>{option}</button
          >
        {/each}
      </div>
    {/if}
  {/if}

  <label class="block text-sm font-medium">Answer <textarea name="answer" rows="4" class={input} bind:value={answer}></textarea></label>

  <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
    <button type="button" class="{button} border-border text-text" onclick={oncancel}>Cancel</button>
    <button type="submit" class="{button} border-accent text-accent" disabled={busy || ask === undefined}>Send answer</button>
  </div>
  {#if ask?.initiative !== undefined}
    <a class="inline-flex min-h-11 items-center text-sm text-accent hover:underline" href={initiativeHref(ask.initiative)} data-testid="answer-initiative">Initiative {ask.initiative}</a>
  {/if}
</form>
