<script lang="ts">
  import { onMount } from 'svelte';
  import Bot from '@lucide/svelte/icons/bot';
  import Terminal from '@lucide/svelte/icons/terminal';
  import Monitor from '@lucide/svelte/icons/monitor';
  import Users from '@lucide/svelte/icons/users';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';
  import Plug from '@lucide/svelte/icons/plug';
  import Copy from '@lucide/svelte/icons/copy';
  import CircleAlert from '@lucide/svelte/icons/triangle-alert';
  import { SessionService } from '../../lib/api';
  import type { Session } from '../../lib/api';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { clock } from '../../lib/state/clock.svelte';
  import { dialogs } from '../../lib/state/dialogs.svelte';
  import { router } from '../../lib/router.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import RelativeTime from '../../lib/ui/RelativeTime.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';

  type Status = 'live' | 'idle' | 'closed';
  type StatusFilter = 'all' | Status;

  const LIVE_MS = 30000;

  let sessions = $state<Session[]>([]);
  let loaded = $state(false);
  let refreshing = $state(false);
  let error = $state('');
  let query = $state('');
  let filter = $state<StatusFilter>('all');
  let closing = $state(new Set<string>());

  async function load(manual = false) {
    if (manual) refreshing = true;
    try {
      sessions = await SessionService.list();
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loaded = true;
      refreshing = false;
    }
  }

  onMount(() => {
    void load();
    const t = setInterval(() => void load(), 5000);
    const release = clock.retain();
    return () => {
      clearInterval(t);
      release();
    };
  });

  function statusOf(s: Session): Status {
    if (s.closed) return 'closed';
    return clock.now - s.lastSeen < LIVE_MS ? 'live' : 'idle';
  }

  function nameOf(s: Session): string {
    if (s.harness && s.harness !== 'unknown') return s.harness;
    return s.kind === 'gui' ? 'Desktop app' : s.kind === 'cli' ? 'Command line' : 'Unknown client';
  }

  const scoped = $derived(
    sessions.filter((s) => scopeState.all || !s.workspaceId || s.workspaceId === scopeState.workspaceId)
  );

  const counts = $derived({
    all: scoped.length,
    live: scoped.filter((s) => statusOf(s) === 'live').length,
    idle: scoped.filter((s) => statusOf(s) === 'idle').length,
    closed: scoped.filter((s) => statusOf(s) === 'closed').length
  });

  const needle = $derived(query.trim().toLowerCase());

  const rows = $derived(
    scoped
      .filter((s) => {
        if (filter !== 'all' && statusOf(s) !== filter) return false;
        if (!needle) return true;
        return `${nameOf(s)} ${s.kind} ${s.id} ${s.clientPid} ${scopeState.workspaceLabel(s.workspaceId)}`.toLowerCase().includes(needle);
      })
      .sort((a, b) => b.lastSeen - a.lastSeen)
  );

  async function close(s: Session) {
    const ok = await dialogs.confirm(
      `Close the session from ${nameOf(s)}${s.clientPid ? ` (pid ${s.clientPid})` : ''}? Processes it started with a session lifetime will stop after the grace period.`
    );
    if (!ok) return;
    closing = new Set(closing).add(s.id);
    try {
      await SessionService.close(s.id);
      toasts.ok('Session closed');
      await load();
    } catch (err) {
      toastError(err);
    } finally {
      const next = new Set(closing);
      next.delete(s.id);
      closing = next;
    }
  }

  async function copyId(id: string) {
    try {
      await navigator.clipboard.writeText(id);
      toasts.ok('Session id copied');
    } catch {
      toasts.err('Copy failed');
    }
  }

  function clear() {
    query = '';
    filter = 'all';
  }

  const filtersActive = $derived(needle !== '' || filter !== 'all');
</script>

