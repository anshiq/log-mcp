<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import LogViewer from './LogViewer.svelte';
  import { LogService, ProcessService, type StreamHandle } from '../lib/api';

  let { processId, onClose }: { processId: string; onClose: () => void } = $props();

  let tab = $state<'logs' | 'terminal' | 'env'>('logs');
  let lines = $state<{ line: string; stream?: string; timestamp?: string }[]>([]);
  let env = $state<string[]>([]);
  let envLoading = $state(false);
  let revealed = $state(false);
  let handle: StreamHandle | null = null;

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

  onMount(() => {
    connectLogs();
  });

  onDestroy(() => {
    handle?.close();
  });

  $effect(() => {
    if (tab === 'env') void loadEnv();
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
  </div>
  <div class="body">
    {#if tab === 'logs'}
      <LogViewer {lines} />
    {:else if tab === 'terminal'}
      {#await import('./Terminal.svelte') then { default: Terminal }}
        <Terminal {processId} />
      {/await}
    {:else}
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
