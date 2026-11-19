<script lang="ts">
  import DensityControl from './DensityControl.svelte';
  import ThemeControl from './ThemeControl.svelte';

  // The display switches together: density and theme. The sidebar foot and the account menu each show one copy.
  let { scope }: { scope: string } = $props();
</script>

<div data-testid="display-controls-{scope}">
  <DensityControl name="density-{scope}" />
  <ThemeControl name="theme-{scope}" />
</div>
