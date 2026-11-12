<script lang="ts">
  import Icon from './Icon.svelte';
  import type { NavEntry } from './lib/routes';

  // One navigation surface of the shell, drawn from the route table's entries. The shell shows
  // exactly one of the three at a time (Tailwind breakpoints): the phone tab bar, the tablet icon
  // rail, the desktop sidebar. A hidden one is display:none, so it is out of the accessibility tree.
  let { entries, variant, testid }: { entries: NavEntry[]; variant: 'tabbar' | 'rail' | 'sidebar'; testid: string } = $props();

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
    {#each entries as entry (entry.id)}
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
  </ul>
</nav>
