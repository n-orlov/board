<script lang="ts">
  import SegmentedRadio from './SegmentedRadio.svelte';
  import { THEME_SETTINGS, type ThemeSetting } from './lib/theme';
  import { theme } from './themeController';

  // The theme setting: Auto follows the device, Light and Dark override it. Persisted by the controller.
  let { name = 'theme' }: { name?: string } = $props();
  let setting = $state<ThemeSetting>(theme.setting());
  const labels: Record<ThemeSetting, string> = { system: 'Auto', light: 'Light', dark: 'Dark' };
  const options = THEME_SETTINGS.map((value) => ({ value, label: labels[value] }));

  function choose(next: ThemeSetting) {
    setting = next;
    theme.set(next);
  }
</script>

<SegmentedRadio legend="Theme" {name} {options} value={setting} onchoose={choose} testid="theme-control" />
