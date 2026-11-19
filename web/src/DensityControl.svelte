<script lang="ts">
  import SegmentedRadio from './SegmentedRadio.svelte';
  import { DENSITIES, type Density } from './lib/density';
  import { density } from './densityController';

  // The density of the list pages: Compact (the default) or Sparse. Persisted by the controller.
  let { name = 'density' }: { name?: string } = $props();
  let setting = $state<Density>(density.density());
  const labels: Record<Density, string> = { compact: 'Compact', sparse: 'Sparse' };
  const options = DENSITIES.map((value) => ({ value, label: labels[value] }));

  function choose(next: Density) {
    setting = next;
    density.set(next);
  }
</script>

<SegmentedRadio legend="Density" {name} {options} value={setting} onchoose={choose} testid="density-control" />
