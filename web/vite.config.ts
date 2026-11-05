import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// The build lands in internal/webui/dist, which the Go binary embeds with go:embed.
// Asset URLs are relative ('./'), so the page works under any path prefix; the
// build script clears the directory itself so the tracked .gitkeep survives.
export default defineConfig({
  base: './',
  plugins: [svelte()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: false,
  },
});
