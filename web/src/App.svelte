<script lang="ts">
  import { currentBrowser, loadSession, type SessionState } from './lib/session';

  let state: SessionState | undefined = $state();

  $effect(() => {
    void loadSession(currentBrowser()).then((s) => (state = s));
  });
</script>

<main>
  <h1>board</h1>
  {#if state === undefined || state.kind === 'redirecting'}
    <p>Signing in…</p>
  {:else if state.kind === 'error'}
    <p role="alert">{state.message}</p>
  {:else}
    <p>Signed in as <span data-testid="email">{state.session.email}</span></p>
    <h2>Initiatives</h2>
    <p>No initiatives yet.</p>
  {/if}
</main>
