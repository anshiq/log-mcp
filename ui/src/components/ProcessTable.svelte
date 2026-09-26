<!-- Process table (Docker-Desktop-style): status dot, name, project,
  pid, uptime, restarts, health, CPU/RSS, started-by. Sortable columns,
  filter chips, text filter, multi-select with bulk actions. -->
<script lang="ts">
  import type { Process } from '../lib/stores';

  let { processes = [] as Process[], onAction }: {
    processes?: Process[];
    onAction?: (action: string, ids: string[]) => void;
  } = $props();

  let filter = $state('');
  let selected = $state(new Set<string>());
  let sortKey = $state('command');
  let sortDir = $state(1);

  const rows = $derived(
    processes
      .filter((p) => !filter || JSON.stringify(p).toLowerCase().includes(filter.toLowerCase()))
      .sort((a, b) => {
        const av = String(a[sortKey] ?? '');
        const bv = String(b[sortKey] ?? '');
        return av < bv ? -sortDir : av > bv ? sortDir : 0;
      })
  );

  function toggle(id: string) {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selected = next;
  }

  function statusDot(p: Process) {
    const s = String(p.status ?? '');
    return s === 'running' || s === 'ready' ? 'ok' : s === 'exited' || s === 'stopped' ? 'off' : 'bad';
  }
</script>

<div class="procs">
  <div class="toolbar">
    <input placeholder="Filter…" bind:value={filter} aria-label="Filter processes" />
    <span>{rows.length} processes</span>
    {#if selected.size > 0}
      <button onclick={() => onAction?.('stop', [...selected])}>Stop</button>
      <button onclick={() => onAction?.('restart', [...selected])}>Restart</button>
    {/if}
  </div>
  <table>
    <thead>
      <tr>
        <th></th>
        <th><button onclick={() => (sortKey = 'status')}>Status</button></th>
        <th><button onclick={() => (sortKey = 'command')}>Command</button></th>
        <th><button onclick={() => (sortKey = 'profile')}>Profile</button></th>
        <th>PID</th>
        <th>Health</th>
      </tr>
    </thead>
    <tbody>
      {#each rows as p (p.process_id ?? p.id)}
        {@const id = String(p.process_id ?? p.id)}
        <tr>
          <td><input type="checkbox" checked={selected.has(id)} onchange={() => toggle(id)} aria-label="Select {id}" /></td>
          <td><span class="dot {statusDot(p)}"></span>{p.status}</td>
          <td class="mono">{p.command}</td>
          <td>{p.profile ?? ''}</td>
          <td class="mono">{p.pid ?? ''}</td>
          <td>{p.health ?? ''}</td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .toolbar { display: flex; gap: 12px; align-items: center; margin-bottom: 8px; }
  table { width: 100%; border-collapse: collapse; }
  th, td { text-align: left; padding: 6px 8px; border-bottom: 1px solid #30363d; }
  .mono { font-family: monospace; }
  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; }
  .dot.ok { background: #3fb950; }
  .dot.off { background: #6e7681; }
  .dot.bad { background: #f47067; }
</style>
