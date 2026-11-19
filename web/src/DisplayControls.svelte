<script lang="ts">
  // UX prototype: display settings in the sidebar foot. Density (compact / sparse) and theme
  // (auto / light / dark), both remembered in this browser.
  import { prefs, setDensity, type Density } from './lib/proto/prefs.svelte';
  import type { ThemeSetting } from './lib/theme';
  import { theme } from './themeController';

  let themeSetting = $state<ThemeSetting>(theme.setting());
  const DENSITY: { value: Density; label: string }[] = [
    { value: 'compact', label: 'Compact' },
    { value: 'sparse', label: 'Sparse' },
  ];
  const THEMES: { value: ThemeSetting; label: string }[] = [
    { value: 'system', label: 'Auto' },
    { value: 'light', label: 'Light' },
    { value: 'dark', label: 'Dark' },
  ];
  const group = 'flex overflow-hidden rounded-md border border-border text-xs';
  const item = 'flex-1 px-2 py-1 text-text-muted hover:text-text aria-pressed:bg-accent aria-pressed:font-semibold aria-pressed:text-surface-raised';

  function chooseTheme(next: ThemeSetting) {
    themeSetting = next;
    theme.set(next);
  }
</script>

<div class="flex flex-col gap-1.5 px-1" data-testid="display-controls">
  <div role="group" aria-label="Density" class={group} data-testid="density-switch">
    {#each DENSITY as o (o.value)}
      <button type="button" class={item} aria-pressed={prefs.density === o.value} onclick={() => setDensity(o.value)}>{o.label}</button>
    {/each}
  </div>
  <div role="group" aria-label="Theme" class={group} data-testid="theme-switch">
    {#each THEMES as o (o.value)}
      <button type="button" class={item} aria-pressed={themeSetting === o.value} onclick={() => chooseTheme(o.value)}>{o.label}</button>
    {/each}
  </div>
</div>
