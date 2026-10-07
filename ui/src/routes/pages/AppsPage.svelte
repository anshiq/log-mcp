<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import Boxes from '@lucide/svelte/icons/boxes';
  import Search from '@lucide/svelte/icons/search';
  import Play from '@lucide/svelte/icons/play';
  import Square from '@lucide/svelte/icons/square';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import FileCog from '@lucide/svelte/icons/file-cog';
  import Rocket from '@lucide/svelte/icons/rocket';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import Network from '@lucide/svelte/icons/network';
  import HeartPulse from '@lucide/svelte/icons/heart-pulse';
  import Radio from '@lucide/svelte/icons/radio';
  import Repeat from '@lucide/svelte/icons/repeat';
  import Zap from '@lucide/svelte/icons/zap';
  import Timer from '@lucide/svelte/icons/timer';
  import Link2 from '@lucide/svelte/icons/link-2';
  import X from '@lucide/svelte/icons/x';
  import Check from '@lucide/svelte/icons/check';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import WorkspacePicker from '../../components/WorkspacePicker.svelte';
  import { ConfigService, ProcessService, openStream } from '../../lib/api';
  import type { Process } from '../../lib/api/types';
  import { router } from '../../lib/router.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { processes } from '../../lib/state/processes.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import { duration } from '../../lib/format';
  import { commandLine, isLive, matchProcesses, primaryProcess, toConfigApps, type ConfigApp } from '../../lib/config/apps';

  interface Row {
    app: ConfigApp;
    matches: Process[];
    primary: Process | null;
    live: boolean;
  }

  interface StackResult {
    order: string[];
    started: { app: string; processId: string }[];
    failed: string;
    error: string;
  }

  interface LayerProblem {
    layer: string;
    count: number;
  }

  let apps = $state<ConfigApp[]>([]);
  let loading = $state(false);
  let loadError = $state('');
  let loadedFor = $state('');
  let problems = $state<LayerProblem[]>([]);
  let query = $state('');
  let filter = $state<'all' | 'running' | 'stopped'>('all');
  let selected = $state(new Set<string>());
  let busy = $state(new Set<string>());
  let stack = $state<StackResult | null>(null);
  let starting = $state(false);
  let now = $state(Date.now());
  let watcher: { close(): void } | null = null;
  let loadToken = 0;

  const wsId = $derived(scopeState.workspaceId);

  const rows = $derived.by<Row[]>(() => {
    const pool = processes.list.filter((p) => p.workspaceId === wsId);
    return apps.map((app) => {
      const matches = matchProcesses(app, pool);
      const primary = primaryProcess(matches);
      return { app, matches, primary, live: !!primary && isLive(primary.status) };
    });
  });

  const counts = $derived({
    all: rows.length,
    running: rows.filter((r) => r.live).length,
    stopped: rows.filter((r) => !r.live).length
  });

  const visible = $derived(
    rows.filter((r) => {
      if (filter === 'running' && !r.live) return false;
      if (filter === 'stopped' && r.live) return false;
      const q = query.trim().toLowerCase();
      if (!q) return true;
      return (
        r.app.name.toLowerCase().includes(q) ||
        commandLine(r.app.command).toLowerCase().includes(q) ||
        r.app.type.toLowerCase().includes(q) ||
        r.app.ports.some((p) => String(p.port).includes(q) || p.name.toLowerCase().includes(q))
      );
    })
  );

  const allVisibleSelected = $derived(visible.length > 0 && visible.every((r) => selected.has(r.app.name)));

  $effect(() => {
    const id = wsId;
    untrack(() => {
      selected = new Set();
      stack = null;
      query = '';
      filter = 'all';
      if (!id) {
        apps = [];
        loadedFor = '';
        watcher?.close();
        watcher = null;
        return;
      }
      void load(id);
    });
  });

  $effect(() => {
    if (!rows.some((r) => r.live)) return;
    const t = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(t);
  });

  onDestroy(() => watcher?.close());

  async function load(id: string, silent = false) {
    const token = ++loadToken;
    if (!silent) loading = true;
    loadError = '';
    try {
      const cfg = await ConfigService.get(id);
      if (token !== loadToken) return;
      const c = cfg as unknown as { provenance?: Record<string, unknown>; revision?: number; projectId?: string };
      apps = toConfigApps(cfg.apps, c.provenance ?? {});
      loadedFor = id;
      const raw = (cfg.raw ?? {}) as Record<string, string>;
      const checks = await Promise.all(
        Object.entries(raw)
          .filter(([, text]) => typeof text === 'string' && text.trim() !== '')
          .map(async ([layer, text]) => {
            try {
              const res = await ConfigService.validate(text);
              return res.valid ? null : { layer, count: res.errors.length };
            } catch {
              return null;
            }
          })
      );
      if (token !== loadToken) return;
      problems = checks.filter((x): x is LayerProblem => x !== null);
      if (!watcher && c.projectId) {
        const projectId = c.projectId;
        const since = Number(c.revision ?? 0);
        watcher = openStream(
          'ConfigService',
          'WatchConfig',
          { projectId, since },
          {
            onMessage: (msg) => {
              if (msg.kind === 'revision' && scopeState.workspaceId) void load(scopeState.workspaceId, true);
            }
          }
        );
      }
    } catch (err) {
      if (token !== loadToken) return;
      loadError = err instanceof Error ? err.message : String(err);
      apps = [];
    } finally {
      if (token === loadToken) loading = false;
    }
  }

  function setBusy(name: string, on: boolean) {
    const next = new Set(busy);
    if (on) next.add(name);
    else next.delete(name);
    busy = next;
  }

  async function act(name: string, fn: () => Promise<unknown>, done: string) {
    setBusy(name, true);
    try {
      await fn();
      toasts.ok(done);
    } catch (err) {
      toastError(err);
    } finally {
      setBusy(name, false);
    }
  }

  const start = (r: Row) => act(r.app.name, () => ProcessService.start({ workspaceId: wsId, app: r.app.name }), `Started ${r.app.name}`);
  const stop = (r: Row) => (r.primary ? act(r.app.name, () => ProcessService.stop(r.primary!.id), `Stopped ${r.app.name}`) : Promise.resolve());
  const restart = (r: Row) => (r.primary ? act(r.app.name, () => ProcessService.restart(r.primary!.id), `Restarted ${r.app.name}`) : Promise.resolve());

  function logs(p: Process) {
    void router.navigate(`/logs?p=${encodeURIComponent(p.id)}`);
  }

  function toggle(name: string) {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    selected = next;
  }

  function toggleAll() {
    if (allVisibleSelected) {
      const next = new Set(selected);
      for (const r of visible) next.delete(r.app.name);
      selected = next;
    } else {
      const next = new Set(selected);
      for (const r of visible) next.add(r.app.name);
      selected = next;
    }
  }

  async function startStack() {
    starting = true;
    stack = null;
    try {
      const res = await ProcessService.startStack(wsId, [...selected]);
      stack = {
        order: res.order ?? [],
        started: res.started ?? [],
        failed: res.failed ?? '',
        error: res.error ?? ''
      };
      if (stack.failed) toasts.err(`Stack stopped at ${stack.failed}`);
      else toasts.ok(`Stack started (${stack.started.length} app${stack.started.length === 1 ? '' : 's'})`);
    } catch (err) {
      toastError(err);
    } finally {
      starting = false;
    }
  }

  function stackState(name: string): 'ok' | 'failed' | 'skipped' {
    if (!stack) return 'skipped';
    if (stack.failed === name) return 'failed';
    return stack.started.some((s) => s.app === name) ? 'ok' : 'skipped';
  }

  function stackProc(name: string): string {
    return stack?.started.find((s) => s.app === name)?.processId ?? '';
  }

  function statusOf(r: Row): { label: string; dot: string; tone: string } {
    const p = r.primary;
    if (!p) return { label: 'Not running', dot: '', tone: 'muted' };
    switch (p.status) {
      case 'running':
      case 'ready':
        return { label: p.health && p.health !== 'unknown' && p.health !== 'none' ? `Running · ${p.health}` : 'Running', dot: 'ok', tone: 'ok' };
      case 'starting':
        return { label: 'Starting', dot: 'busy', tone: 'info' };
      case 'failed':
      case 'crashed':
        return { label: p.exitCode !== null ? `Crashed · exit ${p.exitCode}` : 'Crashed', dot: 'err', tone: 'err' };
      default:
        return { label: p.exitCode ? `Exited · code ${p.exitCode}` : 'Stopped', dot: '', tone: 'muted' };
    }
  }

  function uptime(p: Process): string {
    return p.startedAt ? duration(now - p.startedAt) : '';
  }

  const layerNames: Record<string, string> = { project: 'project', workspace: 'workspace' };
