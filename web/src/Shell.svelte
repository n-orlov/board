<script lang="ts">
  import type { Snippet } from 'svelte';
  import AccountMenu from './AccountMenu.svelte';
  import Icon from './Icon.svelte';
  import LiveIndicator from './LiveIndicator.svelte';
  import Nav from './Nav.svelte';
  import type { NavEntry } from './lib/routes';

  // The responsive frame around every signed-in view. Below 768 px: a top bar (product name, view
  // title, account button) and a fixed bottom tab bar. 768-1023 px: a collapsed icon rail. 1024 px
  // and up: a labelled sidebar whose foot shows the email, Sign out and an account button.
  let {
    title,
    entries,
    email,
    actor,
    onsignout,
    children,
  }: { title: string; entries: NavEntry[]; email: string; actor: string; onsignout: () => void; children: Snippet } = $props();

  let menuOpen = $state(false);

  const USER = 'M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0z';
  const button = 'flex size-11 shrink-0 items-center justify-center rounded-md text-text-muted hover:text-text';

  // The skip link must not touch the fragment (it is the route): it moves focus instead.
  function skip(event: Event) {
    event.preventDefault();
    document.getElementById('main')?.focus();
  }

  function signOut() {
    menuOpen = false;
    onsignout();
  }
</script>

<a
  href="#main"
  onclick={skip}
  class="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-[60] focus:rounded-md focus:bg-surface-raised focus:px-4 focus:py-3 focus:text-text focus:shadow-lg"
  >Skip to content</a
>

<div class="min-h-dvh md:flex">
  <header
    class="sticky top-0 z-30 flex h-14 items-center gap-3 border-b border-border bg-surface-raised px-4 md:hidden"
    data-testid="topbar"
  >
    <span class="shrink-0 whitespace-nowrap font-bold">Village Hall</span>
    <span class="min-w-0 flex-1 truncate text-text-muted" data-testid="topbar-title">{title}</span>
    <LiveIndicator />
    <button type="button" class={button} aria-label="Account menu" aria-haspopup="dialog" onclick={() => (menuOpen = true)}>
      <Icon d={USER} />
    </button>
  </header>

  <aside
    class="sticky top-0 hidden h-dvh w-16 shrink-0 flex-col items-center gap-4 border-r border-border bg-surface-raised py-4 md:flex lg:hidden"
  >
    <span class="font-bold" aria-hidden="true">VH</span>
    <div class="flex-1">
      <Nav {entries} variant="rail" testid="nav-rail" />
    </div>
    <LiveIndicator compact />
    <button type="button" class={button} aria-label="Account menu" aria-haspopup="dialog" onclick={() => (menuOpen = true)}>
      <Icon d={USER} />
    </button>
  </aside>

  <aside
    class="sticky top-0 hidden h-dvh w-60 shrink-0 flex-col gap-4 border-r border-border bg-surface-raised p-3 lg:flex"
  >
    <span class="px-3 py-2 text-lg font-bold">Village Hall</span>
    <div class="flex-1">
      <Nav {entries} variant="sidebar" testid="nav-sidebar" />
    </div>
    <div class="flex flex-col gap-2 border-t border-border pt-3" data-testid="sidebar-foot">
      <div class="px-3"><LiveIndicator /></div>
      <div class="flex items-center gap-2">
        <span class="min-w-0 flex-1 truncate px-3 text-sm text-text-muted" data-testid="email">{email}</span>
        <button type="button" class={button} aria-label="Account menu" aria-haspopup="dialog" onclick={() => (menuOpen = true)}>
          <Icon d={USER} />
        </button>
      </div>
      <button
        type="button"
        class="min-h-11 rounded-md border border-border px-3 text-sm text-text hover:bg-surface"
        onclick={onsignout}>Sign out</button
      >
    </div>
  </aside>

  <main
    id="main"
    tabindex="-1"
    class="min-w-0 flex-1 p-4 pb-[calc(3.5rem+env(safe-area-inset-bottom)+1rem)] outline-none md:p-6"
  >
    {@render children()}
  </main>

  <div
    class="fixed inset-x-0 bottom-0 z-30 border-t border-border bg-surface-raised pb-[env(safe-area-inset-bottom)] md:hidden"
    data-testid="tabbar"
  >
    <Nav {entries} variant="tabbar" testid="nav-tabbar" />
  </div>
</div>

{#if menuOpen}
  <AccountMenu {email} {actor} onclose={() => (menuOpen = false)} onsignout={signOut} />
{/if}
