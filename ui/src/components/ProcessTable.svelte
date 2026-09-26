<script lang="ts">
  import type { Process } from '../lib/stores';

  let {
    processes = [] as Process[],
    onAction,
    onOpen,
    onStart
  }: {
    processes?: Process[];
    onAction?: (action: string, ids: string[]) => void;
    onOpen?: (id: string) => void;
    onStart?: () => void;
  } = $props();

  let filter = $state('');
  let statusFilter = $state<'all' | 'running' | 'exited' | 'failed'>('all');
  let selected = $state(new Set<string>());
  let sortKey = $state<keyof Process | 'uptime'>('command');
  let sortDir = $state(1);
  let now = $state(Date.now());

  $effect(() => {
    const t = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(t);
  });

  function idOf(p: Process): string {
    return String(p.id ?? p.process_id ?? '');
  }

  function statusClass(p: Process): 'ok' | 'off' | 'bad' | 'busy' {
    const s = String(p.status ?? '');
    if (s === 'running' || s === 'ready') return 'ok';
    if (s === 'exited' || s === 'stopped') return 'off';
    if (s === 'starting' || s === 'stopping') return 'busy';
    return 'bad';
  }

  function uptimeOf(p: Process): string {
    if (!p.startedAt || p.status === 'exited' || p.status === 'stopped') return '';
    const start = Date.parse(p.startedAt);
    if (Number.isNaN(start)) return '';
    const secs = Math.max(0, Math.floor((now - start) / 1000));
    if (secs < 60) return `${secs}s`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m`;
    if (secs < 86400) return `${Math.floor(secs / 3600)}h${Math.floor((secs % 3600) / 60)}m`;
    return `${Math.floor(secs / 86400)}d`;
  }

  const filtered = $derived(
    processes
      .filter((p) => {
        if (statusFilter === 'all') return true;
        if (statusFilter === 'running') return p.status === 'running' || p.status === 'ready';
        if (statusFilter === 'exited') return p.status === 'exited' || p.status === 'stopped';
        return statusFilter === 'failed' && (p.status === 'failed' || p.status === 'crashed');
      })
      .filter((p) => {
        if (!filter) return true;
        const f = filter.toLowerCase();
        return (
          String(p.command ?? '').toLowerCase().includes(f) ||
          idOf(p).toLowerCase().includes(f) ||
          String(p.pid ?? '').includes(f) ||
          String(p.profile ?? '').toLowerCase().includes(f)
        );
      })
  );

  const rows = $derived(
    [...filtered].sort((a, b) => {
      const av = sortKey === 'uptime' ? uptimeOf(a) : String(a[sortKey] ?? '');
      const bv = sortKey === 'uptime' ? uptimeOf(b) : String(b[sortKey] ?? '');
      return av < bv ? -sortDir : av > bv ? sortDir : 0;
    })
  );

  function sortBy(key: typeof sortKey) {
    if (sortKey === key) sortDir = -sortDir;
    else {
      sortKey = key;
      sortDir = 1;
    }
  }

  function toggle(id: string) {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selected = next;
  }

  function toggleAll() {
    if (selected.size === rows.length) selected = new Set();
    else selected = new Set(rows.map(idOf));
  }

  function bulk(action: string) {
    onAction?.(action, [...selected]);
    selected = new Set();
  }

  const statusCounts = $derived({
    running: processes.filter((p) => p.status === 'running' || p.status === 'ready').length,
    exited: processes.filter((p) => p.status === 'exited' || p.status === 'stopped').length,
    failed: processes.filter((p) => p.status === 'failed' || p.status === 'crashed').length
  });
</script>

<div class="procs">
  <div class="toolbar">
    <input class="search" placeholder="Filter by command, id, pid…" bind:value={filter} aria-label="Filter processes" />
    <div class="chips">
      <button class="chip" class:active={statusFilter === 'all'} onclick={() => (statusFilter = 'all')}>
        All {processes.length}
      </button>
      <button class="chip" class:active={statusFilter === 'running'} onclick={() => (statusFilter = 'running')}>
        Running {statusCounts.running}
      </button>
      <button class="chip" class:active={statusFilter === 'exited'} onclick={() => (statusFilter = 'exited')}>
        Exited {statusCounts.exited}
      </button>
      <button class="chip" class:active={statusFilter === 'failed'} onclick={() => (statusFilter = 'failed')}>
        Failed {statusCounts.failed}
      </button>
    </div>
    <div class="toolbar-spacer"></div>
    {#if selected.size > 0}
      <div class="bulk">
        <span>{selected.size} selected</span>
        <button onclick={() => bulk('restart')}>Restart</button>
        <button onclick={() => bulk('stop')}>Stop</button>
        <button class="danger" onclick={() => bulk('remove')}>Remove</button>
      </div>
    {/if}
    <button class="primary" onclick={onStart}>Start process</button>
  </div>
  {#if rows.length === 0}
    <div class="empty">
      {#if processes.length === 0}
        No processes yet. Start one from the Apps tab or the CLI.
      {:else}
        No processes match this filter.
      {/if}
    </div>
  {:else}
    <table>
      <thead>
        <tr>
          <th class="checkbox">
            <input
              type="checkbox"
              checked={selected.size > 0 && selected.size === rows.length}
              onchange={toggleAll}
              aria-label="Select all"
            />
          </th>
          <th><button onclick={() => sortBy('status')}>Status {sortKey === 'status' ? (sortDir > 0 ? '▲' : '▼') : ''}</button></th>
          <th><button onclick={() => sortBy('command')}>Command {sortKey === 'command' ? (sortDir > 0 ? '▲' : '▼') : ''}</button></th>
          <th><button onclick={() => sortBy('profile')}>Profile {sortKey === 'profile' ? (sortDir > 0 ? '▲' : '▼') : ''}</button></th>
          <th class="num">PID</th>
          <th><button onclick={() => sortBy('uptime')}>Uptime {sortKey === 'uptime' ? (sortDir > 0 ? '▲' : '▼') : ''}</button></th>
          <th class="num">Restarts</th>
          <th>Health</th>
          <th>Ports</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each rows as p (idOf(p))}
          {@const id = idOf(p)}
          <tr class:stale={p.stale}>
            <td class="checkbox">
              <input type="checkbox" checked={selected.has(id)} onchange={() => toggle(id)} aria-label="Select {id}" />
            </td>
            <td><span class="dot {statusClass(p)}"></span>{p.status}</td>
            <td class="mono link" onclick={() => onOpen?.(id)} role="button" tabindex="0"
              onkeydown={(e) => e.key === 'Enter' && onOpen?.(id)}>
              {p.command}{p.args?.length ? ' ' + p.args.join(' ') : ''}
            </td>
            <td>{p.profile ?? ''}</td>
            <td class="mono num">{p.pid ?? ''}</td>
            <td class="mono">{uptimeOf(p)}</td>
            <td class="num" class:warn={(p.restarts ?? 0) > 0}>{p.restarts ?? 0}</td>
            <td>{p.health ?? ''}</td>
            <td class="mono">{Array.isArray(p.ports) && p.ports.length ? p.ports.join(', ') : '—'}</td>
            <td class="actions">
              <button class="ghost" onclick={() => onAction?.('restart', [id])} title="Restart">↻</button>
              <button class="ghost" onclick={() => onAction?.('stop', [id])} title="Stop">■</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .procs {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: var(--space-5);
    gap: var(--space-4);
    overflow: hidden;
  }
  .toolbar {
    display: flex;
    gap: var(--space-4);
    align-items: center;
    flex-wrap: wrap;
  }
  .search {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: var(--space-3) var(--space-4);
    color: var(--text-0);
    min-width: 260px;
  }
  .search:focus {
    border-color: var(--accent);
    outline: none;
  }
  .chips {
    display: flex;
    gap: var(--space-2);
  }
  .toolbar-spacer {
    flex: 1;
  }
  .primary {
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-4);
    font-size: var(--fs-sm);
  }
  .chip {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: var(--space-2) var(--space-4);
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  .chip.active {
    border-color: var(--accent);
    color: var(--text-0);
    background: var(--accent-subtle);
  }
  .bulk {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    padding: var(--space-2) var(--space-4);
  }
  .bulk button,
  .actions button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-3);
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .bulk button.danger {
    color: var(--err);
    border-color: var(--err);
  }
  .empty {
    color: var(--text-2);
    text-align: center;
    padding: var(--space-8);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-sm);
    flex: 1;
    overflow: auto;
    display: block;
  }
  thead {
    display: table;
    width: 100%;
    table-layout: fixed;
  }
  tbody {
    display: table;
    width: 100%;
    table-layout: fixed;
  }
  th,
  td {
    text-align: left;
    padding: var(--space-3) var(--space-4);
    border-bottom: 1px solid var(--border);
  }
  th {
    color: var(--text-2);
    font-weight: 500;
    text-transform: uppercase;
    font-size: var(--fs-xs);
    letter-spacing: 0.04em;
  }
  th button {
    background: none;
    border: none;
    color: inherit;
    padding: 0;
    font: inherit;
  }
  th.checkbox,
  td.checkbox {
    width: 32px;
  }
  th.num,
  td.num {
    text-align: right;
  }
  tr:hover {
    background: var(--bg-2);
  }
  tr.stale {
    background: color-mix(in srgb, var(--warn) 8%, transparent);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .link {
    cursor: pointer;
  }
  .link:hover {
    color: var(--accent);
  }
  .warn {
    color: var(--warn);
  }
  .dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    margin-right: var(--space-3);
  }
  .dot.ok {
    background: var(--ok);
  }
  .dot.off {
    background: var(--neutral);
  }
  .dot.bad {
    background: var(--err);
  }
  .dot.busy {
    background: var(--warn);
    animation: pulse 1.2s infinite;
  }
  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }
  .actions {
    display: flex;
    gap: var(--space-2);
    justify-content: flex-end;
  }
  .ghost {
    background: transparent;
    border: 1px solid transparent;
  }
  .ghost:hover {
    border-color: var(--border-strong);
    background: var(--bg-3);
  }
</style>
