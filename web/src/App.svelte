<script lang="ts">
  import ApiKeys from './ApiKeys.svelte';
  import Initiatives from './Initiatives.svelte';
  import { KEYS_HASH, viewFromHash, type View } from './lib/route';
  import { currentBrowser, loadSession, type SessionState } from './lib/session';

  let auth: SessionState | undefined = $state();
  let view: View = $state(viewFromHash(window.location.hash));

  $effect(() => {
    void loadSession(currentBrowser()).then((s) => (auth = s));
  });
</script>

<svelte:window onhashchange={() => (view = viewFromHash(window.location.hash))} />

<main>
  <h1>board</h1>
  {#if auth === undefined || auth.kind === 'redirecting'}
    <p>Signing in…</p>
  {:else if auth.kind === 'error'}
    <p role="alert">{auth.message}</p>
  {:else}
    <p>Signed in as <span data-testid="email">{auth.session.email}</span></p>
    <nav>
      <a href="#/">Initiatives</a>
      <a href={KEYS_HASH}>Settings → API keys</a>
    </nav>
    {#if view === 'keys'}
      <ApiKeys csrf={auth.session.csrf_token} />
    {:else}
      <Initiatives csrf={auth.session.csrf_token} />
    {/if}
  {/if}
</main>
