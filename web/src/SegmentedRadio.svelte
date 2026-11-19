<script lang="ts" generics="T extends string">
  // One segmented choice drawn as a radio group (arrow keys move it, a label is the hit area): the theme and the density
  // switches share it. `name` must differ between two copies on one page (the sidebar foot and the account menu).
  let {
    legend,
    name,
    options,
    value,
    onchoose,
    testid,
  }: {
    legend: string;
    name: string;
    options: readonly { value: T; label: string }[];
    value: T;
    onchoose: (next: T) => void;
    testid: string;
  } = $props();
</script>

<fieldset class="mb-4 min-w-0">
  <legend class="mb-2 text-sm text-text-muted">{legend}</legend>
  <div class="flex gap-2" data-testid={testid}>
    {#each options as option (option.value)}
      <label
        class="relative flex min-h-11 flex-1 cursor-pointer items-center justify-center rounded-md border border-border px-3 text-sm text-text has-[:checked]:border-accent has-[:checked]:bg-surface has-[:checked]:font-semibold has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-accent"
      >
        <input
          type="radio"
          {name}
          class="absolute -inset-px h-[calc(100%+2px)] w-[calc(100%+2px)] cursor-pointer opacity-0 text-base"
          value={option.value}
          checked={value === option.value}
          onchange={() => onchoose(option.value)}
        />
        {option.label}
      </label>
    {/each}
  </div>
</fieldset>
