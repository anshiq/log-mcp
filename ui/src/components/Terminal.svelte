<script lang="ts">
  import { onDestroy, tick, untrack } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import { WebLinksAddon } from '@xterm/addon-web-links';
  import '@xterm/xterm/css/xterm.css';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import Eraser from '@lucide/svelte/icons/eraser';
  import Zap from '@lucide/svelte/icons/zap';
  import SquareTerminal from '@lucide/svelte/icons/square-terminal';
  import { ProcessService, type StreamHandle, type StreamState } from '../lib/api';
  import { router } from '../lib/router.svelte';
  import { toasts, toastError } from '../lib/toasts.svelte';

  let { processId = '', interactive = true }: { processId?: string; interactive?: boolean } = $props();

  let el: HTMLDivElement | null = $state(null);
  let term: Terminal | null = null;
  let fit: FitAddon | null = null;
  let handle: StreamHandle | null = null;
  let resizeObserver: ResizeObserver | null = null;
  let themeObserver: MutationObserver | null = null;
  let fitTimer: ReturnType<typeof setInterval> | null = null;
  let conn = $state<StreamState>('connecting');
  let stdinDead = $state(false);
  let stdinError = $state('');
  let openingShell = $state(false);
  let attachedId = $state('');

  const canWrite = $derived(interactive && !stdinDead);
  const statusText = $derived(
    stdinDead
      ? stdinError || 'Stdin is closed. Output is read-only.'
      : !interactive
        ? 'Process is not running. Output is read-only.'
        : conn === 'open'
          ? 'Connected. Keystrokes go to stdin. Prompts without a newline cannot display until the process prints one.'
          : conn === 'reconnecting'
            ? 'Reconnecting…'
            : 'Connecting…'
  );

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

  function writeText(raw: string) {
    if (!term || raw === '') return;
    const normalized = raw.replace(/\r\n/g, '\n').replace(/\r/g, '\n').replace(/\n/g, '\r\n');
    term.write(normalized);
  }

  function writeLine(raw: string) {
    if (!term) return;
    const stripped = raw.replace(/(\r\n|\n|\r)$/, '');
    writeText(stripped + '\r\n');
  }

  function extractText(v: unknown): string {
    if (typeof v === 'string') return v;
    if (v && typeof v === 'object') {
      const o = v as Record<string, unknown>;
      const d = o['data'] ?? o['line'] ?? o['text'];
      if (typeof d === 'string') return d;
    }
    return '';
  }

  function onAttachMessage(msg: Record<string, unknown>) {
    const kind = msg['kind'];
    if (kind === 'batch' && Array.isArray(msg['lines'])) {
      for (const l of msg['lines'] as unknown[]) writeLine(extractText(l));
      return;
    }
    if (kind === 'output') {
      const text = extractText(msg['data']);
      if (text !== '') writeLine(text);
      return;
    }
    if (kind === 'line') {
      const text = extractText(msg['line'] ?? msg);
      if (text !== '') writeLine(text);
    }
  }

  function closeAttach() {
    handle?.close();
    handle = null;
  }

  function connect(id: string) {
    closeAttach();
    if (!id || !term) return;
    attachedId = id;
    stdinDead = false;
    stdinError = '';
    conn = 'connecting';
    handle = ProcessService.attach(
      id,
      200,
      (msg) => onAttachMessage(msg as Record<string, unknown>),
      (s) => {
        conn = s;
        if (s === 'open') void tick().then(() => safeFit(true));
      }
    );
  }

  function safeFit(force = false) {
    if (!el || !term || !fit) return;
    if (el.clientWidth === 0 || el.clientHeight === 0) return;
    try {
      fit.fit();
    } catch {
      if (force) requestAnimationFrame(() => safeFit());
    }
  }

  function ensureTerm() {
    if (term || !el) return;
    term = new Terminal({
      convertEol: false,
      fontFamily: "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, monospace",
      fontSize: 12.5,
      lineHeight: 1.25,
      cursorBlink: canWrite,
      cursorStyle: 'bar',
      disableStdin: !canWrite,
      scrollback: 10000,
      theme: palette()
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(el);
    safeFit(true);

    term.onData((data) => {
      if (!canWrite || !attachedId) return;
      void ProcessService.sendStdin(attachedId, data).catch((err: unknown) => {
        stdinDead = true;
        const msg = err instanceof Error ? err.message : String(err);
        stdinError = /already|closed|dead|stdin/i.test(msg) ? 'Stdin is closed. Output is read-only.' : msg;
      });
    });
  }

  $effect(() => {
    const host = el;
    if (!host) return;
    untrack(() => ensureTerm());

    resizeObserver?.disconnect();
    resizeObserver = new ResizeObserver(() => safeFit());
    resizeObserver.observe(host);

    themeObserver?.disconnect();
    themeObserver = new MutationObserver(() => {
      if (term) term.options.theme = palette();
    });
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'class'] });

    fitTimer?.valueOf();
    if (fitTimer) clearInterval(fitTimer);
    fitTimer = setInterval(() => safeFit(), 1000);

    return () => {
      resizeObserver?.disconnect();
      themeObserver?.disconnect();
      if (fitTimer) clearInterval(fitTimer);
    };
  });

  $effect(() => {
    const id = processId;
    untrack(() => {
      ensureTerm();
      term?.clear();
      connect(id);
      void tick().then(() => safeFit(true));
    });
  });

  $effect(() => {
    const w = canWrite;
    untrack(() => {
      if (!term) return;
      term.options.disableStdin = !w;
      term.options.cursorBlink = w;
    });
  });

  onDestroy(() => {
    closeAttach();
    resizeObserver?.disconnect();
    themeObserver?.disconnect();
    if (fitTimer) clearInterval(fitTimer);
    term?.dispose();
    term = null;
  });

  function reconnect() {
    term?.clear();
    connect(processId);
  }

  function clearTerm() {
    term?.clear();
  }

  async function sendSignal(sig: 'SIGINT' | 'SIGTERM') {
    try {
      await ProcessService.signal(processId, sig);
      toasts.ok(`Sent ${sig}`);
    } catch (err) {
      toastError(err);
    }
  }

  async function openShellHere() {
    if (openingShell) return;
    openingShell = true;
    try {
      const res = await ProcessService.openShell({ processId });
      const id = (res as { processId?: string }).processId;
      if (id) {
        toasts.ok('Shell opened');
        void router.navigate(`/processes/${id}?tab=terminal`);
      }
    } catch (err) {
      toastError(err);
    } finally {
      openingShell = false;
    }
  }

  export function focus() {
    term?.focus();
  }
