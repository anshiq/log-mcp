<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import Search from '@lucide/svelte/icons/search';
  import Eraser from '@lucide/svelte/icons/eraser';
  import Download from '@lucide/svelte/icons/download';
  import Hourglass from '@lucide/svelte/icons/hourglass';
  import Radio from '@lucide/svelte/icons/radio';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import Regex from '@lucide/svelte/icons/regex';
  import CaseSensitive from '@lucide/svelte/icons/case-sensitive';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Pause from '@lucide/svelte/icons/pause';
  import Play from '@lucide/svelte/icons/play';
  import FilterX from '@lucide/svelte/icons/filter-x';
  import LogPane from '../features/logs/LogPane.svelte';
  import LogViewer from './LogViewer.svelte';
  import ActionMenu, { type MenuEntry } from './ActionMenu.svelte';
  import { procActions } from './processActions.svelte';
  import { LogService, ProcessService } from '../lib/api';
  import { buildMatcher, stripAnsi } from '../lib/ansi';
  import { LogSession } from '../lib/state/logs.svelte';
  import { LEVEL_RANK, type LevelFilter } from '../lib/state/logsui.svelte';
  import { prefs } from '../lib/state/prefs.svelte';
  import { getPlatform } from '../lib/platform';
  import { toasts, toastError } from '../lib/toasts.svelte';
  import type { LogLine } from './LogViewer.svelte';

  let { processId, live, workspaceId = '' }: { processId: string; live: boolean; workspaceId?: string } = $props();

  const session = new LogSession({ max: prefs.data.maxBufferLines });

  let mode = $state<'live' | 'search'>('live');
  let follow = $state(true);
  let query = $state('');
  let debounced = $state('');
  let regex = $state(false);
  let caseSensitive = $state(false);
  let level = $state<LevelFilter>('all');
  let stdout = $state(true);
  let stderr = $state(true);
  let searching = $state(false);
  let searched = $state(false);
  let searchError = $state('');
  let results = $state<LogLine[]>([]);
  let truncated = $state(false);
  let searchedRe = $state<ReturnType<typeof buildMatcher>['re']>(null);
  let searchToken = 0;
  let lastTarget = '';

  const matcher = $derived(buildMatcher(debounced, regex, caseSensitive));
  const liveMatcher = $derived(mode === 'live' ? matcher : null);
  const matcherError = $derived(matcher.error);
  const streams = $derived(stdout && stderr ? null : new Set([stdout ? 'stdout' : '', stderr ? 'stderr' : ''].filter(Boolean)));
  const buffered = $derived(Math.max(0, session.liveCount - session.viewCount));
  const noFilterHit = $derived(session.total > 0 && session.viewCount === 0);
  const connLabel = $derived(
    !live ? 'Process not running' : session.conn === 'live' ? 'Streaming' : session.conn === 'reconnecting' ? 'Reconnecting…' : session.conn === 'connecting' ? 'Connecting…' : 'Disconnected'
  );

  const levels: { id: LevelFilter; label: string }[] = [
    { id: 'all', label: 'All' },
    { id: 'debug', label: 'Debug' },
    { id: 'info', label: 'Info' },
    { id: 'warn', label: 'Warn' },
    { id: 'error', label: 'Error' }
  ];

  $effect(() => {
    const v = query;
    const t = setTimeout(() => (debounced = v), v === '' ? 0 : 140);
    return () => clearTimeout(t);
  });

  $effect(() => {
    const crit = { procs: null, streams, minRank: LEVEL_RANK[level], matcher: liveMatcher };
    untrack(() => session.setCriteria(crit));
  });

  $effect(() => {
    const id = processId;
    const ws = workspaceId;
    const backlog = prefs.data.defaultBacklog;
    untrack(() => {
      const key = `${ws}|${id}`;
      if (key === lastTarget) {
        session.setTargets(new Map([[ws, [id]]]), backlog);
        return;
      }
      lastTarget = key;
      session.reset();
      follow = true;
      searched = false;
      searchError = '';
      session.setTargets(new Map([[ws, [id]]]), backlog);
    });
  });

  $effect(() => {
    void level;
    void stdout;
    void stderr;
    void regex;
    void caseSensitive;
    untrack(() => {
      if (mode === 'search' && searched) void runSearch();
    });
  });

  onDestroy(() => session.dispose());

  async function runSearch() {
    if (!query.trim()) return;
    const q = query.trim();
    const token = ++searchToken;
    searching = true;
    searchError = '';
    const m = buildMatcher(q, regex, caseSensitive);
    if (m.error) {
      searching = false;
      searchError = `Invalid regular expression: ${m.error}`;
      return;
    }
    try {
      const res = await LogService.search({
        workspaceId,
        processIds: [processId],
        query: q,
        regex,
        caseSensitive,
        minLevel: level === 'all' ? '' : level,
        streams: stdout && stderr ? [] : [stdout ? 'stdout' : '', stderr ? 'stderr' : ''].filter(Boolean),
        maxRows: 1000,
        contextLines: 0
      });
      if (token !== searchToken) return;
      const matches = (res.matches as { line: string; stream?: string; timestamp?: string | number; level?: string; processId?: string }[]) ?? [];
      results = matches.map((x) => ({
        line: x.line,
        stream: x.stream,
        timestamp: x.timestamp?.toString(),
        level: x.level,
        processId: x.processId ?? processId
      }));
      truncated = !!res.truncatedScan;
      searchedRe = m.re;
      searched = true;
    } catch (err) {
      if (token !== searchToken) return;
      searchError = err instanceof Error ? err.message : String(err);
    } finally {
      if (token === searchToken) searching = false;
    }
  }

  function viewText(): string {
    return session
      .viewLines()
      .map((l) => `${new Date(l.ts).toISOString()} ${l.stream.padEnd(6)} ${stripAnsi(l.text)}`)
      .join('\n');
  }

  async function clearLogs() {
    const ok = await procActions.ask({
      title: 'Clear logs',
      message: "Delete this process's stored log lines? This cannot be undone.",
      confirmLabel: 'Clear logs',
      danger: true
    });
    if (!ok) return;
    try {
      await LogService.clear(processId);
      session.clear();
      results = [];
      toasts.ok('Logs cleared');
    } catch (err) {
      toastError(err);
    }
  }

  async function exportLogs() {
    let data = '';
    let finished = false;
    const finish = () => {
      if (finished) return;
      finished = true;
      h.close();
      void getPlatform().saveFile(`${processId}.log`, data || viewText() || results.map((l) => l.line).join('\n'));
    };
    const h = LogService.exportLogs(processId, 'txt', (msg) => {
      if (msg.kind === 'chunk' && typeof msg['data'] === 'string') data += msg['data'] as string;
      if (msg.kind === 'done') finish();
    });
    setTimeout(() => {
      if (!data) finish();
    }, 5000);
  }

  async function waitExit() {
    try {
      const res = await ProcessService.waitForExit(processId, 5000);
      if (res.exited) toasts.ok(`Process exited${res.exitCode !== undefined ? ` with code ${res.exitCode}` : ''}`);
      else toasts.info('Still running after 5 seconds');
    } catch (err) {
      toastError(err);
    }
  }

  async function waitReady() {
    try {
      const res = await LogService.waitForLog(processId, 'ready', undefined, 5000);
      if (res.matched) toasts.ok(`Matched: ${res.entry?.line ?? 'ready'}`);
      else toasts.info('No "ready" log line within 5 seconds');
    } catch (err) {
      toastError(err);
    }
  }

  function clearView() {
    session.clear();
    follow = true;
  }

  function clearFilters() {
    query = '';
    debounced = '';
    level = 'all';
    stdout = true;
    stderr = true;
  }

  const moreItems: MenuEntry[] = [
    { id: 'export', label: 'Export logs', icon: Download, onselect: () => void exportLogs() },
    { id: 'clear', label: 'Clear logs', icon: Eraser, danger: true, onselect: () => void clearLogs() },
    { type: 'separator' },
    { id: 'wait-exit', label: 'Wait for exit (5s)', icon: Hourglass, onselect: () => void waitExit() },
    { id: 'wait-log', label: 'Wait for "ready" line (5s)', icon: Radio, onselect: () => void waitReady() }
  ];
