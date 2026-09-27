<script lang="ts">
  import { onMount } from 'svelte';
  import { LogService } from '../../lib/api';
  import { LogBuffer } from '../../lib/state/logs.svelte';
  import LogView from '../../features/logs/LogView.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { getPlatform } from '../../lib/platform';

  let buffer = new LogBuffer(200000);
  let view = $state<LogView | null>(null);
  let processIds = $state('');
  let searchMode = $state(false);
  let query = $state('');
  let results = $state<Record<string, unknown>[]>([]);
  let closer: (() => void) | null = null;

  function tail() {
    closer?.();
    const ids = processIds.split(',').map((s) => s.trim()).filter(Boolean);
    const h = LogService.tail(scopeState.workspaceId, ids, 500, (msg) => {
      if (msg.kind === 'batch' && Array.isArray(msg['lines'])) {
        for (const l of msg['lines'] as Record<string, unknown>[]) {
          view?.append({
            id: Number(l['id'] ?? 0),
            ts: Date.parse(String(l['timestamp'] ?? '')) || Date.now(),
            stream: String(l['stream'] ?? 'stdout'),
            level: String(l['level'] ?? 'info'),
            text: String(l['line'] ?? ''),
            proc: String(l['processId'] ?? '')
          });
        }
      } else if (msg.kind === 'line') {
        view?.append({
          id: Number(msg['id'] ?? 0),
          ts: Date.parse(String(msg['timestamp'] ?? '')) || Date.now(),
          stream: String(msg['stream'] ?? 'stdout'),
          level: String(msg['level'] ?? 'info'),
          text: String(msg['line'] ?? ''),
          proc: String(msg['processId'] ?? '')
        });
      }
    });
    closer = () => h.close();
  }

  async function search() {
    const r = await LogService.search({ workspaceId: scopeState.workspaceId, query, maxRows: 100 });
    results = r.matches ?? [];
  }

  async function exportAll() {
    const ids = processIds.split(',').map((s) => s.trim()).filter(Boolean);
    if (ids.length === 0) return;
    const first = ids[0] as string;
    let data = '';
    const h = LogService.exportLogs(first, 'txt', (msg) => {
      if (msg.kind === 'chunk' && typeof msg['data'] === 'string') data += msg['data'] as string;
      if (msg.kind === 'done') {
        h.close();
        void getPlatform().saveFile(`${first}.log`, data);
      }
    });
  }

  onMount(() => {
    const m = window.location.hash.match(/[?&]p=([^&]+)/);
    if (m && m[1]) processIds = decodeURIComponent(m[1]);
    tail();
    return () => closer?.();
  });
</script>

<div class="page">
  <h2>Logs</h2>
  <div class="bar">
    <input placeholder="process ids, comma separated" bind:value={processIds} />
    <button onclick={tail}>Tail</button>
    <button onclick={() => (searchMode = !searchMode)}>{searchMode ? 'Live' : 'Search history'}</button>
    <button onclick={() => void exportAll()}>Export</button>
  </div>
  {#if searchMode}
    <div class="bar">
      <input placeholder="Search query…" bind:value={query} />
      <button onclick={search}>Search</button>
    </div>
    <ul>
      {#each results as r}
        <li>{String(r['processId'])}: {String(r['line']).slice(0, 200)}</li>
      {/each}
    </ul>
  {:else}
    <LogView {buffer} bind:this={view} />
  {/if}
</div>

<style>
  .page { padding: var(--space-5); }
  .bar { display: flex; gap: var(--space-3); margin-bottom: var(--space-4); }
  .bar input { flex: 1; background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--radius); padding: var(--space-2) var(--space-3); color: var(--text-0); }
</style>
