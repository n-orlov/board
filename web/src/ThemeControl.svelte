<script lang="ts">
  import { THEME_SETTINGS, type ThemeSetting } from './lib/theme';
  import { theme } from './themeController';

  // The theme setting: System follows the device, Light and Dark override it. Persisted by the controller.
  let setting = $state<ThemeSetting>(theme.setting());
  const labels: Record<ThemeSetting, string> = { system: 'System', light: 'Light', dark: 'Dark' };

  function choose(next: ThemeSetting) {
    setting = next;
    theme.set(next);
  }
</script>

<fieldset class="mb-4 min-w-0">
  <legend class="mb-2 text-sm text-text-muted">Theme</legend>
  <div class="flex gap-2" data-testid="theme-control">
    {#each THEME_SETTINGS as option (option)}
      <label
        class="flex min-h-11 flex-1 cursor-pointer items-center justify-center rounded-md border border-border px-3 text-sm text-text has-[:checked]:border-accent has-[:checked]:bg-surface has-[:checked]:font-semibold has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-accent"
      >
        <input
          type="radio"
          name="theme"
          class="sr-only"
          value={option}
          checked={setting === option}
          onchange={() => choose(option)}
        />
        {labels[option]}
      </label>
    {/each}
  </div>
</fieldset>
