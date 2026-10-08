import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

let version = '0.5.0';
try {
  const p = join(process.cwd(), 'package.json');
  if (existsSync(p)) {
    version = JSON.parse(readFileSync(p, 'utf-8')).version ?? version;
  }
} catch {
}

// Builds the standalone web UI (served by the daemon on the TCP listener
// or any static host, bearer-token login).
export default defineConfig({
  plugins: [svelte()],
  base: '/',
  build: {
    outDir: 'dist-web',
    sourcemap: false,
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
    __APP_VERSION__: JSON.stringify(version)
  },
    server: {
    proxy: {
      // Local dev against a real daemon: vite emulates the /api proxy here.
      '/api': {
        target: 'http://127.0.0.1:7350',
        changeOrigin: true
      }
    }
  }
});
