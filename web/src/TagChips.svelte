<script lang="ts">
  // A task's tags as coloured chips (lib/taskTags.ts, SPEC 11): the one tag colour, the source in the title and in words for a
  // screen reader, a dotted border for a task's own tag and a dashed one for a derived one. With an `editor` a task's own tag
  // has a remove button and an Add tag button opens a field that suggests tags in use; an initiative's or derived tag never has one.
  import { chipSourceText, tagStyle, type TagMark } from './lib/chipFilter';
  import { chipTitle, ownTags, removable, removeLabel, suggestTags, tagChips, type TagEditor } from './lib/taskTags';

  let { marks, ref, editor }: { marks: readonly TagMark[]; ref: string; editor?: TagEditor } = $props();

  let adding = $state(false);
  let text = $state('');
  let error = $state('');
  let busy = $state(false);
  const chips = $derived(tagChips(marks));
  const suggestions = $derived(editor === undefined ? [] : suggestTags(editor.inUse, ownTags(marks), text));

  async function run(work: Promise<string>) {
    busy = true;
    error = await work;
    busy = false;
    return error === '';
  }

  async function add(typed: string) {
    if (editor === undefined || busy) return;
    if (await run(editor.onadd(typed))) text = '';
  }

  const remove = (tag: string) => (editor === undefined || busy ? undefined : run(editor.onremove(tag)));
  const small = 'min-h-11 rounded-md border border-border bg-surface-raised px-3 text-sm font-medium text-text';
</script>

<div class="flex min-w-0 flex-wrap items-center gap-1" data-testid="task-tags" data-ref={ref}>
  {#each chips as chip (chip.tag)}
    <span class="vh-tag" style={tagStyle(chip.tag)} data-source={chip.sources.includes('task') ? 'task' : chip.sources[0]} data-tag={chip.tag} data-testid="task-tag" title={chipTitle(chip)}>
      {chip.tag}<span class="sr-only"> ({chipSourceText(chip)})</span>
      {#if editor !== undefined && removable(chip)}
        <button
          type="button"
          class="inline-flex min-h-11 min-w-11 items-center justify-center rounded-full"
          aria-label={removeLabel(chip.tag, ref)}
          data-testid="tag-remove"
          disabled={busy}
          onclick={() => remove(chip.tag)}><span aria-hidden="true">×</span></button
        >
      {/if}
    </span>
  {/each}
  {#if editor !== undefined}
    {#if adding}
      <form
        class="flex w-full flex-col gap-1"
        data-testid="tag-form"
        onsubmit={(e) => {
          e.preventDefault();
          void add(text);
        }}
      >
        <div class="flex flex-wrap items-center gap-1">
          <input
            type="text"
            class="min-h-11 w-40 max-w-full rounded-md border border-border bg-surface-raised px-3 text-base text-text"
            aria-label="New tag for {ref}"
            autocomplete="off"
            autocapitalize="none"
            spellcheck="false"
            data-testid="tag-input"
            bind:value={text}
          />
          <button type="submit" class={small} data-testid="tag-submit" disabled={busy}>Add</button>
          <button type="button" class={small} data-testid="tag-cancel" onclick={() => { adding = false; text = ''; error = ''; }}>Done</button>
        </div>
        {#if suggestions.length > 0}
          <ul class="flex flex-wrap gap-1" aria-label="Suggested tags" data-testid="tag-suggestions">
            {#each suggestions as tag (tag)}
              <li>
                <button type="button" class="vh-tag min-h-11 cursor-pointer" style={tagStyle(tag)} data-testid="tag-suggestion" data-tag={tag} disabled={busy} onclick={() => add(tag)}>{tag}</button>
              </li>
            {/each}
          </ul>
        {/if}
      </form>
    {:else}
      <button type="button" class="{small} px-2" data-testid="tag-add-open" aria-label="Add tag to {ref}" onclick={() => (adding = true)}>+ tag</button>
    {/if}
    {#if error !== ''}
      <p role="alert" class="w-full text-sm text-danger" data-testid="tag-error">{error}</p>
    {/if}
  {/if}
</div>
