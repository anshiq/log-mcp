<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { SessionService } from '../../lib/api';

  interface SessionRow {
    id: string;
    kind: string;
    harness: string;
    clientPid?: number;
    workspaceId?: string;
    startedAt?: number;
    lastSeenAt?: number;
    closedAt?: number;
  }

  let sessions = $state<SessionRow[]>([]);
  let timer: ReturnType<typeof setInterval> | null = null;

  async function load() {
    const res = (await SessionService.list()) as unknown;
    const arr = Array.isArray(res) ? res : (res as { sessions?: SessionRow[] }).sessions ?? [];
    sessions = arr as SessionRow[];
  }

  async function close(id: string) {
    if (!confirm('Close this session? Its lifetime:session processes will stop after the grace period.')) return;
    await SessionService.close(id);
    await load();
  }

  async function detail(id: string) {
    try {
      await SessionService.get(id);
    } catch {
    }
  }

  onMount(() => {
    void load();
    timer = setInterval(load, 5000);
  });
  onDestroy(() => {
    if (timer) clearInterval(timer);
  });

  function ago(ts?: number): string {
    if (!ts) return '—';
    const secs = Math.floor(Date.now() / 1000 - ts);
    if (secs < 60) return `${secs}s ago`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
    return `${Math.floor(secs / 3600)}h ago`;
  }
</script>

<div class="page">
  {#if sessions.length === 0}
    <p class="muted">No connected sessions.</p>
  {:else}
    <table>
      <thead>
        <tr>
          <th>Kind</th>
          <th>Harness</th>
          <th>PID</th>
          <th>Workspace</th>
          <th>Started</th>
          <th>Last seen</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each sessions as s (s.id)}
          <tr class:closed={!!s.closedAt} onclick={() => void detail(s.id)}>
            <td>{s.kind}</td>
            <td>{s.harness || '—'}</td>
            <td class="mono">{s.clientPid ?? ''}</td>
            <td class="mono">{s.workspaceId ?? ''}</td>
            <td>{ago(s.startedAt)}</td>
            <td>{ago(s.lastSeenAt)}</td>
            <td>
              {#if !s.closedAt}
                <button onclick={() => close(s.id)}>Close</button>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
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
    padding: var(--space-3) var(--space-4);
    border-bottom: 1px solid var(--border);
  }
  th {
    color: var(--text-2);
    text-transform: uppercase;
    font-size: var(--fs-xs);
  }
  tr.closed {
    opacity: 0.5;
  }
  .mono {
    font-family: var(--font-mono);
  }
  button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-3);
    color: var(--text-0);
    font-size: var(--fs-xs);
  }
</style>
