<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import FilterX from '@lucide/svelte/icons/filter-x';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Play from '@lucide/svelte/icons/play';
  import History from '@lucide/svelte/icons/history';
  import { LogService } from '../../lib/api/services';
  import { buildMatcher, stripAnsi } from '../../lib/ansi';
  import { buildLabels } from '../../lib/logproc';
  import { searchLogs, type SearchHit } from '../../lib/logsearch';
  import { getPlatform } from '../../lib/platform';
  import { router } from '../../lib/router.svelte';
  import { toastError, toasts } from '../../lib/toasts.svelte';
  import { LogSession } from '../../lib/state/logs.svelte';
  import { LEVEL_RANK, LogsUi, parseSelection, selectionPath } from '../../lib/state/logsui.svelte';
  import { prefs } from '../../lib/state/prefs.svelte';
  import { processes } from '../../lib/state/processes.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import type { Process } from '../../lib/api/types';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import LogPane from '../../features/logs/LogPane.svelte';
  import LogToolbar from '../../features/logs/LogToolbar.svelte';
  import SearchResults from '../../features/logs/SearchResults.svelte';

  const ui = new LogsUi();
  const session = new LogSession({ max: prefs.data.maxBufferLines });

  let follow = $state(true);
  let pane = $state<LogPane | null>(null);
  let toolbar = $state<{ focusFilter: () => void } | null>(null);
  let debounced = $state('');
  let downBanner = $state(false);
  let hits = $state<SearchHit[]>([]);
  let searching = $state(false);
  let searched = $state(false);
  let searchError = $state('');
  let searchTruncated = $state(false);
  let searchedQuery = $state('');
  let searchedRe = $state<RegExp | null>(null);
  let searchToken = 0;
  let lastScope = scopeState.workspaceId;

  const labels = $derived(buildLabels(processes.list));
  const liveStatus = (s: string) => s === 'running' || s === 'ready' || s === 'starting';
  const runningList = $derived(processes.scoped.filter((p) => liveStatus(p.status)));
  const pickerList = $derived.by(() => {
    const scoped = processes.scoped;
    const have = new Set(scoped.map((p) => p.id));
    const extra: Process[] = [];
    for (const id of ui.selected) {
      const p = processes.map.get(id);
      if (p && !have.has(id)) extra.push(p);
    }
    return [...extra, ...scoped];
  });
  const targetIds = $derived(ui.selected.length > 0 ? ui.selected : runningList.map((p) => p.id));
  const targetProcs = $derived(targetIds.map((id) => processes.map.get(id)).filter((p): p is Process => !!p));
  const groups = $derived.by(() => {
    const m = new Map<string, string[]>();
    for (const p of targetProcs) {
      const list = m.get(p.workspaceId);
      if (list) list.push(p.id);
      else m.set(p.workspaceId, [p.id]);
    }
    return m;
  });
  const groupsKey = $derived([...groups].map(([w, ids]) => `${w}:${[...ids].sort().join(',')}`).sort().join('|'));
  const matcher = $derived(buildMatcher(debounced, ui.regex, ui.caseSensitive));
  const liveMatcher = $derived(ui.mode === 'live' ? matcher : null);
  const matcherError = $derived(matcher.error);
  const buffered = $derived(Math.max(0, session.liveCount - session.viewCount));
  const noFilterHit = $derived(session.total > 0 && session.viewCount === 0);

  const status = $derived.by(() => {
    if (ui.mode === 'history') return { tone: 'idle', label: 'History', hint: 'Searching stored logs' };
    if (targetProcs.length === 0) return { tone: 'idle', label: 'Idle', hint: 'No processes to tail' };
    if (session.paused) return { tone: 'warn', label: 'Paused', hint: 'View frozen, new lines are still buffered' };
    if (session.conn === 'live') return { tone: 'ok', label: 'Live', hint: 'Streaming' };
    if (session.conn === 'reconnecting') return { tone: 'warn', label: 'Reconnecting', hint: 'Trying to restore the stream' };
    if (session.conn === 'connecting') return { tone: 'busy', label: 'Connecting', hint: 'Opening the stream' };
    return { tone: 'idle', label: 'Disconnected', hint: 'Stream closed' };
  });

  function sameIds(a: string[], b: string[]): boolean {
    return a.length === b.length && a.every((x, i) => x === b[i]);
  }

  function setSelected(ids: string[]) {
    ui.selected = ids;
    void router.navigate(selectionPath(ids), { replace: true });
  }

  function toggleSelected(id: string) {
    ui.toggleSelected(id);
    void router.navigate(selectionPath(ui.selected), { replace: true });
  }

  function selectRunning() {
    setSelected(runningList.map((p) => p.id));
  }

  function searchGroups(): Map<string, string[]> {
    const m = new Map<string, string[]>();
    const pool = ui.selected.length > 0 ? ui.selected.map((id) => processes.map.get(id)).filter((p): p is Process => !!p) : processes.scoped;
    for (const p of pool) {
      const list = m.get(p.workspaceId);
      if (list) list.push(p.id);
      else m.set(p.workspaceId, [p.id]);
    }
    return m;
  }

  async function runSearch() {
    const q = ui.query.trim();
    if (!q) return;
    const g = searchGroups();
    const token = ++searchToken;
    searching = true;
    searchError = '';
    if (g.size === 0) {
      hits = [];
      searched = false;
      searching = false;
      searchError = 'There are no processes in this scope to search.';
      return;
    }
    const m = buildMatcher(q, ui.regex, ui.caseSensitive);
    if (m.error) {
      searching = false;
      searchError = `Invalid regular expression: ${m.error}`;
      return;
    }
    try {
      const out = await searchLogs({
        groups: g,
        query: q,
        regex: ui.regex,
        caseSensitive: ui.caseSensitive,
        minLevel: ui.level,
        streams: ui.stdout && ui.stderr ? null : [ui.stdout ? 'stdout' : '', ui.stderr ? 'stderr' : ''].filter(Boolean),
        maxRows: 1000
      });
      if (token !== searchToken) return;
      hits = out.hits;
      searchTruncated = out.truncated;
      searchedQuery = q;
      searchedRe = m.re;
      searched = true;
    } catch (e) {
      if (token !== searchToken) return;
      searchError = e instanceof Error ? e.message : String(e);
    } finally {
      if (token === searchToken) searching = false;
    }
  }

  function openLive(proc: string) {
    ui.mode = 'live';
    setSelected([proc]);
  }

  function stamp(): string {
    const d = new Date();
    const p = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`;
  }

  function viewText(): string {
    return session
      .viewLines()
      .map((l) => `${new Date(l.ts).toISOString()} ${(labels.get(l.proc) ?? l.proc).padEnd(10)} ${l.stream.padEnd(6)} ${stripAnsi(l.text)}`)
      .join('\n');
  }

  async function exportView() {
    const text = viewText();
    if (!text) {
      toasts.info('Nothing to export in the current view');
      return;
    }
    try {
      await getPlatform().saveFile(`logs-${stamp()}.log`, text + '\n', 'text/plain');
    } catch (e) {
      toastError(e);
    }
  }

  async function copyView() {
    const text = viewText();
    if (!text) {
      toasts.info('Nothing to copy in the current view');
      return;
    }
    try {
      await getPlatform().copyText(text);
      toasts.ok(`Copied ${session.viewCount.toLocaleString()} lines`);
    } catch (e) {
      toastError(e);
    }
  }

  function exportProcess(id: string) {
    let data = '';
    const name = labels.get(id) ?? id;
    toasts.info(`Exporting ${name}`);
    const h = LogService.exportLogs(id, 'txt', (msg) => {
      if (msg.kind === 'chunk' && typeof msg['data'] === 'string') data += msg['data'] as string;
      if (msg.kind === 'done') {
        h.close();
        getPlatform()
          .saveFile(`${name}.log`, data, 'text/plain')
          .catch((e: unknown) => toastError(e));
      }
    });
  }

  function clearView() {
    session.clear();
    follow = true;
  }

  function clearFilters() {
    ui.resetFilters();
    debounced = '';
  }

  function onWindowKey(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'f') {
      e.preventDefault();
      toolbar?.focusFilter();
      return;
    }
    const t = e.target as HTMLElement | null;
    const typing = !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable);
    if (e.key === '/' && !typing && !e.metaKey && !e.ctrlKey) {
      e.preventDefault();
      toolbar?.focusFilter();
    }
  }

  $effect(() => {
    const v = ui.query;
    const t = setTimeout(() => (debounced = v), v === '' ? 0 : 140);
    return () => clearTimeout(t);
  });

  $effect(() => {
    const crit = {
      procs: ui.selected.length > 0 ? new Set(ui.selected) : null,
      streams: ui.streams,
      minRank: LEVEL_RANK[ui.level],
      matcher: liveMatcher
    };
    untrack(() => session.setCriteria(crit));
  });

  $effect(() => {
    void groupsKey;
    const ws = scopeState.workspaceId;
    untrack(() => {
      if (ws !== lastScope) {
        lastScope = ws;
        session.reset();
        follow = true;
        if (ui.selected.length > 0) setSelected([]);
      }
      session.setTargets(groups, prefs.data.defaultBacklog);
    });
  });

  $effect(() => {
    const ids = parseSelection(router.path);
    untrack(() => {
      if (!sameIds(ids, ui.selected)) ui.selected = ids;
    });
  });

  $effect(() => {
    void ui.level;
    void ui.stdout;
    void ui.stderr;
    void ui.regex;
    void ui.caseSensitive;
    void ui.selected;
    untrack(() => {
      if (ui.mode === 'history' && searched) void runSearch();
    });
  });

  $effect(() => {
    if (ui.mode === 'history' && !searched && !searching && ui.query.trim()) untrack(() => void runSearch());
  });

  $effect(() => {
    if (session.conn !== 'reconnecting') {
      downBanner = false;
      return;
    }
    const t = setTimeout(() => (downBanner = true), 2500);
    return () => clearTimeout(t);
  });

  onMount(() => () => session.dispose());
</script>

<svelte:window onkeydown={onWindowKey} />

<div class="page fill logs">
  <div class="page-header">
    <div class="titles">
      <h1>Logs</h1>
      <p class="subtitle">
        {#if ui.mode === 'history'}
          Search retained output across {ui.selected.length > 0 ? `${ui.selected.length} selected` : 'every process'} in {scopeState.label}
        {:else if targetProcs.length > 0}
          Tailing {targetProcs.length} {targetProcs.length === 1 ? 'process' : 'processes'} in {scopeState.label}
        {:else}
          {scopeState.label}
        {/if}
      </p>
    </div>
    <div class="actions">
      <span class="pill {status.tone}" title={status.hint} role="status">
        <span class="dot {status.tone === 'ok' ? 'ok' : status.tone === 'busy' ? 'busy' : status.tone === 'warn' ? 'warn' : ''}"></span>
        {status.label}
      </span>
    </div>
  </div>

  <LogToolbar
    bind:this={toolbar}
    {ui}
    {session}
    list={pickerList}
    {labels}
    runningCount={runningList.length}
    {matcherError}
    {buffered}
    {searching}
    exportTargets={targetProcs.slice(0, 6)}
    onSearch={() => void runSearch()}
    onClear={clearView}
    onExportView={() => void exportView()}
    onCopyView={() => void copyView()}
    onExportProcess={exportProcess}
    onToggleProcess={toggleSelected}
    onSelectRunning={selectRunning}
    onClearSelection={() => setSelected([])}
  />

  <div class="card view">
    {#if downBanner && ui.mode === 'live'}
      <div class="banner err strip" role="alert">
        <TriangleAlert size={16} />
        <span class="grow">Lost the log stream. Retrying automatically, lines already received are kept.</span>
        <button type="button" class="btn sm" onclick={() => session.retry()}><RefreshCw size={13} />Retry now</button>
      </div>
    {/if}

    {#if ui.mode === 'live'}
      <LogPane
        bind:this={pane}
        {session}
        bind:follow
        wrap={prefs.data.wrap}
        fontSize={prefs.data.logFontSize}
        compact={prefs.data.density === 'compact'}
        tsFormat={prefs.data.timestamps}
        ansi={prefs.data.ansi}
        showTags={ui.view.tags}
        showLevels={ui.view.levels}
        showLineNumbers={ui.view.lineNumbers}
        zebra={ui.view.zebra}
        matcher={liveMatcher}
        {labels}
        onTag={(proc) => setSelected([proc])}
      >
        {#snippet empty()}
          {#if targetProcs.length === 0}
            <div class="empty-state">
              <div class="icon-wrap"><ScrollText size={22} /></div>
              <h3>No running processes to tail</h3>
              <p>
                {#if ui.selected.length > 0}
                  The selected processes are not available in this scope.
                {:else}
                  Start a process to stream its output here, or pick an exited process to read what it left behind.
                {/if}
              </p>
              <div class="cta">
                <button type="button" class="btn primary" onclick={() => void router.navigate('/processes')}><Play size={14} />Start a process</button>
                <button type="button" class="btn" onclick={() => (ui.mode = 'history')}><History size={14} />Search history</button>
              </div>
            </div>
          {:else if noFilterHit}
            <div class="empty-state">
              <div class="icon-wrap"><FilterX size={22} /></div>
              <h3>No lines match</h3>
              <p>{session.total.toLocaleString()} lines are buffered but none pass the current filters.</p>
              <button type="button" class="btn" onclick={clearFilters}>Clear filters</button>
            </div>
          {:else}
            <div class="empty-state">
              <div class="icon-wrap">
                {#if session.conn === 'connecting' || session.conn === 'reconnecting'}<Spinner size={20} />{:else}<ScrollText size={22} />{/if}
              </div>
              <h3>{session.conn === 'connecting' || session.conn === 'reconnecting' ? 'Connecting to the log stream' : 'Waiting for output'}</h3>
              <p>New lines from {targetProcs.length} {targetProcs.length === 1 ? 'process' : 'processes'} appear here as soon as they are written.</p>
            </div>
          {/if}
        {/snippet}
      </LogPane>
    {:else}
      <SearchResults
        {hits}
        loading={searching}
        error={searchError}
        {searched}
        truncated={searchTruncated}
        query={searchedQuery || ui.query}
        re={searchedRe}
        {labels}
        onRetry={() => void runSearch()}
        onOpenLive={openLive}
      />
    {/if}

    <div class="foot">
      {#if ui.mode === 'live'}
        <span class="count"><b>{session.total.toLocaleString()}</b> lines</span>
        <span class="sep">·</span>
        <span class="count"><b>{session.viewCount.toLocaleString()}</b> shown</span>
        {#if session.total >= prefs.data.maxBufferLines}
          <span class="sep">·</span>
          <span class="muted">buffer capped at {prefs.data.maxBufferLines.toLocaleString()}</span>
        {/if}
        {#if session.gaps > 0}
          <span class="sep">·</span>
          <span class="warnText">some lines were dropped by the daemon</span>
        {/if}
        <span class="grow"></span>
        {#if session.paused}
          <span class="mode warn">Paused{buffered > 0 ? ` · ${buffered.toLocaleString()} buffered` : ''}</span>
        {:else if follow}
          <span class="mode ok">Following</span>
        {:else}
          <span class="mode">Scrolled back</span>
        {/if}
        <span class="keys"><span class="kbd">End</span> live</span>
      {:else}
        <span class="count"><b>{hits.length.toLocaleString()}</b> results</span>
        <span class="grow"></span>
        <span class="keys"><span class="kbd">Enter</span> search</span>
      {/if}
    </div>
  </div>
</div>

<style>
  .logs {
    padding: var(--space-5) var(--space-6);
    gap: var(--space-4);
  }

  .page-header {
    align-items: center;
    flex-wrap: nowrap;
  }

  .page-header .titles {
    min-width: 0;
  }

  .subtitle {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .pill {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 4px 12px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-1);
    font-size: var(--fs-sm);
    font-weight: 500;
    white-space: nowrap;
  }

  .pill.ok {
    color: var(--ok);
    border-color: color-mix(in srgb, var(--ok) 35%, transparent);
    background: color-mix(in srgb, var(--ok) 10%, transparent);
  }

  .pill.warn {
    color: var(--warn);
    border-color: color-mix(in srgb, var(--warn) 40%, transparent);
    background: color-mix(in srgb, var(--warn) 10%, transparent);
  }

  .pill.busy {
    color: var(--info);
    border-color: color-mix(in srgb, var(--info) 35%, transparent);
    background: color-mix(in srgb, var(--info) 10%, transparent);
  }

  .view {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    position: relative;
    z-index: 1;
  }

  .strip {
    border-radius: 0;
    border-width: 0 0 1px 0;
    flex: none;
  }

  .grow {
    flex: 1;
  }

  .cta {
    display: flex;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }

  .foot {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 6px var(--space-5);
    border-top: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-1);
    font-size: var(--fs-xs);
    min-height: 30px;
    overflow: hidden;
    white-space: nowrap;
  }

  .foot b {
    color: var(--text-0);
    font-variant-numeric: tabular-nums;
    font-weight: 600;
  }

  .sep {
    color: var(--text-2);
  }

  .warnText {
    color: var(--warn);
  }

  .mode {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-2);
    font-weight: 500;
  }

  .mode::before {
    content: '';
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--neutral);
  }

  .mode.ok {
    color: var(--ok);
  }

  .mode.ok::before {
    background: var(--ok);
  }

  .mode.warn {
    color: var(--warn);
  }

  .mode.warn::before {
    background: var(--warn);
  }

  .keys {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-2);
  }

  @media (max-width: 900px) {
    .logs {
      padding: var(--space-4);
    }

    .keys {
      display: none;
    }
  }
</style>
