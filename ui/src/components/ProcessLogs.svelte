<script lang="ts">
  import { onDestroy } from 'svelte';
  import Search from '@lucide/svelte/icons/search';
  import Eraser from '@lucide/svelte/icons/eraser';
  import Download from '@lucide/svelte/icons/download';
  import Hourglass from '@lucide/svelte/icons/hourglass';
  import Radio from '@lucide/svelte/icons/radio';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import Regex from '@lucide/svelte/icons/regex';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import LogViewer from './LogViewer.svelte';
  import ActionMenu, { type MenuEntry } from './ActionMenu.svelte';
  import { procActions } from './processActions.svelte';
  import { LogService, ProcessService, type StreamHandle } from '../lib/api';
  import { getPlatform } from '../lib/platform';
  import { toasts, toastError } from '../lib/toasts.svelte';

  interface Line {
    line: string;
    stream?: string;
    timestamp?: string;
  }

  let { processId, live }: { processId: string; live: boolean } = $props();

  const MAX_LINES = 20000;
  const KEEP_LINES = 15000;

  let mode = $state<'live' | 'search'>('live');
  let lines = $state<Line[]>([]);
  let streaming = $state(false);
  let query = $state('');
  let regex = $state(false);
  let searching = $state(false);
  let searched = $state(false);
  let results = $state<Line[]>([]);
  let truncated = $state(false);
  let handle: StreamHandle | null = null;

  function append(next: Line[]) {
    let merged = lines.concat(next);
    if (merged.length > MAX_LINES) merged = merged.slice(-KEEP_LINES);
    lines = merged;
  }

  function connect() {
    handle?.close();
    lines = [];
    streaming = false;
    handle = LogService.tail(
      '',
      [processId],
      500,
      (msg) => {
        if (msg.kind === 'batch' && Array.isArray(msg.lines)) append(msg.lines as Line[]);
        else if (msg.kind === 'line') append([msg as unknown as Line]);
      },
      (s) => {
        streaming = s === 'open';
      }
    );
  }

  $effect(() => {
    void processId;
    connect();
  });

  onDestroy(() => handle?.close());

  async function runSearch() {
    if (!query.trim()) return;
    searching = true;
    try {
      const res = await LogService.search({ processIds: [processId], query: query.trim(), regex, maxRows: 500 });
      const matches = (res.matches as { line: string; stream?: string; timestamp?: string | number }[]) ?? [];
      results = matches.map((m) => ({ line: m.line, stream: m.stream, timestamp: m.timestamp?.toString() }));
      truncated = !!res.truncatedScan;
      searched = true;
    } catch (err) {
      toastError(err);
    } finally {
      searching = false;
    }
  }

  async function clearLogs() {
    const ok = await procActions.ask({
      title: 'Clear logs',
      message: "Delete this process's stored log lines? This cannot be undone.",
      confirmLabel: 'Clear logs',
      danger: true
    });
    if (!ok) return;
    try {
      await LogService.clear(processId);
      lines = [];
      results = [];
      toasts.ok('Logs cleared');
    } catch (err) {
      toastError(err);
    }
  }

  async function exportLogs() {
    let data = '';
    let finished = false;
    const finish = () => {
      if (finished) return;
      finished = true;
      h.close();
      void getPlatform().saveFile(`${processId}.log`, data || lines.map((l) => l.line).join('\n'));
    };
    const h = LogService.exportLogs(processId, 'txt', (msg) => {
      if (msg.kind === 'chunk' && typeof msg['data'] === 'string') data += msg['data'] as string;
      if (msg.kind === 'done') finish();
    });
    setTimeout(() => {
      if (!data) finish();
    }, 5000);
  }

  async function waitExit() {
    try {
      const res = await ProcessService.waitForExit(processId, 5000);
      if (res.exited) toasts.ok(`Process exited${res.exitCode !== undefined ? ` with code ${res.exitCode}` : ''}`);
      else toasts.info('Still running after 5 seconds');
    } catch (err) {
      toastError(err);
    }
  }

  async function waitReady() {
    try {
      const res = await LogService.waitForLog(processId, 'ready', undefined, 5000);
      if (res.matched) toasts.ok(`Matched: ${res.entry?.line ?? 'ready'}`);
      else toasts.info('No "ready" log line within 5 seconds');
    } catch (err) {
      toastError(err);
    }
  }

  const moreItems: MenuEntry[] = [
    { id: 'export', label: 'Export logs', icon: Download, onselect: () => void exportLogs() },
    { id: 'clear', label: 'Clear logs', icon: Eraser, danger: true, onselect: () => void clearLogs() },
    { type: 'separator' },
    { id: 'wait-exit', label: 'Wait for exit (5s)', icon: Hourglass, onselect: () => void waitExit() },
    { id: 'wait-log', label: 'Wait for "ready" line (5s)', icon: Radio, onselect: () => void waitReady() }
  ];
