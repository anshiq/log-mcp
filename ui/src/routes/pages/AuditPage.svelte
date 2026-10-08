<script lang="ts">
  import { onMount } from 'svelte';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';
  import ShieldCheck from '@lucide/svelte/icons/shield-check';
  import ArrowDownToLine from '@lucide/svelte/icons/arrow-down-to-line';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Eye from '@lucide/svelte/icons/eye';
  import { AuditService } from '../../lib/api';
  import type { AuditEntry } from '../../lib/api';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { clock } from '../../lib/state/clock.svelte';
  import { duration } from '../../lib/format';
  import { clock as clockTime } from '../../lib/activity';
  import RelativeTime from '../../lib/ui/RelativeTime.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';

  const PAGE = 100;
  const MAX = 500;
  const READ_ONLY = /^(Get|List|Watch|Ping|Heartbeat|Health|Validate|Plan|Search|Count|Read)/;

  let entries = $state<AuditEntry[]>([]);
  let limit = $state(PAGE);
  let loaded = $state(false);
  let refreshing = $state(false);
  let loadingMore = $state(false);
  let error = $state('');
  let query = $state('');
  let result = $state<'all' | 'ok' | 'error'>('all');
  let harness = $state('');
  let changesOnly = $state(false);

  async function load(mode: 'initial' | 'refresh' | 'more' = 'refresh') {
    if (mode === 'refresh') refreshing = true;
    if (mode === 'more') loadingMore = true;
    try {
      entries = await AuditService.list('', limit);
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loaded = true;
      refreshing = false;
      loadingMore = false;
    }
  }

  onMount(() => {
    void load('initial');
    return clock.retain();
  });

  function loadMore() {
    limit = Math.min(MAX, limit + PAGE);
    void load('more');
  }

  function parts(action: string): { service: string; method: string } {
    const m = action.match(/^\/?(?:[\w.]+\.)?(\w+)\/(\w+)$/);
    if (m) return { service: (m[1] ?? '').replace(/Service$/, ''), method: m[2] ?? action };
    return { service: '', method: action };
  }

  function isOk(r: string): boolean {
    return r === 'ok' || r === '';
  }

  function resultDetail(r: string): string {
    const i = r.indexOf(':');
    return i >= 0 ? r.slice(i + 1).trim() : '';
  }

  const scoped = $derived(entries.filter((e) => scopeState.all || !e.workspaceId || e.workspaceId === scopeState.workspaceId));
  const harnesses = $derived([...new Set(scoped.map((e) => e.harness).filter(Boolean))].sort());
  const needle = $derived(query.trim().toLowerCase());

  const base = $derived(
    scoped.filter((e) => {
      if (harness === '__none' ? e.harness !== '' : harness && e.harness !== harness) return false;
      if (changesOnly && READ_ONLY.test(parts(e.action).method)) return false;
      if (!needle) return true;
      return `${e.action} ${e.harness} ${e.session} ${e.result}`.toLowerCase().includes(needle);
    })
  );

  const okCount = $derived(base.filter((e) => isOk(e.result)).length);
  const errCount = $derived(base.length - okCount);
  const rows = $derived(base.filter((e) => (result === 'all' ? true : result === 'ok' ? isOk(e.result) : !isOk(e.result))));
  const filtersActive = $derived(needle !== '' || result !== 'all' || harness !== '' || changesOnly);
  const canLoadMore = $derived(entries.length >= limit && limit < MAX);
  const atCeiling = $derived(entries.length >= MAX);

  function clear() {
    query = '';
    result = 'all';
    harness = '';
    changesOnly = false;
  }
</script>

