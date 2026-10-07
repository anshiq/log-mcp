import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

let version = '0.4.2';
try {
  const p = join(process.cwd(), 'package.json');
  if (existsSync(p)) {
    version = JSON.parse(readFileSync(p, 'utf-8')).version ?? version;
  }
} catch {
}

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
    target: 'es2022',
    chunkSizeWarningLimit: 2000,
    rollupOptions: {
      output: {
        manualChunks: {
          monaco: ['monaco-editor'],
          xterm: ['@xterm/xterm']
        }
      }
    }
  },
  worker: {
    format: 'es'
  },
  define: {
    __APP_TARGET__: JSON.stringify(target),
    __APP_VERSION__: JSON.stringify(version)
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
