<script lang="ts">
  import { onMount } from 'svelte';
  import { AuditService } from '../../lib/api';
  import { scope } from '../../lib/scope.svelte';

  interface AuditRow {
    id: number;
    ts: number;
    action: string;
    result: string;
    durationMs?: number;
    sessionId?: string;
    harness?: string;
    workspaceId?: string;
  }

  let entries = $state<AuditRow[]>([]);
  let limit = $state(100);
  let loading = $state(false);

  async function load() {
    loading = true;
    try {
      const res = (await AuditService.list(scope.workspaceId, limit)) as unknown;
      const arr = Array.isArray(res) ? res : (res as { entries?: AuditRow[] }).entries ?? [];
      entries = arr as AuditRow[];
    } finally {
      loading = false;
    }
  }

  function loadMore() {
    limit += 100;
    void load();
  }

  onMount(load);
  $effect(() => {
    void scope.workspaceId;
    void load();
  });
</script>

<div class="page">
  {#if loading && entries.length === 0}
    <p class="muted">Loading…</p>
  {:else if entries.length === 0}
    <p class="muted">No audit entries.</p>
  {:else}
    <table>
      <thead>
        <tr>
          <th>Time</th>
          <th>Action</th>
          <th>Result</th>
          <th>Duration</th>
          <th>Harness</th>
          <th>Session</th>
        </tr>
      </thead>
      <tbody>
        {#each entries as e (e.id)}
          <tr>
            <td>{new Date(e.ts * 1000).toLocaleString()}</td>
            <td class="mono">{e.action}</td>
            <td class:err={e.result?.startsWith('error')}>{e.result}</td>
            <td>{e.durationMs ?? 0}ms</td>
            <td>{e.harness ?? ''}</td>
            <td class="mono">{e.sessionId ?? ''}</td>
          </tr>
        {/each}
      </tbody>
    </table>
    <button class="more" onclick={loadMore}>Load more</button>
  {/if}
</div>

<style>
  .page {
    padding: var(--space-5);
    overflow: auto;
    height: 100%;
  }
  .muted {
    color: var(--text-2);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-sm);
  }
  th,
  td {
    text-align: left;
    padding: var(--space-2) var(--space-4);
    border-bottom: 1px solid var(--border);
  }
  th {
    color: var(--text-2);
    text-transform: uppercase;
    font-size: var(--fs-xs);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .err {
    color: var(--err);
  }
  .more {
    margin-top: var(--space-4);
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-4);
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
</style>