</script>

<div class="logs">
  <div class="bar">
    <div class="seg" role="group" aria-label="Log mode">
      <button type="button" class:active={mode === 'live'} aria-pressed={mode === 'live'} onclick={() => (mode = 'live')}>Live</button>
      <button type="button" class:active={mode === 'search'} aria-pressed={mode === 'search'} onclick={() => (mode = 'search')}>Search</button>
    </div>
    <div class="filter" class:bad={!!matcherError}>
      <Search size={13} />
      <input
        placeholder={mode === 'live' ? 'Filter lines…' : 'Search log history…'}
        bind:value={query}
        aria-label={mode === 'live' ? 'Filter lines' : 'Search logs'}
        spellcheck="false"
        autocomplete="off"
        onkeydown={(e) => {
          if (e.key === 'Enter' && mode === 'search') {
            e.preventDefault();
            void runSearch();
          } else if (e.key === 'Escape' && query) {
            query = '';
          }
        }}
      />
      <button type="button" class="tog" class:on={caseSensitive} aria-pressed={caseSensitive} aria-label="Match case" title="Match case" onclick={() => (caseSensitive = !caseSensitive)}>
        <CaseSensitive size={14} />
      </button>
      <button type="button" class="tog" class:on={regex} aria-pressed={regex} aria-label="Use regular expression" title="Regular expression" onclick={() => (regex = !regex)}>
        <Regex size={14} />
      </button>
    </div>
    {#if mode === 'search'}
      <button type="button" class="btn sm primary" disabled={searching || !query.trim()} onclick={() => void runSearch()}>{searching ? 'Searching…' : 'Search'}</button>
    {:else}
      <span class="stream" class:on={session.conn === 'live' && live}>
        <span class="dot {session.conn === 'live' && live ? 'ok' : ''}"></span>
        {connLabel}
      </span>
    {/if}
  </div>

  <div class="bar sub">
    <div class="seg" role="group" aria-label="Minimum level">
      {#each levels as l (l.id)}
        <button type="button" class:active={level === l.id} aria-pressed={level === l.id} onclick={() => (level = l.id)}>{l.label}</button>
      {/each}
    </div>
    <div class="chips" role="group" aria-label="Streams">
      <button type="button" class="chip" class:active={stdout} aria-pressed={stdout} onclick={() => (stdout = !stdout)}>stdout</button>
      <button type="button" class="chip err" class:active={stderr} aria-pressed={stderr} onclick={() => (stderr = !stderr)}>stderr</button>
    </div>
    <span class="spacer"></span>
    {#if mode === 'live'}
      <button
        type="button"
        class="btn sm ghost"
        aria-pressed={session.paused}
        title={session.paused ? 'Resume live updates' : 'Freeze the view while logs keep buffering'}
        onclick={() => (session.paused ? session.resume() : session.pause())}
      >
        {#if session.paused}<Play size={13} />Resume{#if buffered > 0}<span class="badge warn">{buffered.toLocaleString()}</span>{/if}{:else}<Pause size={13} />Pause{/if}
      </button>
      <button type="button" class="btn ghost icon sm" title="Clear view (stored logs are kept)" aria-label="Clear view" onclick={clearView}>
        <Eraser size={14} />
      </button>
    {/if}
    <button type="button" class="btn ghost icon sm" title="Export logs" aria-label="Export logs" onclick={() => void exportLogs()}>
      <Download size={14} />
    </button>
    <ActionMenu items={moreItems} label="More log actions">
      <Ellipsis size={15} />
    </ActionMenu>
  </div>

  {#if matcherError}
    <p class="errline" role="alert">Invalid regular expression: {matcherError}</p>
  {/if}
  {#if mode === 'search' && searchError}
    <p class="errline" role="alert">{searchError}</p>
  {/if}
  {#if mode === 'search' && truncated}
    <div class="banner warn note"><TriangleAlert size={14} /> Results were truncated. Narrow the query for a complete scan.</div>
  {/if}

  <div class="logbody">
    {#if mode === 'live'}
      {#if noFilterHit}
        <div class="hint-pane">
          <FilterX size={20} />
          <p>{session.total.toLocaleString()} lines are buffered but none pass the current filters.</p>
          <button type="button" class="btn sm" onclick={clearFilters}>Clear filters</button>
        </div>
      {:else}
        <LogPane
          {session}
          bind:follow
          wrap={prefs.data.wrap}
          fontSize={prefs.data.logFontSize}
          compact={prefs.data.density === 'compact'}
          tsFormat={prefs.data.timestamps}
          ansi={prefs.data.ansi}
          showTags={false}
          showLevels={true}
          showLineNumbers
          zebra={true}
          matcher={liveMatcher}
          label="Process logs"
        >
          {#snippet empty()}
            <div class="hint-pane">
              <Search size={20} />
              <p>{live ? 'Waiting for output. New lines appear here as soon as they are written.' : 'Process is not running. Switch to Search to read retained history.'}</p>
            </div>
          {/snippet}
        </LogPane>
      {/if}
    {:else if !searched}
      <div class="hint-pane">
        <Search size={20} />
        <p>Search the stored history of this process. Filters for level and stream apply to search too.</p>
      </div>
    {:else}
      <LogViewer lines={results} matcher={searchedRe ? { active: true, error: '', re: searchedRe, test: () => true } : null} emptyText="No matches found." />
    {/if}
  </div>
  <div class="foot">
    {#if mode === 'live'}
      <span><b>{session.total.toLocaleString()}</b> lines</span>
      <span class="sep">·</span>
      <span><b>{session.viewCount.toLocaleString()}</b> shown</span>
      {#if session.paused && buffered > 0}
        <span class="sep">·</span>
        <span class="warnText">{buffered.toLocaleString()} buffered</span>
      {/if}
      <span class="grow"></span>
      {#if session.paused}<span class="mode warn">Paused</span>{:else if follow}<span class="mode ok">Following</span>{:else}<span class="mode">Scrolled back</span>{/if}
    {:else if searched}
      <span>{results.length.toLocaleString()} matches</span>
    {/if}
  </div>
</div>

<style>
  .logs {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-5);
    border-bottom: 1px solid var(--border);
    min-height: 46px;
    flex-wrap: wrap;
  }
  .bar.sub {
    min-height: 0;
    padding-top: var(--space-2);
    padding-bottom: var(--space-2);
  }
  .seg > button {
    padding: 2px 12px;
    font-size: var(--fs-xs);
    font-weight: 500;
  }
  .spacer {
    flex: 1;
  }
  .filter {
    flex: 1 1 200px;
    min-width: 160px;
    display: flex;
    align-items: center;
    gap: 4px;
    height: 30px;
    padding: 0 4px 0 10px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--text-2);
  }
  .filter:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-subtle);
  }
  .filter.bad {
    border-color: var(--err);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--err) 18%, transparent);
  }
  .filter input {
    flex: 1;
    min-width: 0;
    height: 100%;
    padding: 0 4px;
    border: none;
    background: transparent;
    box-shadow: none;
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
  }
  .filter input:focus {
    border: none;
    box-shadow: none;
  }
  .tog {
    width: 24px;
    height: 24px;
    padding: 0;
    border: 1px solid transparent;
    background: transparent;
    color: var(--text-2);
    border-radius: var(--radius-sm);
    display: grid;
    place-items: center;
  }
  .tog:hover:not(:disabled) {
    background: var(--bg-hover);
    color: var(--text-0);
  }
  .tog.on {
    color: var(--accent);
    background: var(--accent-subtle);
    border-color: color-mix(in srgb, var(--accent) 40%, transparent);
  }
  .stream {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: var(--fs-xs);
    color: var(--text-2);
    white-space: nowrap;
  }
  .stream.on {
    color: var(--text-1);
  }
  .stream .dot {
    width: 6px;
    height: 6px;
    box-shadow: none;
  }
  .errline {
    color: var(--err);
    font-size: var(--fs-xs);
    font-family: var(--font-mono);
    margin: var(--space-2) var(--space-5) 0;
  }
  .note {
    margin: var(--space-3) var(--space-5) 0;
    font-size: var(--fs-xs);
    padding: 6px 10px;
  }
  .logbody {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-1);
  }
  .logbody > :global(*) {
    flex: 1;
    min-height: 0;
  }
  .hint-pane {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    color: var(--text-2);
    text-align: center;
    padding: var(--space-8);
  }
  .hint-pane p {
    max-width: 320px;
    font-size: var(--fs-sm);
    margin: 0;
  }
  .foot {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    justify-content: flex-start;
    padding: 5px var(--space-5);
    border-top: 1px solid var(--border);
    font-size: var(--fs-xs);
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
    min-height: 26px;
  }
  .foot b {
    color: var(--text-0);
  }
  .foot .sep {
    opacity: 0.6;
  }
  .foot .grow {
    flex: 1;
  }
  .foot .mode {
    font-weight: 500;
  }
  .foot .mode.ok {
    color: var(--ok);
  }
  .foot .mode.warn {
    color: var(--warn);
  }
  .warnText {
    color: var(--warn);
  }
  .badge.warn {
    margin-left: 6px;
  }
</style>
