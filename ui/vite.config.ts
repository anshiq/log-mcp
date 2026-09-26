import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { readFileSync } from 'node:fs';

const pkg = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf-8'));

// VITE_TARGET=web builds the standalone web UI (served by the daemon on
// the TCP listener or any static host, bearer-token login). The default
// build targets the Wails desktop shell (embedded via go:embed).
const target = process.env.VITE_TARGET ?? 'wails';

export default defineConfig({
  plugins: [svelte()],
  base: target === 'web' ? '/' : './',
  build: {
    outDir: target === 'web' ? 'dist-web' : 'dist',
    sourcemap: true,
    target: 'es2022'
  },
  define: {
    __APP_TARGET__: JSON.stringify(target),
    __APP_VERSION__: JSON.stringify(pkg.version)
  },
  server: {
    proxy: {
      // Local dev against a real daemon: the Go GUI proxy forwards /api
      // to agentd.sock in production; vite emulates it here.
      '/api': {
        target: 'http://127.0.0.1:7350',
        changeOrigin: true
      }
    }
  }
});
