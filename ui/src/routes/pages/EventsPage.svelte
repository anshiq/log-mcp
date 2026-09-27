<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { EventService } from '../../lib/api';
  import { scope } from '../../lib/scope.svelte';

  interface EventRow {
    id?: number;
    ts?: number;
    type?: string;
    processId?: string;
    instanceId?: string;
    [key: string]: unknown;
  }

  let events = $state<EventRow[]>([]);
  let typeFilter = $state('all');
  let handle: { close(): void } | null = null;

  const types = [
    'all',
    'process.started',
    'process.stopped',
    'process.exited',
    'process.failed',
    'process.restarted',
    'process.healthy',
    'process.unhealthy',
    'process.crashed',
    'process.removed',
    'logs.alert'
  ];

  async function loadHistory() {
    const res = (await EventService.list(scope.workspaceId, 200)) as unknown as { events: unknown[] };
    events = ((res.events ?? []) as unknown as EventRow[]).reverse();
  }

  function connect() {
    handle?.close();
    handle = EventService.watch(scope.workspaceId, (msg) => {
      if (msg.kind !== 'event') return;
      events = [msg as EventRow, ...events].slice(0, 500);
    });
  }

  onMount(() => {
    void loadHistory();
    connect();
  });

  onDestroy(() => handle?.close());

  $effect(() => {
    void scope.workspaceId;
    void loadHistory();
    connect();
  });

  const filtered = $derived(typeFilter === 'all' ? events : events.filter((e) => e.type === typeFilter));

  function iconFor(type?: string): string {
    if (!type) return '•';
    if (type.includes('failed') || type.includes('crashed')) return '✕';
    if (type.includes('unhealthy')) return '⚠';
    if (type.includes('started')) return '▶';
    if (type.includes('stopped') || type.includes('exited')) return '■';
    if (type.includes('alert')) return '🔔';
    return '•';
  }
</script>

<div class="page">
  <div class="toolbar">
    <select bind:value={typeFilter} aria-label="Event type filter">
      {#each types as t}
        <option value={t}>{t}</option>
      {/each}
    </select>
    <span class="count">{filtered.length} events</span>
  </div>
  {#if filtered.length === 0}
    <p class="muted">No events yet.</p>
  {:else}
    <ul class="events">
      {#each filtered as e (e.id ?? e.cursor ?? Math.random())}
        <li class={e.type?.includes('failed') || e.type?.includes('crashed') ? 'bad' : ''}>
          <span class="icon">{iconFor(e.type)}</span>
          <span class="type mono">{e.type}</span>
          {#if e.processId}<span class="proc mono">{e.processId}</span>{/if}
          {#if e.ts}<span class="time">{new Date(Number(e.ts) / 1e6).toLocaleString()}</span>{/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .page {
    padding: var(--space-5);
    overflow: auto;
    height: 100%;
  }
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    margin-bottom: var(--space-4);
  }
  select {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    color: var(--text-0);
  }
  .count {
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .muted {
    color: var(--text-2);
  }
  .events {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .events li {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--border);
    font-size: var(--fs-sm);
  }
  .events li.bad {
    color: var(--err);
  }
  .icon {
    width: 20px;
    text-align: center;
  }
  .type {
    color: var(--text-0);
  }
  .proc {
    color: var(--text-2);
  }
  .time {
    margin-left: auto;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .mono {
    font-family: var(--font-mono);
  }
</style>