<div class="page">
  <header class="page-header">
    <div class="titles">
      <h1>Audit log</h1>
      <p class="subtitle">Every call made to the daemon by agents, the CLI and this app, newest first.</p>
    </div>
    <div class="actions">
      <button class="btn" onclick={() => void load('refresh')} disabled={refreshing} aria-label="Refresh audit log">
        {#if refreshing}<Spinner size={14} />{:else}<RefreshCw size={14} />{/if}Refresh
      </button>
    </div>
  </header>

  <div class="toolbar">
    <div class="input-wrap search">
      <Search size={14} />
      <input type="search" placeholder="Search action, client, session" aria-label="Search audit actions" bind:value={query} />
    </div>
    <div class="chips" role="group" aria-label="Result filter">
      <button class="chip" class:active={result === 'all'} onclick={() => (result = 'all')}>All <span class="count">{base.length}</span></button>
      <button class="chip" class:active={result === 'ok'} onclick={() => (result = 'ok')}><span class="dot ok"></span>OK <span class="count">{okCount}</span></button>
      <button class="chip" class:active={result === 'error'} onclick={() => (result = 'error')}><span class="dot err"></span>Errors <span class="count">{errCount}</span></button>
    </div>
    <select bind:value={harness} aria-label="Client filter">
      <option value="">All clients</option>
      {#each harnesses as h (h)}<option value={h}>{h}</option>{/each}
      <option value="__none">Unidentified</option>
    </select>
    <button class="chip" class:active={changesOnly} onclick={() => (changesOnly = !changesOnly)} aria-pressed={changesOnly} title="Hide Get, List and Watch calls">
      <Eye size={14} />Changes only
    </button>
    <span class="grow"></span>
    {#if filtersActive}
      <button class="btn ghost sm" onclick={clear}><X size={14} />Clear filters</button>
    {/if}
  </div>

  {#if error}
    <div class="banner err" role="alert"><TriangleAlert size={16} /><span>Could not load the audit log: {error}</span><button class="btn sm" onclick={() => void load('refresh')}>Retry</button></div>
  {/if}

  {#if !loaded}
    <div class="table-wrap" aria-busy="true">
      <table class="data">
        <thead><tr><th>Time</th><th>Action</th><th>Result</th><th class="num">Duration</th><th>Client</th><th>Workspace</th></tr></thead>
        <tbody>
          {#each Array(8) as _, i (i)}
            <tr>
              <td><Skeleton width="70px" height="12px" /></td>
              <td><Skeleton width={`${110 + ((i * 29) % 70)}px`} height="12px" /></td>
              <td><Skeleton width="40px" height="18px" /></td>
              <td class="num"><Skeleton width="36px" height="12px" /></td>
              <td><Skeleton width="72px" height="12px" /></td>
              <td><Skeleton width="60px" height="12px" /></td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else if rows.length === 0}
    <div class="card">
      <div class="empty-state">
        <span class="icon-wrap"><ShieldCheck size={20} /></span>
        {#if filtersActive && scoped.length > 0}
          <h3>No matching entries</h3>
          <p>Nothing in the loaded audit history matches these filters.</p>
          <button class="btn" onclick={clear}><X size={14} />Clear filters</button>
        {:else}
          <h3>No audit entries yet</h3>
          <p>Each request handled by the daemon is recorded here with who made it, how long it took and whether it succeeded.</p>
        {/if}
      </div>
    </div>
  {:else}
    <div class="table-wrap">
      <table class="data">
        <thead>
          <tr>
            <th>Time</th>
            <th>Action</th>
            <th>Result</th>
            <th class="num">Duration</th>
            <th>Client</th>
            <th>Workspace</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as e (e.id)}
            {@const a = parts(e.action)}
            {@const ok = isOk(e.result)}
            <tr class:bad={!ok}>
              <td class="when"><RelativeTime ts={e.time} /><span class="clk mono">{clockTime(e.time)}</span></td>
              <td>
                <div class="action" title={e.action}>
                  <span class="method mono">{a.method}</span>
                  {#if a.service}<span class="service">{a.service}</span>{/if}
                </div>
              </td>
              <td>
                {#if ok}
                  <span class="badge ok">ok</span>
                {:else}
                  <span class="badge err" title={e.result}>error{resultDetail(e.result) ? ` · ${resultDetail(e.result)}` : ''}</span>
                {/if}
              </td>
              <td class="num" class:slow={e.duration >= 1000}>{duration(e.duration)}</td>
              <td>
                {#if e.harness || e.session}
                  <div class="who">
                    <span>{e.harness || 'session'}</span>
                    {#if e.session}<span class="sid mono" title={e.session}>{e.session}</span>{/if}
                  </div>
                {:else}
                  <span class="muted">—</span>
                {/if}
              </td>
              <td>
                {#if e.workspaceId}
                  <span title={scopeState.workspacePath(e.workspaceId)}>{scopeState.workspaceLabel(e.workspaceId)}</span>
                {:else}
                  <span class="muted">—</span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <div class="foot">
      <span class="muted">Showing {rows.length} of {entries.length} loaded {entries.length === 1 ? 'entry' : 'entries'}</span>
      {#if canLoadMore}
        <button class="btn" onclick={loadMore} disabled={loadingMore}>
          {#if loadingMore}<Spinner size={14} />Loading{:else}<ArrowDownToLine size={14} />Load {Math.min(PAGE, MAX - limit)} more{/if}
        </button>
      {:else if atCeiling}
        <span class="muted">Showing the latest {MAX} entries</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .toolbar :global(.btn),
  .toolbar :global(.chip),
  .toolbar select,
  .search input {
    height: 32px;
    box-sizing: border-box;
  }
  .search {
    width: 280px;
    max-width: 100%;
  }
  .search input {
    width: 100%;
  }
  .table-wrap {
    overflow-x: auto;
  }
  .when {
    white-space: nowrap;
    color: var(--text-1);
  }
  .clk {
    margin-left: 10px;
    font-size: var(--fs-micro);
    color: var(--text-2);
  }
  .action {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
  }
  .method {
    font-size: var(--fs-sm);
    color: var(--text-0);
  }
  .service {
    font-size: var(--fs-micro);
    color: var(--text-2);
    padding: 0 6px;
    border: 1px solid var(--border);
    border-radius: 999px;
    white-space: nowrap;
  }
  tr.bad td {
    background: color-mix(in srgb, var(--err) 5%, transparent);
  }
  .slow {
    color: var(--warn);
  }
  .who {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .who > span:first-child {
    text-transform: capitalize;
  }
  .sid {
    font-size: var(--fs-micro);
    color: var(--text-2);
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    font-size: var(--fs-sm);
    flex-wrap: wrap;
  }
</style>
