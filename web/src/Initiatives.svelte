<script lang="ts">
  import { InitiativesApi, InitiativesStore, type InitiativesState } from './lib/initiatives';
  import { currentBrowser } from './lib/session';

  let { csrf }: { csrf: string } = $props();

  let view: InitiativesState = $state({ initiatives: [], loading: true, error: undefined });
  let slug = $state('');
  let title = $state('');
  let description = $state('');
  let tags = $state('');

  const store = new InitiativesStore(new InitiativesApi(currentBrowser(), () => csrf), (s) => (view = s));

  $effect(() => {
    void store.load();
  });

  async function create(event: Event) {
    event.preventDefault();
    if (await store.create({ slug, title, description, tags })) {
      slug = '';
      title = '';
      description = '';
      tags = '';
    }
  }
</script>

<section aria-labelledby="initiatives-heading">
  <h2 id="initiatives-heading">Initiatives</h2>

  {#if view.error !== undefined}
    <p role="alert">{view.error}</p>
  {/if}

  <form onsubmit={create}>
    <label>Slug <input name="slug" bind:value={slug} /></label>
    <label>Title <input name="title" bind:value={title} /></label>
    <label>Description <textarea name="description" bind:value={description}></textarea></label>
    <label>Tags <input name="tags" bind:value={tags} placeholder="comma separated (optional)" /></label>
    <button type="submit">Create initiative</button>
  </form>

  {#if view.loading}
    <p>Loading…</p>
  {:else if view.initiatives.length === 0}
    <p>No initiatives yet.</p>
  {:else}
    <table>
      <thead>
        <tr><th>Slug</th><th>Title</th><th>Status</th><th>Tags</th><th>Created</th></tr>
      </thead>
      <tbody>
        {#each view.initiatives as initiative (initiative.id)}
          <tr data-testid="initiative-row" data-slug={initiative.slug}>
            <td><code>{initiative.slug}</code></td>
            <td>{initiative.title}</td>
            <td data-testid="initiative-status">{initiative.status}</td>
            <td data-testid="initiative-tags">{initiative.tags.join(', ')}</td>
            <td>{initiative.created_at}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</section>
