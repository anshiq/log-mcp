<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import '@xterm/xterm/css/xterm.css';
  import { ProcessService, type StreamHandle } from '../lib/api';

  let { processId = '', interactive = true }: { processId?: string; interactive?: boolean } = $props();
  let el: HTMLDivElement | null = $state(null);
  let term: Terminal | null = null;
  let fit: FitAddon | null = null;
  let handle: StreamHandle | null = null;
  let resizeObserver: ResizeObserver | null = null;
  let themeObserver: MutationObserver | null = null;
  let ready = $state(false);

  function css(name: string, fallback: string): string {
    const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return v || fallback;
  }

  function palette() {
    return {
      background: css('--bg-0', '#0a0a0b'),
      foreground: css('--text-0', '#f4f1ea'),
      cursor: css('--accent', '#e4002b'),
      cursorAccent: css('--bg-0', '#0a0a0b'),
      selectionBackground: css('--accent-subtle', 'rgba(228,0,43,0.3)'),
      black: css('--ansi-0', '#101012'),
      red: css('--ansi-1', '#f04438'),
      green: css('--ansi-2', '#4caf6d'),
      yellow: css('--ansi-3', '#d9a227'),
      blue: css('--ansi-4', '#6ea8ff'),
      magenta: css('--ansi-5', '#c39bff'),
      cyan: css('--ansi-6', '#4fd1d9'),
      white: css('--ansi-7', '#ece7da'),
      brightBlack: css('--ansi-8', '#6b675c'),
      brightRed: css('--ansi-9', '#ff7b72'),
      brightGreen: css('--ansi-10', '#7ee787'),
      brightYellow: css('--ansi-11', '#e3b341'),
      brightBlue: css('--ansi-12', '#79c0ff'),
      brightMagenta: css('--ansi-13', '#d2a8ff'),
      brightCyan: css('--ansi-14', '#76e3ea'),
      brightWhite: css('--ansi-15', '#ffffff')
    };
  }

  function safeFit() {
    if (!el || el.clientWidth === 0 || el.clientHeight === 0) return;
    try {
      fit?.fit();
    } catch {
      return;
    }
  }

  onMount(() => {
    term = new Terminal({
      convertEol: true,
      fontFamily: "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, monospace",
      fontSize: 12.5,
      lineHeight: 1.25,
      cursorBlink: interactive,
      disableStdin: !interactive,
      scrollback: 5000,
      theme: palette()
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el!);
    safeFit();

    resizeObserver = new ResizeObserver(safeFit);
    if (el) resizeObserver.observe(el);

    themeObserver = new MutationObserver(() => {
      if (term) term.options.theme = palette();
    });
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'class'] });

    term.onData((data) => {
      if (interactive) void ProcessService.sendStdin(processId, data);
    });

    handle = ProcessService.attach(
      processId,
      200,
      (msg) => {
        ready = true;
        if (msg.kind === 'batch' && Array.isArray(msg.lines)) {
          for (const l of msg.lines as { data?: string; line?: string }[]) term?.writeln(l.data ?? l.line ?? '');
          return;
        }
        if (msg.kind === 'output' && typeof msg['data'] === 'string') {
          term?.writeln(msg['data'] as string);
          return;
        }
        if (msg.kind === 'line' && msg.line && typeof msg.line === 'object') {
          const l = msg.line as { data?: string; line?: string };
          term?.writeln(l.data ?? l.line ?? '');
        }
      },
      () => {
        ready = true;
      }
    );
  });

  $effect(() => {
    if (!term) return;
    term.options.disableStdin = !interactive;
    term.options.cursorBlink = interactive;
  });

  onDestroy(() => {
    handle?.close();
    resizeObserver?.disconnect();
    themeObserver?.disconnect();
    term?.dispose();
  });
</script>

<div class="wrap">
  <div class="term" bind:this={el} class:ready></div>
  <div class="hint">
    {#if interactive}
      Keystrokes are sent to the process stdin.
    {:else}
      Process is not running. Output is read-only.
    {/if}
  </div>
</div>

<style>
  .wrap {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-0);
  }
  .term {
    flex: 1;
    min-height: 0;
    padding: var(--space-3) var(--space-4);
    overflow: hidden;
  }
  .term :global(.xterm) {
    height: 100%;
  }
  .term :global(.xterm-viewport) {
    background: transparent !important;
  }
  .hint {
    padding: 5px var(--space-5);
    border-top: 1px solid var(--border);
    font-size: var(--fs-xs);
    color: var(--text-2);
    min-height: 26px;
    background: var(--bg-1);
  }
</style>
