<script lang="ts">
  // A stroked 24x24 icon from SVG path data; decorative, the label next to it names the control.
  let { d, class: className = 'size-6' }: { d: string; class?: string } = $props();
</script>

<svg class={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
  <path {d} />
</svg>
