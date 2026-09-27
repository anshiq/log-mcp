import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  test: {
    exclude: ['e2e/**', 'node_modules/**', 'dist/**', 'dist-web/**'],
    environmentMatchGlobs: [['**/*.svelte.test.ts', 'jsdom'], ['**/*.test.ts', 'node']],
    environment: 'node'
  }
});
