<script lang="ts">
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';
  import Plus from '@lucide/svelte/icons/plus';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import Square from '@lucide/svelte/icons/square';
  import Zap from '@lucide/svelte/icons/zap';
  import Trash from '@lucide/svelte/icons/trash';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import ProcessTable from '../../components/ProcessTable.svelte';
  import ProcessDetail, { type DetailTab } from '../../components/ProcessDetail.svelte';
  import ProcessConfirm from '../../components/ProcessConfirm.svelte';
  import StartProcessDialog from '../../components/StartProcessDialog.svelte';
  import ActionMenu from '../../components/ActionMenu.svelte';
  import { bulkSignalMenu } from '../../components/processMenu';
  import { procActions } from '../../components/processActions.svelte';
  import { matchesQuery, matchesStatus, isRunning, type StatusFilter } from '../../components/processView';
  import { processes } from '../../lib/state/processes.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { router } from '../../lib/router.svelte';
  import { toasts } from '../../lib/toasts.svelte';
  import { getPlatform } from '../../lib/platform';

  let query = $state('');
  let statusFilter = $state<StatusFilter>('all');
  let selected = $state(new Set<string>());
  let detailTab = $state<DetailTab>('logs');
  let showStart = $state(false);
  let stageWidth = $state(1200);
  let searchInput: HTMLInputElement | null = $state(null);

  const openId = $derived(router.match('/processes/:id')?.params['id'] ?? '');
  const overlay = $derived(stageWidth < 900);
  const scoped = $derived(processes.scoped);
  const counts = $derived({
    all: scoped.length,
    running: scoped.filter((p) => matchesStatus(p, 'running')).length,
    starting: scoped.filter((p) => matchesStatus(p, 'starting')).length,
    failed: scoped.filter((p) => matchesStatus(p, 'failed')).length,
    exited: scoped.filter((p) => matchesStatus(p, 'exited')).length
  });
  const rows = $derived(
    scoped.filter((p) => matchesStatus(p, statusFilter) && matchesQuery(p, query, scopeState.workspaceLabel(p.workspaceId)))
  );
  const loading = $derived(processes.streamState === 'connecting' && processes.map.size === 0);
  const selectedIds = $derived([...selected].filter((id) => processes.map.has(id)));
  const selectedProcs = $derived(selectedIds.map((id) => processes.map.get(id)!));
  const filtersActive = $derived(query.trim() !== '' || statusFilter !== 'all');
  const openProcess = $derived(processes.map.get(openId));

  const chips = $derived([
    { id: 'all' as StatusFilter, label: 'All', count: counts.all },
    { id: 'running' as StatusFilter, label: 'Running', count: counts.running },
    ...(counts.starting > 0 || statusFilter === 'starting'
      ? [{ id: 'starting' as StatusFilter, label: 'Starting', count: counts.starting }]
      : []),
    { id: 'failed' as StatusFilter, label: 'Failed', count: counts.failed },
    { id: 'exited' as StatusFilter, label: 'Exited', count: counts.exited }
  ]);

  const subtitle = $derived.by(() => {
    const where = scopeState.all ? 'all workspaces' : scopeState.label;
    if (counts.all === 0) return `Nothing running in ${where}`;
    const parts = [`${counts.running} running`];
    if (counts.failed > 0) parts.push(`${counts.failed} failed`);
    parts.push(`${counts.all} total`);
    return `${parts.join(' · ')} in ${where}`;
  });

  $effect(() => {
    void scopeState.workspaceId;
    selected = new Set();
  });

  $effect(() => {
    if (selected.size > 0 && selectedIds.length !== selected.size) selected = new Set(selectedIds);
  });

  function openDetail(id: string) {
    void router.navigate(`/processes/${id}`);
  }

  function openLogs(id: string) {
    detailTab = 'logs';
    openDetail(id);
  }

  function closeDetail() {
    void router.navigate('/processes');
  }

  async function copyId(id: string) {
    try {
      await getPlatform().copyText(id);
      toasts.ok('Process id copied');
    } catch {
      toasts.err('Could not copy to the clipboard');
    }
  }

  function afterRemove(ids: string[]) {
    if (ids.length === 0) return;
    const next = new Set(selected);
    for (const id of ids) next.delete(id);
    selected = next;
    if (openId && ids.includes(openId)) closeDetail();
  }

  function clearFilters() {
    query = '';
    statusFilter = 'all';
  }

  async function bulk(action: 'restart' | 'stop' | 'remove') {
    const ids = [...selectedIds];
    if (action === 'restart') await procActions.restart(ids);
    else if (action === 'stop') await procActions.stop(ids.filter((id) => isRunning(processes.map.get(id)!) || processes.map.get(id)?.status === 'starting'));
    else afterRemove(await procActions.remove(ids));
  }

  function started(processId: string) {
    showStart = false;
    toasts.ok('Process started');
    detailTab = 'logs';
    openDetail(processId);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key !== 'Escape' || e.defaultPrevented) return;
    const target = e.target as HTMLElement | null;
    if (target?.closest('.xterm')) return;
    if (target === searchInput && query) {
      query = '';
      return;
    }
    if (selected.size > 0 && !openId) {
      selected = new Set();
      return;
    }
    if (openId) closeDetail();
  }

  const signalItems = $derived(bulkSignalMenu(() => selectedIds));
  const stoppable = $derived(selectedProcs.some((p) => p.status === 'running' || p.status === 'ready' || p.status === 'starting'));
