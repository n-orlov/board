<script lang="ts">
  import Dialog from './Dialog.svelte';
  import Icon from './Icon.svelte';
  import type { NavEntry } from './lib/routes';

  // The phone tab bar's "More" entry: the navigation entries that have no tab of their own, in the one dialog/sheet component.
  let { entries, onclose }: { entries: NavEntry[]; onclose: () => void } = $props();
</script>

<Dialog label="More" {onclose}>
  <ul class="mb-2 flex flex-col" data-testid="more-menu">
    {#each entries as entry (entry.id)}
      <li>
        <a
          href={entry.href}
          class="flex min-h-11 items-center gap-3 rounded-md px-3 text-text hover:bg-surface aria-[current=page]:font-semibold aria-[current=page]:text-accent"
          aria-current={entry.current ? 'page' : undefined}
          onclick={onclose}
        >
          <Icon d={entry.icon} />
          {entry.label}
        </a>
      </li>
    {/each}
  </ul>
  <button type="button" class="min-h-11 w-full rounded-md px-4 text-text-muted hover:text-text" onclick={onclose}>Close</button>
</Dialog>