</script>

<div class="page">
  {#if scopeState.all}
    <div class="center">
      <WorkspacePicker
        icon={Boxes}
        title="Pick a workspace to see its apps"
        body="Apps are the services defined in a workspace's config. Choose a workspace to start, stop and inspect them."
      />
    </div>
  {:else}
    <div class="page-header">
      <div class="titles">
        <h1>Apps</h1>
        <span class="subtitle">
          {scopeState.label}
          {#if loadedFor === wsId}
            · {counts.all} configured · {counts.running} running
          {/if}
        </span>
      </div>
      <div class="actions">
        <button class="btn icon" aria-label="Refresh apps" title="Refresh" onclick={() => void load(wsId)} disabled={loading}>
          {#if loading}<Spinner size={15} />{:else}<RefreshCw size={15} />{/if}
        </button>
        <button class="btn" onclick={() => void router.navigate('/config')}><FileCog size={15} />Edit config</button>
      </div>
    </div>

    {#if loadError}
      <div class="banner err" role="alert">
        <CircleAlert size={16} />
        <span class="grow">Could not load this workspace's config: {loadError}</span>
        <button class="btn sm" onclick={() => void load(wsId)}>Retry</button>
      </div>
    {/if}

    {#if problems.length > 0}
      <div class="banner err" role="alert">
        <TriangleAlert size={16} />
        <span class="grow">
          The config has validation errors ({problems.map((p) => `${p.count} in the ${layerNames[p.layer] ?? p.layer} layer`).join(', ')}). Some apps may be missing or stale.
        </span>
        <button class="btn sm" onclick={() => void router.navigate('/config')}>Open in editor<ArrowRight size={13} /></button>
      </div>
    {/if}

    {#if loading && loadedFor !== wsId}
      <div class="grid" aria-busy="true">
        {#each Array.from({ length: 3 }) as _, i (i)}
          <div class="app skeleton">
            <span class="sk w40"></span>
            <span class="sk w90"></span>
            <span class="sk w60"></span>
            <span class="sk w30"></span>
          </div>
        {/each}
      </div>
    {:else if loadedFor === wsId && apps.length === 0 && !loadError}
      <div class="empty">
        <div class="empty-state">
          <div class="icon-wrap"><Boxes size={22} /></div>
          <h3>No apps configured</h3>
          <p>Define apps in this workspace's project config and they will show up here, ready to start with one click.</p>
          <button class="btn primary" onclick={() => void router.navigate('/config')}><FileCog size={15} />Open config editor</button>
        </div>
      </div>
    {:else if apps.length > 0}
      <div class="toolbar">
        <div class="input-wrap search">
          <Search size={15} />
          <input bind:value={query} placeholder="Search apps, commands, ports" aria-label="Search apps" spellcheck="false" />
        </div>
        <div class="chips" role="group" aria-label="Filter by status">
          <button class="chip" class:active={filter === 'all'} onclick={() => (filter = 'all')}>All <span class="count">{counts.all}</span></button>
          <button class="chip" class:active={filter === 'running'} onclick={() => (filter = 'running')}>Running <span class="count">{counts.running}</span></button>
          <button class="chip" class:active={filter === 'stopped'} onclick={() => (filter = 'stopped')}>Stopped <span class="count">{counts.stopped}</span></button>
        </div>
        <span class="grow"></span>
        <label class="selall">
          <input type="checkbox" checked={allVisibleSelected} onchange={toggleAll} disabled={visible.length === 0} aria-label="Select all visible apps" />
          Select all
        </label>
        <button class="btn primary" disabled={selected.size === 0 || starting} onclick={startStack}>
          {#if starting}<Spinner size={14} />Starting…{:else}<Rocket size={15} />Start stack{#if selected.size > 0}<span class="sel">{selected.size}</span>{/if}{/if}
        </button>
      </div>

      {#if stack}
        <section class="stack" class:bad={!!stack.failed} aria-label="Start stack result">
          <header>
            {#if stack.failed}
              <CircleAlert size={16} />
              <strong>Stack stopped at {stack.failed}</strong>
            {:else}
              <Check size={16} />
              <strong>Stack started</strong>
            {/if}
            <span class="muted">dependency order</span>
            <span class="grow"></span>
            <button class="btn icon ghost sm" aria-label="Dismiss result" onclick={() => (stack = null)}><X size={14} /></button>
          </header>
          {#if stack.order.length > 0}
            <ol class="order">
              {#each stack.order as name, i (name)}
                {@const st = stackState(name)}
                <li class={st}>
                  {#if i > 0}<ArrowRight size={13} class="arrow" />{/if}
                  <span class="step">
                    <span class="dot" class:ok={st === 'ok'} class:err={st === 'failed'}></span>
                    <span class="mono">{name}</span>
                    <span class="st">{st === 'ok' ? 'started' : st === 'failed' ? 'failed' : 'skipped'}</span>
                    {#if st === 'ok' && stackProc(name)}
                      <button class="linkbtn" onclick={() => router.navigate(`/logs?p=${encodeURIComponent(stackProc(name))}`)}>logs</button>
                    {/if}
                  </span>
                </li>
              {/each}
            </ol>
          {/if}
          {#if stack.failed}
            <div class="failure">
              <span class="mono">{stack.failed}</span>
              <span>{stack.error || 'The app did not become ready in time.'}</span>
            </div>
          {/if}
        </section>
      {/if}

      {#if visible.length === 0}
        <div class="empty">
          <div class="empty-state">
            <div class="icon-wrap"><Search size={20} /></div>
            <h3>No apps match</h3>
            <p>Nothing matches the current search or status filter.</p>
            <button class="btn sm" onclick={() => { query = ''; filter = 'all'; }}>Clear filters</button>
          </div>
        </div>
      {:else}
        <div class="grid">
          {#each visible as r (r.app.name)}
            {@const st = statusOf(r)}
            {@const isBusy = busy.has(r.app.name)}
            <article class="app" class:selected={selected.has(r.app.name)} class:live={r.live} aria-label={`App ${r.app.name}`}>
              <header>
                <input type="checkbox" checked={selected.has(r.app.name)} onchange={() => toggle(r.app.name)} aria-label={`Select ${r.app.name}`} />
                <h3 class="name truncate" title={r.app.name}>{r.app.name}</h3>
                {#if r.matches.length > 1}<span class="badge" title="Matching processes">×{r.matches.length}</span>{/if}
                <span class="grow"></span>
                <span class="status {st.tone}"><span class="dot {st.dot}"></span>{st.label}</span>
              </header>

              <code class="cmd mono" title={commandLine(r.app.command)}>
                <span class="prompt">$</span><span class="truncate">{r.app.command.length > 0 ? commandLine(r.app.command) : 'no command'}</span>
              </code>

              <div class="tags">
                {#if r.app.type}<span class="badge">{r.app.type}</span>{/if}
                {#each r.app.ports as p (p.name)}
                  <span class="badge info" title={`port ${p.name}`}><Network size={11} />:{p.port}{p.name ? ` ${p.name}` : ''}</span>
                {/each}
                {#if r.app.readiness.length > 0}
                  <span class="badge" title={`Ready when logs match: ${r.app.readiness.join(', ')}`}><Radio size={11} />readiness</span>
                {/if}
                {#if r.app.healthHttp || r.app.healthTcp}
                  <span class="badge" title={r.app.healthHttp || r.app.healthTcp}><HeartPulse size={11} />health check</span>
                {/if}
                {#if r.app.restartPolicy && r.app.restartPolicy !== 'never'}
                  <span class="badge" title="Restart policy"><Repeat size={11} />{r.app.restartPolicy}</span>
                {/if}
                {#if r.app.autostart}<span class="badge accent"><Zap size={11} />autostart</span>{/if}
                {#if r.app.lifetime === 'session'}<span class="badge"><Timer size={11} />session</span>{/if}
                {#if r.primary?.stale}<span class="badge warn" title="Config changed since this process started">restart needed</span>{/if}
              </div>

              {#if r.app.dependsOn.length > 0}
                <div class="deps">
                  <Link2 size={12} />
                  <span>needs</span>
                  {#each r.app.dependsOn as d (d)}<span class="dep mono">{d}</span>{/each}
                </div>
              {/if}

              <footer>
                <div class="facts">
                  {#if r.primary && r.live}
                    <span title="Process id">pid <span class="mono">{r.primary.pid || '—'}</span></span>
                    {#if uptime(r.primary)}<span title="Uptime">{uptime(r.primary)}</span>{/if}
                    {#if r.primary.restarts > 0}<span title="Restarts">{r.primary.restarts} restart{r.primary.restarts === 1 ? '' : 's'}</span>{/if}
                  {:else if r.primary}
                    <span>last run {r.primary.exitedAt ? `${duration(now - r.primary.exitedAt)} ago` : 'finished'}</span>
                  {:else}
                    <span>never started</span>
                  {/if}
                </div>
                <div class="acts">
                  {#if r.live}
                    <button class="btn sm icon" title="Restart" aria-label={`Restart ${r.app.name}`} disabled={isBusy} onclick={() => restart(r)}><RotateCw size={14} /></button>
                    <button class="btn sm danger" title="Stop" aria-label={`Stop ${r.app.name}`} disabled={isBusy} onclick={() => stop(r)}>
                      {#if isBusy}<Spinner size={12} />{:else}<Square size={12} />{/if}Stop
                    </button>
                  {:else}
                    <button class="btn sm primary" title="Start" aria-label={`Start ${r.app.name}`} disabled={isBusy} onclick={() => start(r)}>
                      {#if isBusy}<Spinner size={12} />{:else}<Play size={12} />{/if}Start
                    </button>
                  {/if}
                  {#if r.primary}
                    <button class="btn sm" title="Open logs" aria-label={`Logs for ${r.app.name}`} onclick={() => logs(r.primary!)}><ScrollText size={13} />Logs</button>
                  {/if}
                </div>
              </footer>
            </article>
          {/each}
        </div>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .center {
    flex: 1;
    display: flex;
  }
  .grow {
    flex: 1;
  }
  .empty {
    flex: 1;
    display: flex;
  }
  .empty .empty-state {
    margin: auto;
  }
  .search {
    width: 280px;
    max-width: 100%;
  }
  .selall {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-1);
    font-size: var(--fs-sm);
    cursor: pointer;
  }
  .sel {
    display: inline-grid;
    place-items: center;
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    border-radius: 9px;
    background: color-mix(in srgb, var(--accent-fg) 24%, transparent);
    font-size: var(--fs-xs);
    font-variant-numeric: tabular-nums;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: var(--space-5);
  }
  .app {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-5);
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    min-width: 0;
    transition:
      border-color var(--dur-fast) var(--ease),
      box-shadow var(--dur-fast) var(--ease);
  }
  .app:hover {
    border-color: var(--border-strong);
  }
  .app.selected {
    border-color: color-mix(in srgb, var(--accent) 60%, var(--border));
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--accent) 35%, transparent);
  }
  .app > header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }
  .name {
    font-size: var(--fs-lg);
    font-weight: 600;
    min-width: 0;
  }
  .status {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: var(--fs-xs);
    font-weight: 500;
    white-space: nowrap;
    color: var(--text-2);
  }
  .status.ok {
    color: var(--ok);
  }
  .status.info {
    color: var(--info);
  }
  .status.err {
    color: var(--err);
  }
  .cmd {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 7px var(--space-4);
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    font-size: var(--fs-sm);
    color: var(--text-1);
    min-width: 0;
  }
  .prompt {
    color: var(--accent);
    flex: none;
    user-select: none;
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .tags:empty {
    display: none;
  }
  .badge :global(svg) {
    flex: none;
  }
  .deps {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .dep {
    padding: 0 7px;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-1);
    line-height: 1.7;
  }
  .app > footer {
    margin-top: auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding-top: var(--space-4);
    border-top: 1px solid var(--border);
    flex-wrap: wrap;
  }
  .facts {
    display: flex;
    gap: var(--space-4);
    color: var(--text-2);
    font-size: var(--fs-xs);
    min-width: 0;
    flex-wrap: wrap;
  }
  .acts {
    display: flex;
    gap: var(--space-2);
    margin-left: auto;
  }
  .app.skeleton {
    gap: var(--space-4);
  }
  .sk {
    height: 12px;
    border-radius: 5px;
    background: linear-gradient(90deg, var(--bg-3), var(--bg-hover), var(--bg-3));
    background-size: 200% 100%;
    animation: shimmer 1.4s linear infinite;
  }
  .w30 {
    width: 30%;
  }
  .w40 {
    width: 40%;
  }
  .w60 {
    width: 60%;
  }
  .w90 {
    width: 90%;
  }
  @keyframes shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .stack {
    background: var(--bg-1);
    border: 1px solid color-mix(in srgb, var(--ok) 35%, var(--border));
    border-radius: var(--radius-lg);
    padding: var(--space-4) var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .stack.bad {
    border-color: color-mix(in srgb, var(--err) 45%, var(--border));
  }
  .stack > header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    color: var(--ok);
  }
  .stack.bad > header {
    color: var(--err);
  }
  .stack > header strong {
    color: var(--text-0);
  }
  .order {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }
  .order li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .order :global(.arrow) {
    color: var(--text-2);
  }
  .step {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    padding: 4px 10px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    font-size: var(--fs-sm);
  }
  li.ok .step {
    border-color: color-mix(in srgb, var(--ok) 35%, transparent);
  }
  li.failed .step {
    border-color: color-mix(in srgb, var(--err) 50%, transparent);
    background: color-mix(in srgb, var(--err) 10%, transparent);
  }
  li.skipped .step {
    opacity: 0.6;
  }
  .st {
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .linkbtn {
    padding: 0;
    border: none;
    background: transparent;
    color: var(--accent);
    font-size: var(--fs-xs);
    text-decoration: underline;
    text-underline-offset: 2px;
    cursor: pointer;
  }
  .failure {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--space-3) var(--space-4);
    background: color-mix(in srgb, var(--err) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--err) 30%, transparent);
    border-radius: var(--radius);
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .failure .mono {
    color: var(--err);
    font-weight: 600;
  }
  .banner {
    flex: none;
  }
  .banner .grow {
    min-width: 0;
  }
  @media (max-width: 900px) {
    .grid {
      grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    }
    .search {
      width: 100%;
    }
  }
</style>
