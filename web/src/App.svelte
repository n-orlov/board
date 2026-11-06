<script lang="ts">
  import type { Component } from 'svelte';
  import ApiKeys from './ApiKeys.svelte';
  import Initiatives from './Initiatives.svelte';
  import Shell from './Shell.svelte';
  import { loginUrl, currentBrowser, loadSession, logout, type SessionState } from './lib/session';
  import { ROUTES, navEntries, resolveRoute, type RouteId } from './lib/routes';

  // The component of each view; the table in lib/routes.ts decides which one is on screen.
  const VIEWS: Record<RouteId, Component<{ csrf: string }>> = { initiatives: Initiatives, keys: ApiKeys };

  let auth: SessionState | undefined = $state();
  let route = $state(resolveRoute(window.location.hash, ROUTES));
  let signedOut = $state(false);
  let signOutFailed = $state(false);

  const browser = currentBrowser();

  $effect(() => {
    void loadSession(browser).then((s) => (auth = s));
  });

  async function signOut(csrf: string) {
    signOutFailed = false;
    if (await logout(browser, csrf)) signedOut = true;
    else signOutFailed = true;
  }

  const View = $derived(VIEWS[route.id]);
</script>

<svelte:window onhashchange={() => (route = resolveRoute(window.location.hash, ROUTES))} />

{#if auth === undefined || auth.kind === 'redirecting'}
  <main class="p-4">
    <h1 class="text-lg font-bold">board</h1>
    <p>Signing in…</p>
  </main>
{:else if auth.kind === 'error'}
  <main class="p-4">
    <h1 class="text-lg font-bold">board</h1>
    <p role="alert">{auth.message}</p>
  </main>
{:else if signedOut}
  <main class="p-4">
    <h1 class="text-lg font-bold">board</h1>
    <p>You are signed out.</p>
    <a class="text-accent underline" href={loginUrl(browser.pathname)}>Sign in</a>
  </main>
{:else}
  {@const csrf = auth.session.csrf_token}
  <Shell title={route.title} entries={navEntries(route, ROUTES)} email={auth.session.email} onsignout={() => signOut(csrf)}>
    {#if signOutFailed}
      <p role="alert">Sign out failed. Try again.</p>
    {/if}
    <View {csrf} />
  </Shell>
{/if}