</script>

<div class="wrap">
  <div class="tbar">
    <span class="conn" class:on={conn === 'open' && canWrite} class:off={!canWrite} title={statusText}>
      <span class="dot"></span>
      {#if !canWrite}Read-only{:else if conn === 'open'}Live{:else if conn === 'reconnecting'}Reconnecting{:else}Connecting{/if}
    </span>
    <span class="sp"></span>
    <button type="button" class="btn ghost icon sm" title="Reconnect" aria-label="Reconnect" onclick={reconnect}>
      <RotateCw size={14} />
    </button>
    <button type="button" class="btn ghost icon sm" title="Clear terminal" aria-label="Clear terminal" onclick={clearTerm}>
      <Eraser size={14} />
    </button>
    <button type="button" class="btn ghost sm sig" title="Send Ctrl+C (SIGINT)" disabled={!interactive} onclick={() => void sendSignal('SIGINT')}>
      <Zap size={13} /> SIGINT
    </button>
    <button type="button" class="btn ghost sm shell" title="Open a new interactive shell in this process workdir and env" disabled={openingShell} onclick={() => void openShellHere()}>
      <SquareTerminal size={13} /> {openingShell ? 'Opening…' : 'Shell here'}
    </button>
  </div>
  <div class="term" bind:this={el}></div>
  <div class="hint" title={statusText}>{statusText}</div>
</div>

<style>
  .wrap {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-0);
  }
  .tbar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px var(--space-4);
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
  }
  .conn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .conn.on {
    color: var(--text-1);
  }
  .conn.off {
    color: var(--text-2);
  }
  .conn .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--neutral, #888);
  }
  .conn.on .dot {
    background: var(--ok);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 20%, transparent);
  }
  .sp {
    flex: 1;
  }
  .tbar .btn.sm {
    height: 26px;
    font-size: var(--fs-xs);
  }
  .term {
    flex: 1;
    min-height: 240px;
    padding: var(--space-3) var(--space-4);
    overflow: hidden;
    cursor: text;
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
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
