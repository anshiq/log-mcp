<!-- Terminal tab: xterm.js over the Attach stream (server-stream output
  + SendStdin input = half-duplex; full bidi arrives with Connect). -->
<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import { stream } from '../lib/api';

  let { processId = '' }: { processId?: string } = $props();
  let el: HTMLDivElement | null = $state(null);
  let term: Terminal | null = null;
  let close: (() => void) | null = null;

  onMount(() => {
    term = new Terminal({ convertEol: true });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el!);
    fit.fit();
    term.onData((data) => {
      void fetch('/api/agentruntime.v1.ProcessService/SendStdin', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ processId, data })
      });
    });
    close = stream('ProcessService', 'Attach', { processId, backlog: 200 }, (msg) => {
      const out = (msg.output ?? msg.line ?? '') as string;
      if (typeof out === 'string' && out) term?.write(out);
      const lines = msg.lines as { line?: string }[] | undefined;
      if (lines) for (const l of lines) term?.writeln(l.line ?? '');
    });
  });

  onDestroy(() => {
    close?.();
    term?.dispose();
  });
</script>

<div class="term" bind:this={el}></div>

<style>
  .term { height: 100%; background: #000; }
</style>
