<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import { ProcessService, type StreamHandle } from '../lib/api';

  let { processId = '' }: { processId?: string } = $props();
  let el: HTMLDivElement | null = $state(null);
  let term: Terminal | null = null;
  let fit: FitAddon | null = null;
  let handle: StreamHandle | null = null;
  let resizeObserver: ResizeObserver | null = null;

  onMount(() => {
    term = new Terminal({
      convertEol: true,
      fontFamily: 'JetBrains Mono, ui-monospace, monospace',
      fontSize: 13,
      theme: {
        background: '#0b0d12',
        foreground: '#e8eaf0',
        cursor: '#6d8cff'
      }
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el!);
    fit.fit();

    resizeObserver = new ResizeObserver(() => fit?.fit());
    if (el) resizeObserver.observe(el);

    term.onData((data) => {
      void ProcessService.sendStdin(processId, data);
    });

    handle = ProcessService.attach(processId, 200, (msg) => {
      if (msg.kind === 'batch' && Array.isArray(msg.lines)) {
        for (const l of msg.lines as { line?: string }[]) term?.writeln(l.line ?? '');
        return;
      }
      if (msg.kind === 'line' && msg.line && typeof msg.line === 'object') {
        const l = msg.line as { line?: string };
        term?.writeln(l.line ?? '');
      }
    });
  });

  onDestroy(() => {
    handle?.close();
    resizeObserver?.disconnect();
    term?.dispose();
  });
</script>

<div class="term" bind:this={el}></div>

<style>
  .term {
    height: 100%;
    background: var(--bg-1);
    padding: var(--space-3);
  }
</style>