<div class="page">
  <header class="page-header">
    <div class="titles">
      <h1>Sessions</h1>
      <p class="subtitle">AI agents, CLIs and apps currently connected to the daemon.</p>
    </div>
    <div class="actions">
      <button class="btn" onclick={() => void load(true)} disabled={refreshing} aria-label="Refresh sessions">
        {#if refreshing}<Spinner size={14} />{:else}<RefreshCw size={14} />{/if}Refresh
      </button>
    </div>
  </header>

  <div class="toolbar">
    <div class="chips" role="group" aria-label="Session status">
      <button class="chip" class:active={filter === 'all'} onclick={() => (filter = 'all')}>All <span class="count">{counts.all}</span></button>
      <button class="chip" class:active={filter === 'live'} onclick={() => (filter = 'live')}><span class="dot ok"></span>Live <span class="count">{counts.live}</span></button>
      <button class="chip" class:active={filter === 'idle'} onclick={() => (filter = 'idle')}><span class="dot warn"></span>Idle <span class="count">{counts.idle}</span></button>
      {#if counts.closed > 0}
        <button class="chip" class:active={filter === 'closed'} onclick={() => (filter = 'closed')}><span class="dot"></span>Closed <span class="count">{counts.closed}</span></button>
      {/if}
    </div>
    <div class="input-wrap search">
      <Search size={14} />
      <input type="search" placeholder="Search client, pid, workspace" aria-label="Search sessions" bind:value={query} />
    </div>
    {#if filtersActive}
      <button class="btn ghost sm" onclick={clear}><X size={14} />Clear filters</button>
    {/if}
  </div>

  {#if error}
    <div class="banner err" role="alert"><CircleAlert size={16} /><span>Could not load sessions: {error}</span><button class="btn sm" onclick={() => void load(true)}>Retry</button></div>
  {/if}

  {#if !loaded}
    <div class="table-wrap" aria-busy="true">
      <table class="data">
        <thead><tr><th>Client</th><th>Kind</th><th>PID</th><th>Workspace</th><th>Started</th><th>Last seen</th><th></th></tr></thead>
        <tbody>
          {#each Array(4) as _, i (i)}
            <tr>
              <td><div class="sk"><Skeleton width="30px" height="30px" /><Skeleton width="110px" height="12px" /></div></td>
              <td><Skeleton width="48px" height="18px" /></td>
              <td><Skeleton width="48px" height="12px" /></td>
              <td><Skeleton width="80px" height="12px" /></td>
              <td><Skeleton width="64px" height="12px" /></td>
              <td><Skeleton width="72px" height="12px" /></td>
              <td></td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else if rows.length === 0}
    <div class="card">
      <div class="empty-state">
        <span class="icon-wrap"><Users size={20} /></span>
        {#if filtersActive && scoped.length > 0}
          <h3>No matching sessions</h3>
          <p>Nothing matches the current filters.</p>
          <button class="btn" onclick={clear}><X size={14} />Clear filters</button>
        {:else}
          <h3>No sessions connected</h3>
          <p>A session appears here as soon as an AI agent, the CLI or the desktop app connects to agent-runtime. Install the MCP server in your agent to get started.</p>
          <a class="btn primary" href="#/integrations" use:router.link={'/integrations'}><Plug size={14} />Connect an agent</a>
        {/if}
      </div>
    </div>
  {:else}
    <div class="table-wrap">
      <table class="data">
        <thead>
          <tr>
            <th>Client</th>
            <th>Kind</th>
            <th class="num">PID</th>
            <th>Workspace</th>
            <th>Started</th>
            <th>Last seen</th>
            <th class="act"><span class="sr">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each rows as s (s.id)}
            {@const st = statusOf(s)}
            <tr class:closed={st === 'closed'}>
              <td>
                <div class="client">
                  <span class="cic">
                    {#if s.kind === 'cli'}<Terminal size={16} />{:else if s.kind === 'gui'}<Monitor size={16} />{:else}<Bot size={16} />{/if}
                  </span>
                  <div class="cname">
                    <strong>{nameOf(s)}{#if s.harnessVersion}<span class="ver mono">{s.harnessVersion}</span>{/if}</strong>
                    <button class="idbtn mono" onclick={() => void copyId(s.id)} title="Copy session id" aria-label={`Copy session id ${s.id}`}>
                      {s.id}<Copy size={12} />
                    </button>
                  </div>
                </div>
              </td>
              <td><span class="badge {s.kind === 'mcp' ? 'accent' : s.kind === 'cli' ? 'info' : ''}">{s.kind || 'unknown'}</span></td>
              <td class="num mono">{s.clientPid || '—'}</td>
              <td>
                {#if s.workspaceId}
                  <span title={scopeState.workspacePath(s.workspaceId)}>{scopeState.workspaceLabel(s.workspaceId)}</span>
                {:else}
                  <span class="muted">Global</span>
                {/if}
              </td>
              <td><RelativeTime ts={s.started} /></td>
              <td>
                <span class="seen">
                  <span class="dot" class:ok={st === 'live'} class:warn={st === 'idle'}></span>
                  <RelativeTime ts={s.lastSeen} />
                  <span class="state {st}">{st}</span>
                </span>
              </td>
              <td class="act">
                {#if st !== 'closed'}
                  <button class="btn danger sm" onclick={() => void close(s)} disabled={closing.has(s.id)} aria-label={`Close session ${s.id}`}>
                    {#if closing.has(s.id)}<Spinner size={12} />{:else}<X size={14} />{/if}Close
                  </button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
  .toolbar :global(.btn),
  .toolbar :global(.chip),
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
  .client {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }
  .cic {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border-radius: 9px;
    background: var(--bg-3);
    color: var(--text-1);
    flex: none;
  }
  .cname {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .cname strong {
    text-transform: capitalize;
    font-weight: 600;
  }
  .ver {
    margin-left: 8px;
    font-size: var(--fs-micro);
    font-weight: 400;
    color: var(--text-2);
    text-transform: none;
  }
  .idbtn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    border: none;
    background: transparent;
    padding: 0;
    font-size: var(--fs-micro);
    color: var(--text-2);
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    border-radius: 4px;
  }
  .idbtn:hover {
    background: transparent;
    color: var(--text-0);
  }
  tr.closed td {
    opacity: 0.55;
  }
  .seen {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    white-space: nowrap;
  }
  .state {
    font-size: var(--fs-micro);
    
    letter-spacing: 0;
    font-weight: 600;
    color: var(--text-2);
  }
  .state.live {
    color: var(--ok);
  }
  .state.idle {
    color: var(--warn);
  }
  th.act,
  td.act {
    text-align: right;
    width: 1%;
    white-space: nowrap;
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .sk {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
</style>