</script>

<div class="logs">
  <div class="bar">
    <div class="seg" role="group" aria-label="Log mode">
      <button type="button" class:active={mode === 'live'} aria-pressed={mode === 'live'} onclick={() => (mode = 'live')}>Live</button>
      <button type="button" class:active={mode === 'search'} aria-pressed={mode === 'search'} onclick={() => (mode = 'search')}>Search</button>
    </div>
    {#if mode === 'search'}
      <form
        class="search"
        onsubmit={(e) => {
          e.preventDefault();
          void runSearch();
        }}
      >
        <div class="input-wrap">
          <Search size={13} />
          <input placeholder="Search log history…" bind:value={query} aria-label="Search logs" />
        </div>
        <button
          type="button"
          class="btn icon sm regex"
          class:on={regex}
          aria-pressed={regex}
          title="Regular expression"
          aria-label="Regular expression"
          onclick={() => (regex = !regex)}
        >
          <Regex size={14} />
        </button>
        <button type="submit" class="btn sm primary" disabled={searching || !query.trim()}>{searching ? 'Searching…' : 'Search'}</button>
      </form>
    {:else}
      <span class="stream" class:on={streaming && live}>
        <span class="dot {streaming && live ? 'ok' : ''}"></span>
        {live ? (streaming ? 'Streaming' : 'Connecting…') : 'Process not running'}
      </span>
      <span class="spacer"></span>
      <button type="button" class="btn ghost icon sm" title="Export logs" aria-label="Export logs" onclick={() => void exportLogs()}>
        <Download size={14} />
      </button>
      <button type="button" class="btn ghost icon sm" title="Clear logs" aria-label="Clear logs" onclick={() => void clearLogs()}>
        <Eraser size={14} />
      </button>
      <ActionMenu items={moreItems} label="More log actions">
        <Ellipsis size={15} />
      </ActionMenu>
    {/if}
  </div>
  {#if mode === 'search' && truncated}
    <div class="banner warn note"><TriangleAlert size={14} /> Results were truncated. Narrow the query for a complete scan.</div>
  {/if}
  <div class="logbody">
    {#if mode === 'live'}
      <LogViewer {lines} />
    {:else if !searched}
      <div class="hint-pane">
        <Search size={20} />
        <p>Search the stored history of this process. Use the regex toggle for patterns.</p>
      </div>
    {:else}
      <LogViewer lines={results} />
    {/if}
  </div>
  <div class="foot">
    {#if mode === 'live'}
      <span>{lines.length.toLocaleString()} lines</span>
    {:else if searched}
      <span>{results.length.toLocaleString()} matches</span>
    {/if}
  </div>
</div>

<style>
  .logs {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-5);
    border-bottom: 1px solid var(--border);
    min-height: 46px;
  }
  .seg > button {
    padding: 2px 12px;
    font-size: var(--fs-xs);
    font-weight: 500;
  }
  .spacer {
    flex: 1;
  }
  .stream {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .stream.on {
    color: var(--text-1);
  }
  .stream .dot {
    width: 6px;
    height: 6px;
    box-shadow: none;
  }
  .search {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1;
    min-width: 0;
  }
  .search .input-wrap {
    flex: 1;
    min-width: 0;
  }
  .search input {
    padding-top: 4px;
    padding-bottom: 4px;
    font-size: var(--fs-xs);
  }
  .regex.on {
    background: var(--accent-subtle);
    border-color: color-mix(in srgb, var(--accent) 50%, transparent);
    color: var(--accent);
  }
  .note {
    margin: var(--space-3) var(--space-5) 0;
    font-size: var(--fs-xs);
    padding: 6px 10px;
  }
  .logbody {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-1);
  }
  .logbody > :global(*) {
    flex: 1;
    min-height: 0;
  }
  .hint-pane {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    color: var(--text-2);
    text-align: center;
    padding: var(--space-8);
  }
  .hint-pane p {
    max-width: 280px;
    font-size: var(--fs-sm);
    margin: 0;
  }
  .foot {
    display: flex;
    justify-content: flex-end;
    padding: 5px var(--space-5);
    border-top: 1px solid var(--border);
    font-size: var(--fs-xs);
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
    min-height: 26px;
  }
</style>
