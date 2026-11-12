<script lang="ts">
  import Icon from './Icon.svelte';
  import MoreMenu from './MoreMenu.svelte';
  import { phoneSplit, type NavEntry } from './lib/routes';

  // One navigation surface of the shell, drawn from the route table's entries. The shell shows
  // exactly one of the three at a time (Tailwind breakpoints): the phone tab bar, the tablet icon
  // rail, the desktop sidebar. A hidden one is display:none, so it is out of the accessibility tree.
  let { entries, variant, testid }: { entries: NavEntry[]; variant: 'tabbar' | 'rail' | 'sidebar'; testid: string } = $props();

  // The tab bar keeps the entries flagged `phone` and lists the rest under a "More" entry; the other two surfaces list them all.
  const split = $derived(variant === 'tabbar' ? phoneSplit(entries) : { tabs: entries, more: [] as NavEntry[] });
  let moreOpen = $state(false);
  // More is current while the view on screen is one of the entries it lists.
  const moreCurrent = $derived(split.more.some((entry) => entry.current));

  const MORE = 'M5 12h.01M12 12h.01M19 12h.01';

  const link: Record<typeof variant, string> = {
    tabbar: 'flex min-h-14 flex-1 flex-col items-center justify-center gap-1 px-0.5 text-xs',
    rail: 'flex size-11 items-center justify-center rounded-md',
    sidebar: 'flex min-h-11 items-center gap-3 rounded-md px-3 text-sm',
  };
  const list: Record<typeof variant, string> = {
    tabbar: 'flex',
    rail: 'flex flex-col items-center gap-1',
    sidebar: 'flex flex-col gap-1',
  };
</script>

<nav aria-label="Main" data-testid={testid}>
  <ul class={list[variant]}>
    {#each split.tabs as entry (entry.id)}
      <li class={variant === 'tabbar' ? 'flex flex-1' : ''}>
        <a
          href={entry.href}
          class="{link[variant]} text-text-muted hover:text-text aria-[current=page]:font-semibold aria-[current=page]:text-accent {variant === 'tabbar' ? '' : 'aria-[current=page]:bg-surface'}"
          aria-current={entry.current ? 'page' : undefined}
          title={variant === 'rail' ? entry.label : undefined}
        >
          <span class="relative inline-flex">
            <Icon d={entry.icon} />
            {#if entry.badge !== ''}
              <span
                class="absolute -right-3 -top-2 min-w-5 rounded-full bg-danger px-1 text-center text-[11px] font-bold leading-5 text-surface-raised"
                data-testid="attention-badge"
                aria-hidden="true">{entry.badge}</span
              >
            {/if}
          </span>
          <!-- The badge's words ride in the link's content, not in an aria-label, so the name keeps the visible text (axe label-content-name-mismatch). -->
          <span class={variant === 'rail' ? 'sr-only' : ''}
            >{entry.label}{#if entry.badgeLabel !== ''}<span class="sr-only">{entry.badgeLabel}</span>{/if}</span
          >
        </a>
      </li>
    {/each}
    {#if split.more.length > 0}
      <li class="flex flex-1">
        <button
          type="button"
          class="{link[variant]} text-text-muted hover:text-text aria-[current=true]:font-semibold aria-[current=true]:text-accent"
          aria-haspopup="dialog"
          aria-current={moreCurrent ? 'true' : undefined}
          data-testid="more-button"
          onclick={() => (moreOpen = true)}
        >
          <Icon d={MORE} />
          <span>More</span>
        </button>
      </li>
    {/if}
  </ul>
</nav>

{#if moreOpen}
  <MoreMenu entries={split.more} onclose={() => (moreOpen = false)} />
{/if}
