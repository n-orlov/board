<script lang="ts">
  import type { Component } from 'svelte';
  import Actors from './Actors.svelte';
  import Archive from './Archive.svelte';
  import Now from './Now.svelte';
  import ApiKeys from './ApiKeys.svelte';
  import Notes from './Notes.svelte';
  import InitiativePage from './InitiativePage.svelte';
  import Initiatives from './Initiatives.svelte';
  import SessionCard from './SessionCard.svelte';
  import Shell from './Shell.svelte';
  import TaskPage from './TaskPage.svelte';
  import Tasks from './Tasks.svelte';
  import Timeline from './Timeline.svelte';
  import { live } from './lib/live';
  import { loginUrl, currentBrowser, SessionController, type SessionState } from './lib/session';
  import { ROUTES, navEntries, redirectedHash, resolveRoute, routeParam, type RouteId } from './lib/routes';
  import { replaceHash } from './lib/historyEntry';
  import { applyDensity } from './lib/proto/prefs.svelte';

  // The old `#/attention` links land on Now (the view's new name), with their query.
  function redirectOld() {
    const next = redirectedHash(window.location.hash);
    if (next !== window.location.hash) replaceHash(next);
  }
  redirectOld();
  applyDensity();

  // The component of each view; the table in lib/routes.ts decides which one is on screen.
  const VIEWS: Record<RouteId, Component<{ csrf: string; param: string }>> = {
    now: Now,
    initiatives: Initiatives,
    initiative: InitiativePage,
    tasks: Tasks,
    task: TaskPage,
    timeline: Timeline,
    actors: Actors,
    archive: Archive,
    notes: Notes,
    keys: ApiKeys,
  };

  const browser = currentBrowser();
  let auth: SessionState = $state({ kind: 'loading' });
  let route = $state(resolveRoute(window.location.hash, ROUTES));
  let param = $state(routeParam(window.location.hash, ROUTES));
  let signOutFailed = $state(false);

  // The Attention badge in the navigation: the total of the stream's latest `attention` message.
  let attentionTotal = $state(0);

  const session = new SessionController(browser, (s) => (auth = s), window.location.hash);

  $effect(() => {
    void session.start();
  });

  $effect(() => live().onAttention((counts) => (attentionTotal = counts.total)));

  async function signOut() {
    signOutFailed = (await session.signOut()) === 'failed';
  }

  function hashChanged() {
    redirectOld();
    route = resolveRoute(window.location.hash, ROUTES);
    param = routeParam(window.location.hash, ROUTES);
    void session.hashChanged(window.location.hash);
  }

  const View = $derived(VIEWS[route.id]);
</script>

<svelte:window onhashchange={hashChanged} />

{#if auth.kind === 'loading' || auth.kind === 'redirecting'}
  <SessionCard title={auth.kind === 'loading' ? 'Signing in' : 'Redirecting to sign in'}>
    <div
      class="mx-auto mb-3 size-8 animate-spin rounded-full border-4 border-border border-t-accent"
      role="status"
      aria-label={auth.kind === 'loading' ? 'Signing in' : 'Redirecting to sign in'}
      data-testid="spinner"
    ></div>
    <p class="text-text-muted">{auth.kind === 'loading' ? 'Signing in…' : 'Redirecting to sign in…'}</p>
  </SessionCard>
{:else if auth.kind === 'error'}
  <SessionCard title="Session error">
    <p role="alert" class="mb-4" data-testid="session-error">{auth.message}</p>
    <button
      type="button"
      class="min-h-11 rounded-md border border-accent px-4 font-semibold text-accent hover:bg-surface"
      onclick={() => void session.retry()}>Retry</button
    >
  </SessionCard>
{:else if auth.kind === 'signed-out'}
  <SessionCard title="Signed out">
    <p class="mb-4" data-testid="signed-out">You are signed out.</p>
    <a class="inline-flex min-h-11 items-center rounded-md border border-accent px-4 font-semibold text-accent hover:bg-surface" href={loginUrl(browser.pathname)}
      >Sign in</a
    >
  </SessionCard>
{:else}
  <Shell title={route.title} entries={navEntries(route, ROUTES, attentionTotal)} email={auth.session.email} onsignout={signOut}>
    {#if signOutFailed}
      <p role="alert">Sign out failed. Try again.</p>
    {/if}
    {#key param}
      <View csrf={auth.session.csrf_token} {param} />
    {/key}
  </Shell>
{/if}
