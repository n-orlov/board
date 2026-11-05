<script lang="ts">
  import { KeysApi, KeysStore, type KeysState } from './lib/keys';
  import { currentBrowser } from './lib/session';

  let { csrf }: { csrf: string } = $props();

  let view: KeysState = $state({ keys: [], loading: true, created: undefined, error: undefined });
  let name = $state('');
  let expiresIn = $state('');

  const store = new KeysStore(new KeysApi(currentBrowser(), () => csrf), (s) => (view = s));

  $effect(() => {
    void store.load();
  });

  async function create(event: Event) {
    event.preventDefault();
    if (await store.create(name, expiresIn)) {
      name = '';
      expiresIn = '';
    }
  }

  const when = (iso: string | undefined) => (iso === undefined ? '' : iso);
</script>

<section aria-labelledby="keys-heading">
  <h2 id="keys-heading">API keys</h2>

  {#if view.error !== undefined}
    <p role="alert">{view.error}</p>
  {/if}

  {#if view.created !== undefined}
    <div data-testid="new-key" role="status">
      <p>Key <strong>{view.created.name}</strong> created. Copy it now: it is shown only once.</p>
      <code data-testid="new-key-value">{view.created.key}</code>
      <button type="button" onclick={() => store.dismiss()}>Done</button>
    </div>
  {/if}

  <form onsubmit={create}>
    <label>Name <input name="name" bind:value={name} required maxlength="64" pattern="[A-Za-z0-9][A-Za-z0-9._\-]*" /></label>
    <label>Expires in <input name="expires_in" bind:value={expiresIn} placeholder="90d (optional)" /></label>
    <button type="submit">Create key</button>
  </form>

  {#if view.loading}
    <p>Loading…</p>
  {:else if view.keys.length === 0}
    <p>No API keys yet.</p>
  {:else}
    <table>
      <thead>
        <tr><th>Name</th><th>Prefix</th><th>Status</th><th>Created</th><th>Last used</th><th>Expires</th><th></th></tr>
      </thead>
      <tbody>
        {#each view.keys as key (key.id)}
          <tr data-testid="key-row" data-name={key.name}>
            <td>{key.name}</td>
            <td><code>{key.display_prefix}</code></td>
            <td data-testid="key-status">{key.status}</td>
            <td>{key.created_at}</td>
            <td>{when(key.last_used_at)}</td>
            <td>{when(key.expires_at)}</td>
            <td>
              {#if key.status !== 'revoked'}
                <button type="button" onclick={() => store.revoke(key.name)}>Revoke</button>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</section>
