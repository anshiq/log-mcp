<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import LogViewer from './LogViewer.svelte';
  import { LogService, ProcessService, type StreamHandle } from '../lib/api';
  import { getPlatform } from '../lib/platform';

  let { processId, onClose }: { processId: string; onClose: () => void } = $props();

  let tab = $state<'logs' | 'terminal' | 'env' | 'resources'>('logs');
  let lines = $state<{ line: string; stream?: string; timestamp?: string }[]>([]);
  let logMode = $state<'live' | 'search'>('live');
  let searchQuery = $state('');
  let searchRegex = $state(false);
  let searching = $state(false);
  let searchResults = $state<{ line: string; stream?: string; timestamp?: string }[]>([]);
  let searchTruncated = $state(false);
  let env = $state<string[]>([]);
  let envLoading = $state(false);
  let revealed = $state(false);
  let handle: StreamHandle | null = null;

  let cpuPercent = $state(0);
  let memoryBytes = $state(0);
  let ports = $state<number[]>([]);
  let resourcesTimer: ReturnType<typeof setInterval> | null = null;
  let lastCpuNanos = 0;
  let lastSampleAt = 0;

  function connectLogs() {
    handle?.close();
    lines = [];
    handle = LogService.tail('', [processId], 500, (msg) => {
      if (msg.kind === 'batch' && Array.isArray(msg.lines)) {
        lines = [...lines, ...(msg.lines as typeof lines)];
      } else if (msg.kind === 'line') {
        lines = [...lines, msg as unknown as (typeof lines)[number]];
      }
    });
  }

  async function loadEnv() {
    envLoading = true;
    try {
      const res = await ProcessService.getEnv(processId, revealed, false);
      env = (res.env as string[]) ?? [];
    } finally {
      envLoading = false;
    }
  }

  async function sampleResources() {
    const res = await ProcessService.getResourceUsage(processId);
    const now = Date.now();
    if (lastSampleAt > 0) {
      const deltaNanos = res.cpuNanos - lastCpuNanos;
      const deltaMs = now - lastSampleAt;
      if (deltaMs > 0) cpuPercent = Math.max(0, (deltaNanos / 1e6 / deltaMs) * 100);
    }
    lastCpuNanos = res.cpuNanos;
    lastSampleAt = now;
    memoryBytes = res.memoryBytes;
    ports = res.ports ?? [];
  }

  function startResourcePolling() {
    stopResourcePolling();
    lastCpuNanos = 0;
    lastSampleAt = 0;
    void sampleResources();
    resourcesTimer = setInterval(sampleResources, 2000);
  }

  function stopResourcePolling() {
    if (resourcesTimer) clearInterval(resourcesTimer);
    resourcesTimer = null;
  }

  async function runSearch() {
    if (!searchQuery.trim()) return;
    searching = true;
    try {
      const res = await LogService.search({
        processIds: [processId],
        query: searchQuery.trim(),
        regex: searchRegex,
        maxRows: 500
      });
      const matches = (res.matches as { line: string; stream?: string; timestamp?: string | number }[]) ?? [];
      searchResults = matches.map((m) => ({ line: m.line, stream: m.stream, timestamp: m.timestamp?.toString() }));
      searchTruncated = !!res.truncatedScan;
    } finally {
      searching = false;
    }
  }

  async function clearLogs() {
    if (!confirm('Clear this process\'s logs?')) return;
    await LogService.clear(processId);
    lines = [];
  }

  async function exportLogs() {
    const text = lines.map((l) => `${l.timestamp ? l.timestamp + ' ' : ''}${l.stream ? '[' + l.stream + '] ' : ''}${l.line}`).join('\n');
    await getPlatform().saveFile(`${processId}.log`, text);
  }

  function formatBytes(b: number): string {
    if (b < 1024) return `${b} B`;
    if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
    if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`;
    return `${(b / 1024 / 1024 / 1024).toFixed(1)} GB`;
  }

  onMount(() => {
    connectLogs();
  });

  onDestroy(() => {
    handle?.close();
    stopResourcePolling();
  });

  $effect(() => {
    if (tab === 'env') void loadEnv();
    if (tab === 'resources') startResourcePolling();
    else stopResourcePolling();
  });

  $effect(() => {
    void processId;
    connectLogs();
  });
</script>

<div class="detail">
  <div class="header">
    <span class="id mono">{processId}</span>
    <button class="close" onclick={onClose} aria-label="Close">✕</button>
  </div>
  <div class="tabs">
    <button class:active={tab === 'logs'} onclick={() => (tab = 'logs')}>Logs</button>
    <button class:active={tab === 'terminal'} onclick={() => (tab = 'terminal')}>Terminal</button>
    <button class:active={tab === 'env'} onclick={() => (tab = 'env')}>Env</button>
    <button class:active={tab === 'resources'} onclick={() => (tab = 'resources')}>Resources</button>
  </div>
  <div class="body">
    {#if tab === 'logs'}
      <div class="logtoolbar">
        <div class="modeswitch">
          <button class:active={logMode === 'live'} onclick={() => (logMode = 'live')}>Live</button>
          <button class:active={logMode === 'search'} onclick={() => (logMode = 'search')}>Search</button>
        </div>
        {#if logMode === 'search'}
          <form
            class="searchform"
            onsubmit={(e) => {
              e.preventDefault();
              void runSearch();
            }}
          >
            <input placeholder="Search history…" bind:value={searchQuery} aria-label="Search logs" />
            <label class="regex">
              <input type="checkbox" bind:checked={searchRegex} />
              regex
            </label>
            <button type="submit" disabled={searching}>{searching ? 'Searching…' : 'Search'}</button>
          </form>
        {:else}
          <button onclick={clearLogs}>Clear</button>
          <button onclick={exportLogs}>Export</button>
        {/if}
      </div>
      {#if logMode === 'live'}
        <div class="logbody"><LogViewer {lines} /></div>
      {:else}
        <div class="logbody">
          {#if searchTruncated}
            <p class="hint">Results were truncated; narrow the query for a complete scan.</p>
          {/if}
          <LogViewer lines={searchResults} />
        </div>
      {/if}
    {:else if tab === 'terminal'}
      {#await import('./Terminal.svelte') then { default: Terminal }}
        <Terminal {processId} />
      {/await}
    {:else if tab === 'env'}
      <div class="env">
        <label class="reveal">
          <input type="checkbox" bind:checked={revealed} onchange={loadEnv} />
          Reveal secrets
        </label>
        {#if envLoading}
          <p class="muted">Loading…</p>
        {:else}
          <ul class="mono">
            {#each env as line}
              <li>{line}</li>
            {/each}
          </ul>
        {/if}
      </div>
    {:else}
      <div class="resources">
        <div class="stat">
          <span class="label">CPU</span>
          <span class="value">{cpuPercent.toFixed(1)}%</span>
        </div>
        <div class="stat">
          <span class="label">Memory</span>
          <span class="value">{formatBytes(memoryBytes)}</span>
        </div>
        <div class="stat">
          <span class="label">Ports</span>
          <span class="value mono">{ports.length ? ports.join(', ') : '—'}</span>
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .detail {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--bg-1);
    border-left: 1px solid var(--border);
  }
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-4) var(--space-5);
    border-bottom: 1px solid var(--border);
  }
  .id {
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  .close {
    background: transparent;
    border: none;
    color: var(--text-1);
    font-size: var(--fs-md);
  }
  .tabs {
    display: flex;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-5) 0;
  }
  .tabs button {
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-1);
    padding: var(--space-3) var(--space-2);
    font-size: var(--fs-sm);
  }
  .tabs button.active {
    color: var(--text-0);
    border-bottom-color: var(--accent);
  }
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .logtoolbar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-5);
    border-bottom: 1px solid var(--border);
  }
  .logtoolbar button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-3);
    color: var(--text-0);
    font-size: var(--fs-xs);
  }
  .modeswitch {
    display: flex;
    gap: var(--space-1);
  }
  .modeswitch button.active {
    background: var(--accent-subtle);
    border-color: var(--accent);
    color: var(--text-0);
  }
  .searchform {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1;
  }
  .searchform input:not([type]) {
    flex: 1;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-3);
    color: var(--text-0);
    font-size: var(--fs-xs);
  }
  .regex {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    color: var(--text-1);
    font-size: var(--fs-xs);
    white-space: nowrap;
  }
  .hint {
    color: var(--warn);
    font-size: var(--fs-xs);
    padding: var(--space-2) var(--space-5) 0;
    margin: 0;
  }
  .logbody {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .logbody > :global(.logwrap) {
    flex: 1;
    min-height: 0;
  }
  .resources {
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .stat {
    display: flex;
    justify-content: space-between;
    padding: var(--space-3) var(--space-4);
    background: var(--bg-2);
    border-radius: var(--radius);
  }
  .stat .label {
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .stat .value {
    font-size: var(--fs-md);
    font-weight: 500;
  }
  .env {
    padding: var(--space-5);
    height: 100%;
    overflow: auto;
  }
  .reveal {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-sm);
    color: var(--text-1);
    margin-bottom: var(--space-4);
  }
  .env ul {
    list-style: none;
    margin: 0;
    padding: 0;
    font-size: var(--fs-sm);
  }
  .env li {
    padding: var(--space-1) 0;
    color: var(--text-0);
  }
  .muted {
    color: var(--text-2);
  }
  .mono {
    font-family: var(--font-mono);
  }
</style>