</script>

<svelte:window onkeydown={onKey} />

<div class="page fill procs-page">
  <div class="page-header">
    <div class="titles">
      <h1>Processes</h1>
      <span class="subtitle">{subtitle}</span>
    </div>
    <div class="actions">
      <button class="btn primary" onclick={() => (showStart = true)}><Plus size={14} /> Start process</button>
    </div>
  </div>

  <div class="toolbar">
    <div class="input-wrap search-box">
      <Search size={14} />
      <input
        bind:this={searchInput}
        bind:value={query}
        class="search"
        type="text"
        placeholder="Filter by name, command, pid or port"
        aria-label="Filter processes"
        autocomplete="off"
        spellcheck="false"
      />
      {#if query}
        <button class="clear" type="button" aria-label="Clear search" onclick={() => { query = ''; searchInput?.focus(); }}>
          <X size={13} />
        </button>
      {:else}
        <span class="kbd slash">/</span>
      {/if}
    </div>
    <div class="chips" role="group" aria-label="Filter by status">
      {#each chips as c (c.id)}
        <button
          type="button"
          class="chip"
          class:active={statusFilter === c.id}
          aria-pressed={statusFilter === c.id}
          onclick={() => (statusFilter = c.id)}
        >
          {c.label} <span class="count">{c.count}</span>
        </button>
      {/each}
    </div>
    {#if selectedIds.length === 0 && filtersActive && !loading}
      <span class="result muted">{rows.length} of {counts.all} shown</span>
    {/if}
    {#if selectedIds.length > 0}
      <div class="bulk" role="toolbar" aria-label="Bulk actions">
        <span class="sel"><strong>{selectedIds.length}</strong> selected</span>
        <span class="divider"></span>
        <button class="btn sm" onclick={() => void bulk('restart')}><RotateCw size={13} /> Restart</button>
        <button class="btn sm" disabled={!stoppable} onclick={() => void bulk('stop')}><Square size={12} /> Stop</button>
        <ActionMenu items={signalItems} label="Send signal to selection" triggerClass="btn sm" align="start">
          <Zap size={13} /> Signal <ChevronDown size={12} />
        </ActionMenu>
        <button class="btn sm danger" onclick={() => void bulk('remove')}><Trash size={13} /> Remove</button>
        <span class="divider"></span>
        <button class="btn sm ghost icon" aria-label="Clear selection" title="Clear selection" onclick={() => (selected = new Set())}>
          <X size={14} />
        </button>
      </div>
    {/if}
  </div>

  <div class="stage" class:overlay bind:clientWidth={stageWidth}>
    <div class="main">
      <ProcessTable
        {rows}
        totalCount={counts.all}
        {loading}
        bind:selected
        {openId}
        showWorkspace={scopeState.all}
        workspaceLabel={(id) => scopeState.workspaceLabel(id)}
        onOpen={openDetail}
        onLogs={openLogs}
        onCopyId={copyId}
        onRemoved={afterRemove}
        onStart={() => (showStart = true)}
        onClearFilters={clearFilters}
      />
    </div>

    {#if openId}
      {#if overlay}
        <button class="scrim" type="button" tabindex="-1" aria-label="Close details" onclick={closeDetail}></button>
      {/if}
      <aside class="drawer" class:overlay aria-label="Process details">
        {#key openId}
          <ProcessDetail
            processId={openId}
            process={openProcess}
            bind:tab={detailTab}
            onClose={closeDetail}
            onCopyId={copyId}
            onRemoved={afterRemove}
          />
        {/key}
      </aside>
    {/if}
  </div>
</div>

<ProcessConfirm />

{#if showStart}
  <StartProcessDialog workspaceId={scopeState.workspaceId} onClose={() => (showStart = false)} onStarted={started} />
{/if}

<style>
  .procs-page {
    gap: var(--space-4);
    padding-bottom: var(--space-6);
  }
  .procs-page .page-header {
    align-items: center;
  }
  .search-box {
    width: 320px;
    max-width: 100%;
    flex: 0 1 320px;
  }
  .search-box input {
    padding-right: 34px;
  }
  .search-box .slash {
    position: absolute;
    right: 9px;
    pointer-events: none;
    color: var(--text-2);
  }
  .search-box .clear {
    position: absolute;
    right: 5px;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    padding: 0;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: var(--text-2);
  }
  .search-box .clear:hover {
    background: var(--bg-3);
    color: var(--text-0);
  }
  .result {
    font-size: var(--fs-xs);
    margin-left: auto;
  }
  .stage {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: var(--space-4);
    position: relative;
  }
  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    position: relative;
    container: procs / inline-size;
  }
  .drawer {
    flex: none;
    width: clamp(460px, 44%, 620px);
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
    animation: drawer-in 200ms var(--ease);
  }
  .drawer.overlay {
    position: absolute;
    z-index: 40;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(600px, 100%);
    box-shadow: var(--shadow-pop);
    border-color: var(--border-strong);
  }
  .scrim {
    position: absolute;
    inset: 0;
    z-index: 39;
    border: none;
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--bg-0) 62%, transparent);
    backdrop-filter: blur(1.5px);
    animation: fade-in 160ms var(--ease);
    cursor: default;
  }
  .scrim:hover {
    background: color-mix(in srgb, var(--bg-0) 62%, transparent);
  }
  .bulk {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 3px 6px 3px 10px;
    background: var(--accent-subtle);
    border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
    border-radius: var(--radius);
    animation: bulk-in 160ms var(--ease);
    white-space: nowrap;
  }
  .bulk .sel {
    font-size: var(--fs-sm);
    color: var(--text-1);
    padding: 0 var(--space-2);
  }
  .bulk .sel strong {
    color: var(--text-0);
  }
  .divider {
    width: 1px;
    height: 18px;
    background: var(--border);
  }
  @keyframes drawer-in {
    from {
      opacity: 0;
      transform: translateX(22px);
    }
  }
  @keyframes fade-in {
    from {
      opacity: 0;
    }
  }
  @keyframes bulk-in {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
  @media (max-width: 900px) {
    .search-box {
      flex: 1 1 100%;
      width: auto;
    }
  }
</style>
