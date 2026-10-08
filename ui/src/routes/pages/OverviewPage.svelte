<script lang="ts">
  import { onMount } from 'svelte';
  import Play from '@lucide/svelte/icons/play';
  import CircleX from '@lucide/svelte/icons/circle-x';
  import Square from '@lucide/svelte/icons/square';
  import FolderKanban from '@lucide/svelte/icons/folder-kanban';
  import Users from '@lucide/svelte/icons/users';
  import HardDrive from '@lucide/svelte/icons/hard-drive';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import Layers from '@lucide/svelte/icons/layers';
  import Activity from '@lucide/svelte/icons/activity';
  import SquareTerminal from '@lucide/svelte/icons/square-terminal';
  import Bot from '@lucide/svelte/icons/bot';
  import Plug from '@lucide/svelte/icons/plug';
  import Terminal from '@lucide/svelte/icons/terminal';
  import Monitor from '@lucide/svelte/icons/monitor';
  import FolderOpen from '@lucide/svelte/icons/folder-open';
  import Rocket from '@lucide/svelte/icons/rocket';
  import X from '@lucide/svelte/icons/x';
  import Check from '@lucide/svelte/icons/check';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import Cpu from '@lucide/svelte/icons/cpu';
  import { ProcessService, SessionService, SystemService } from '../../lib/api';
  import type { Process, Session } from '../../lib/api';
  import { processes } from '../../lib/state/processes.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { connection } from '../../lib/state/connection.svelte';
  import { events } from '../../lib/state/events.svelte';
  import { resources } from '../../lib/state/resources.svelte';
  import { clock } from '../../lib/state/clock.svelte';
  import { router } from '../../lib/router.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import { bytes, duration, relativeTime } from '../../lib/format';
  import { commandLine, eventKind, eventSentence, eventTone, procTitle, scopedEvents } from '../../lib/activity';
  import StatCard from '../../features/activity/StatCard.svelte';
  import CardHeader from '../../features/activity/CardHeader.svelte';
  import ToneBadge from '../../features/activity/ToneBadge.svelte';
  import RelativeTime from '../../lib/ui/RelativeTime.svelte';
  import Sparkline from '../../lib/ui/Sparkline.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';

  interface Stats {
    projects: number;
    processes: number;
    sessions: number;
    processesRunning: number;
    processesFailed: number;
    logDiskBytes: number;
    indexLagSeconds: number;
    uptimeSeconds: number;
  }

  let stats = $state<Stats | null>(null);
  let statsAt = $state(0);
  let daemonStatus = $state('');
  let sessions = $state<Session[]>([]);
  let sessionsLoaded = $state(false);
  let restarting = $state(new Set<string>());
  let quickPath = $state('');
  let stepsDismissed = $state(false);

  async function refresh() {
    const [s, h, l] = await Promise.allSettled([SystemService.stats(), SystemService.health(), SessionService.list()]);
    if (s.status === 'fulfilled') {
      stats = s.value;
      statsAt = Date.now();
    }
    if (h.status === 'fulfilled') {
      daemonStatus = h.value.status;
      connection.version = h.value.version || connection.version;
    }
    if (l.status === 'fulfilled') {
      sessions = l.value;
      sessionsLoaded = true;
    }
  }

  onMount(() => {
    void refresh();
    const t = setInterval(() => void refresh(), 10000);
    const release = clock.retain();
    return () => {
      clearInterval(t);
      release();
      resources.watch([]);
    };
  });

  const counts = $derived(processes.counts);
  const scopedProcs = $derived(processes.scoped);
  const openSessions = $derived(
    sessions.filter((s) => !s.closed && (scopeState.all || !s.workspaceId || s.workspaceId === scopeState.workspaceId))
  );
  const fresh = $derived(scopeState.loaded && scopeState.workspaces.length === 0 && processes.list.length === 0);
  const showSteps = $derived(!stepsDismissed && (fresh || (scopedProcs.length === 0 && sessionsLoaded)));
  const uptimeMs = $derived.by(() => (stats ? stats.uptimeSeconds * 1000 + Math.max(0, clock.now - statsAt) : connection.uptimeSeconds * 1000));

  const attention = $derived(
    scopedProcs.filter(
      (p) =>
        p.status === 'failed' ||
        p.status === 'crashed' ||
        p.health === 'unhealthy' ||
        (p.status === 'exited' && p.exitCode !== null && p.exitCode !== 0 && p.exitedAt !== null && clock.now - p.exitedAt < 86400000)
    )
  );

  const shownProcs = $derived(scopedProcs.slice(0, 8));
  const recent = $derived(
    scopedEvents(events.items)
      .filter((e) => e.type !== 'process.stdout' && e.type !== 'process.stderr')
      .sort((a, b) => b.ts - a.ts)
      .slice(0, 10)
  );

  $effect(() => {
    resources.watch(shownProcs.filter((p) => p.status === 'running' || p.status === 'ready').map((p) => p.id));
  });



  const daemonOk = $derived(connection.reachable && (daemonStatus === '' || daemonStatus === 'ready'));

  function dotClass(p: Process): string {
    if (p.health === 'unhealthy') return 'warn';
    if (p.status === 'running' || p.status === 'ready') return 'ok';
    if (p.status === 'starting') return 'busy';
    if (p.status === 'failed' || p.status === 'crashed') return 'err';
    return '';
  }

  function attentionText(p: Process): string {
    const parts: string[] = [];
    if (p.status === 'failed' || p.status === 'crashed') parts.push(p.status === 'failed' ? 'Failed' : 'Crashed');
    else if (p.status === 'exited') parts.push('Exited');
    else if (p.health === 'unhealthy') parts.push('Unhealthy');
    if (p.exitCode !== null) parts.push(`exit code ${p.exitCode}`);
    if (p.exitSignal) parts.push(`signal ${p.exitSignal}`);
    parts.push(`${p.restarts} ${p.restarts === 1 ? 'restart' : 'restarts'}`);
    return parts.join(' · ');
  }

  async function restart(p: Process) {
    if (restarting.has(p.id)) return;
    restarting = new Set(restarting).add(p.id);
    try {
      await ProcessService.restart(p.id);
      toasts.ok(`Restarting ${procTitle(p)}`);
    } catch (err) {
      toastError(err);
    } finally {
      const next = new Set(restarting);
      next.delete(p.id);
      restarting = next;
    }
  }

  function cpuHistory(id: string): number[] {
    return resources.history(id).map((s) => s.cpuPercent);
  }

  function liveOf(id: string) {
    return resources.live(id);
  }

  function uptimeOf(p: Process): string {
    if (p.startedAt === null) return '—';
    if (p.status === 'running' || p.status === 'ready' || p.status === 'starting') return duration(Math.max(0, clock.now - p.startedAt));
    return p.exitedAt ? relativeTime(p.exitedAt) : '—';
  }

  function harnessLabel(s: Session): string {
    if (s.harness && s.harness !== 'unknown') return s.harness;
    return s.kind === 'gui' ? 'Desktop app' : s.kind === 'cli' ? 'CLI' : 'Unknown client';
  }

  function sessionLive(s: Session): boolean {
    return clock.now - s.lastSeen < 30000;
  }

  async function openQuick(e: Event) {
    e.preventDefault();
    if (!quickPath.trim()) return;
    if (await scopeState.open(quickPath)) quickPath = '';
  }

  const steps = $derived([
    { done: scopeState.workspaces.length > 0, title: 'Open a workspace', text: 'Point agent-runtime at a project directory. Use the scope switcher in the top bar any time.' },
    { done: processes.list.length > 0, title: 'Start a process', text: 'Run a dev server, worker or script from the Processes page and watch its logs live.' },
    { done: sessions.length > 0, title: 'Connect an AI agent', text: 'Install the MCP server for Claude, Cursor or others from Integrations so agents can drive your processes.' }
  ]);
</script>

<div class="page ov">
  <header class="page-header">
    <div class="titles">
      <h1>Overview</h1>
      <p class="subtitle mono" title={scopeState.path}>{scopeState.all ? 'All workspaces' : scopeState.path || scopeState.label}</p>
    </div>
    <div class="actions">
      <div class="pill" class:bad={!daemonOk} title={connection.socketPath || connection.tcpAddr}>
        <span class="dot" class:ok={daemonOk} class:err={!daemonOk}></span>
        <span>{daemonOk ? 'Daemon running' : 'Daemon unreachable'}</span>
        {#if connection.version}<span class="sep">·</span><span class="mono">{connection.version}</span>{/if}
        {#if daemonOk && uptimeMs > 0}<span class="sep">·</span><span>up {duration(uptimeMs)}</span>{/if}
      </div>
    </div>
  </header>

  {#if showSteps}
    <section class="card start">
      <div class="start-head">
        <span class="rocket"><Rocket size={14} /></span>
        <button class="btn ghost icon sm" aria-label="Dismiss" onclick={() => (stepsDismissed = true)}><X size={14} /></button>
        <div>
          <h2>{fresh ? 'Welcome to agent-runtime' : 'Get this workspace running'}</h2>
          <p>A supervised process runtime your AI agents and you can share. Three steps to get going.</p>
        </div>
      </div>
      <ol class="steps">
        {#each steps as step, i (step.title)}
          <li class:done={step.done}>
            <span class="num">{#if step.done}<Check size={14} />{:else}{i + 1}{/if}</span>
            <div class="step-body">
              <strong>{step.title}</strong>
              <span>{step.text}</span>
              {#if i === 0 && !step.done}
                <form class="quick" onsubmit={openQuick}>
                  <div class="input-wrap">
                    <FolderOpen size={14} />
                    <input placeholder="/path/to/project" aria-label="Workspace path" bind:value={quickPath} disabled={scopeState.resolving} />
                  </div>
                  <button class="btn primary" type="submit" disabled={!quickPath.trim() || scopeState.resolving}>
                    {#if scopeState.resolving}<Spinner size={14} />{/if}Open
                  </button>
                </form>
                {#if scopeState.error}<span class="err-text">{scopeState.error}</span>{/if}
              {:else if i === 1 && !step.done}
                <a class="btn sm" href="#/processes" use:router.link={'/processes'}>Go to Processes<ArrowRight size={14} /></a>
              {:else if i === 2 && !step.done}
                <a class="btn sm" href="#/integrations" use:router.link={'/integrations'}>Open Integrations<ArrowRight size={14} /></a>
              {/if}
            </div>
          </li>
        {/each}
      </ol>
    </section>
  {/if}

  {#if !fresh}
    <div class="stat-grid">
      <StatCard label="Running" value={counts.running} sub={counts.starting > 0 ? `${counts.starting} starting` : 'processes up'} icon={Play} tone="ok" href="/processes" />
      <StatCard
        label="Failed"
        value={counts.failed}
        sub={counts.failed > 0 ? 'need attention' : 'all healthy'}
        icon={CircleX}
        tone={counts.failed > 0 ? 'err' : 'neutral'}
        href="/processes"
      />
      <StatCard label="Exited" value={counts.exited} sub={counts.starting > 0 ? `${counts.starting} starting` : 'finished or stopped'} icon={Square} href="/processes" />
      <StatCard
        label="Projects"
        value={scopeState.all ? (stats?.projects ?? scopeState.projects.length) : 1}
        sub={scopeState.all ? `${scopeState.workspaces.length} ${scopeState.workspaces.length === 1 ? 'workspace' : 'workspaces'}` : scopeState.label}
        icon={FolderKanban}
        tone="info"
        href="/projects"
      />
      <StatCard label="Sessions" value={openSessions.length} sub={openSessions.length === 0 ? 'no agents connected' : 'connected clients'} icon={Users} href="/sessions" />
      <StatCard
        label="Log disk"
        value={stats ? bytes(stats.logDiskBytes) : '—'}
        sub={stats && stats.indexLagSeconds > 0 ? `index lag ${stats.indexLagSeconds}s` : 'indexed logs'}
        icon={HardDrive}
        href="/settings"
      />
    </div>

    <div class="cols">
      <div class="col">
        {#if attention.length > 0}
          <section class="card attn">
            <CardHeader title="Needs attention" icon={TriangleAlert} count={attention.length} tone="err" href="/processes" />
            <ul class="list">
              {#each attention as p (p.id)}
                <li class="attn-row">
                  <span class="dot {p.status === 'failed' || p.status === 'crashed' ? 'err' : 'warn'}"></span>
                  <div class="grow">
                    <a class="name" href={`#/processes/${p.id}`} use:router.link={`/processes/${p.id}`}>{procTitle(p)}</a>
                    <span class="sub truncate">{attentionText(p)}</span>
                  </div>
                  <div class="btns">
                    <a class="btn sm" href={`#/logs?p=${p.id}`} use:router.link={`/logs?p=${encodeURIComponent(p.id)}`}><ScrollText size={14} />View logs</a>
                    <button class="btn sm" onclick={() => void restart(p)} disabled={restarting.has(p.id)}>
                      {#if restarting.has(p.id)}<Spinner size={14} />{:else}<RotateCw size={14} />{/if}Restart
                    </button>
                  </div>
                </li>
              {/each}
            </ul>
          </section>
        {:else if scopedProcs.length > 0}
          <section class="card healthy">
            <span class="ok-ic"><CircleCheck size={20} /></span>
            <div>
              <strong>All systems healthy</strong>
              <span>{counts.running} running, nothing failed{counts.exited > 0 ? `, ${counts.exited} exited cleanly` : ''}.</span>
            </div>
          </section>
        {/if}

        <section class="card">
          <CardHeader title="Processes" icon={SquareTerminal} count={scopedProcs.length} href="/processes" />
          {#if shownProcs.length === 0}
            <div class="empty-state small">
              <span class="icon-wrap"><SquareTerminal size={20} /></span>
              <h3>No processes yet</h3>
              <p>Start a process in {scopeState.all ? 'any workspace' : scopeState.label} and it will show up here live.</p>
              <a class="btn primary sm" href="#/processes" use:router.link={'/processes'}>Go to Processes</a>
            </div>
          {:else}
            <ul class="list">
              {#each shownProcs as p (p.id)}
                {@const live = liveOf(p.id)}
                <li class="proc-row">
                  <span class="dot {dotClass(p)}"></span>
                  <div class="grow">
                    <div class="line">
                      <a class="name" href={`#/processes/${p.id}`} use:router.link={`/processes/${p.id}`}>{procTitle(p)}</a>
                      {#if scopeState.all && scopeState.workspaces.length > 1}
                        <span class="ws">{scopeState.workspaceLabel(p.workspaceId)}</span>
                      {/if}
                    </div>
                    <span class="sub mono truncate" title={commandLine(p)}>{commandLine(p)}</span>
                  </div>
                  {#if p.status === 'running' || p.status === 'ready'}
                    <div class="res">
                      <Sparkline values={cpuHistory(p.id)} width={64} height={24} tone="info" label="CPU" />
                      <span class="res-text">
                        <span class="cpu"><Cpu size={12} />{live ? `${live.cpuPercent.toFixed(1)}%` : '—'}</span>
                        <span class="mem">{live ? bytes(live.rssBytes || live.memoryBytes) : '—'}</span>
                      </span>
                    </div>
                  {/if}
                  <div class="tail">
                    {#if p.status === 'running' || p.status === 'ready' || p.status === 'starting'}
                      <span class="up">{uptimeOf(p)}</span>
                    {:else}
                      <span class="badge {p.status === 'failed' || p.status === 'crashed' ? 'err' : ''}">
                        {p.status}{p.exitCode !== null && p.exitCode !== 0 ? ` ${p.exitCode}` : ''}
                      </span>
                      <span class="up muted">{uptimeOf(p)}</span>
                    {/if}
                  </div>
                </li>
              {/each}
            </ul>
            {#if scopedProcs.length > shownProcs.length}
              <div class="more-row">
                <a href="#/processes" use:router.link={'/processes'}>{scopedProcs.length - shownProcs.length} more processes<ArrowRight size={14} /></a>
              </div>
            {/if}
          {/if}
        </section>
      </div>

      <div class="col">
        <section class="card">
          <CardHeader title="Recent activity" icon={Activity} href="/events" />
          {#if recent.length === 0}
            <div class="empty-state small">
              <span class="icon-wrap"><Activity size={20} /></span>
              <h3>Nothing yet</h3>
              <p>Starts, stops, crashes and alerts will appear here as they happen.</p>
            </div>
          {:else}
            <ul class="list">
              {#each recent as e (e.id)}
                {@const k = eventKind(e.type)}
                <li class="ev-row">
                  <ToneBadge icon={k.icon} tone={eventTone(e)} size={20} />
                  <div class="grow">
                    {#if e.processId}
                      <a class="sentence" href={`#/processes/${e.processId}`} use:router.link={`/processes/${e.processId}`}>{eventSentence(e)}</a>
                    {:else}
                      <span class="sentence">{eventSentence(e)}</span>
                    {/if}
                  </div>
                  <span class="when"><RelativeTime ts={e.ts} /></span>
                </li>
              {/each}
            </ul>
          {/if}
        </section>

        <section class="card">
          <CardHeader title="Active sessions" icon={Users} count={openSessions.length} href="/sessions" />
          {#if !sessionsLoaded}
            <div class="loading"><Spinner size={16} /></div>
          {:else if openSessions.length === 0}
            <div class="empty-state small">
              <span class="icon-wrap"><Bot size={20} /></span>
              <h3>No agents connected</h3>
              <p>Sessions appear when an AI agent or CLI connects to the daemon.</p>
              <a class="btn sm" href="#/integrations" use:router.link={'/integrations'}><Plug size={14} />Connect an agent</a>
            </div>
          {:else}
            <ul class="list">
              {#each openSessions.slice(0, 6) as s (s.id)}
                <li class="sess-row">
                  <span class="s-ic">
                    {#if s.kind === 'cli'}<Terminal size={14} />{:else if s.kind === 'gui'}<Monitor size={14} />{:else}<Bot size={14} />{/if}
                  </span>
                  <div class="grow">
                    <span class="name plain">{harnessLabel(s)}</span>
                    <span class="sub truncate">{s.kind}{s.clientPid ? ` · pid ${s.clientPid}` : ''}{s.workspaceId ? ` · ${scopeState.workspaceLabel(s.workspaceId)}` : ''}</span>
                  </div>
                  <span class="seen">
                    <span class="dot" class:ok={sessionLive(s)}></span>
                    <RelativeTime ts={s.lastSeen} />
                  </span>
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      </div>
    </div>
  {/if}
</div>

<style>
  .ov {
    container-type: inline-size;
    gap: var(--space-6);
  }
  .ov .stat-grid {
    grid-template-columns: repeat(6, minmax(0, 1fr));
  }
  @container (max-width: 1000px) {
    .ov .stat-grid {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }
  @container (max-width: 520px) {
    .ov .stat-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  .page-header .subtitle {
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    max-width: 520px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sep {
    color: var(--text-2);
    opacity: 0.6;
  }
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-1);
    color: var(--text-1);
    font-size: var(--fs-sm);
    white-space: nowrap;
  }
  .pill.bad {
    border-color: color-mix(in srgb, var(--err) 40%, transparent);
    color: var(--err);
  }
  .cols {
    display: grid;
    grid-template-columns: minmax(0, 1.25fr) minmax(0, 1fr);
    gap: var(--space-5);
    align-items: start;
  }
  .col {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    min-width: 0;
  }
  @container (max-width: 960px) {
    .cols {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  .card {
    overflow: hidden;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .list > li {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: 0 var(--space-4);
    min-height: 26px;
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }
  .list > li:last-child {
    border-bottom: none;
  }
  .list > li:hover {
    background: var(--bg-hover);
  }
  .grow {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .line {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }
  .name {
    font-weight: 600;
    color: var(--text-0);
    text-decoration: none;
    font-size: var(--fs-sm);
  }
  a.name:hover,
  a.sentence:hover {
    color: var(--accent);
  }
  .name.plain {
    text-transform: capitalize;
  }
  .sub {
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .ws {
    font-size: var(--fs-micro);
    color: var(--text-2);
    padding: 0 6px;
    border: 1px solid var(--border);
    border-radius: 999px;
    white-space: nowrap;
  }
  .btns {
    display: flex;
    gap: var(--space-2);
    flex: none;
  }
  .btn {
    text-decoration: none;
  }
  .attn {
    border-color: color-mix(in srgb, var(--err) 38%, var(--border));
  }
  .attn :global(.card-header) {
    background: color-mix(in srgb, var(--err) 7%, transparent);
    border-bottom-color: color-mix(in srgb, var(--err) 25%, var(--border));
  }
  .attn-row {
    flex-wrap: wrap;
  }
  .healthy {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-5);
    border-color: color-mix(in srgb, var(--ok) 32%, var(--border));
    background: color-mix(in srgb, var(--ok) 6%, var(--bg-1));
  }
  .healthy > div {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: var(--fs-sm);
  }
  .healthy > div span {
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .ok-ic {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    border-radius: 10px;
    color: var(--ok);
    background: color-mix(in srgb, var(--ok) 15%, transparent);
    flex: none;
  }
  .res {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: none;
  }
  .res-text {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    font-variant-numeric: tabular-nums;
    color: var(--text-1);
    min-width: 58px;
    line-height: 1.35;
  }
  .cpu {
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }
  .mem {
    color: var(--text-2);
  }
  .tail {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
    min-width: 72px;
    flex: none;
  }
  .up {
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    font-variant-numeric: tabular-nums;
    color: var(--text-1);
    white-space: nowrap;
  }
  .more-row {
    padding: 10px var(--space-5);
    border-top: 1px solid var(--border);
    font-size: var(--fs-xs);
  }
  .more-row a {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--text-1);
    text-decoration: none;
  }
  .more-row a:hover {
    color: var(--accent);
  }
  .sentence {
    font-size: var(--fs-sm);
    color: var(--text-0);
    text-decoration: none;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .when {
    font-size: var(--fs-xs);
    color: var(--text-2);
    flex: none;
  }
  .s-ic {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    border-radius: 8px;
    background: var(--bg-3);
    color: var(--text-1);
    flex: none;
  }
  .seen {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: var(--fs-xs);
    color: var(--text-2);
    flex: none;
  }
  .empty-state.small {
    padding: 32px var(--space-5);
  }
  .loading {
    display: grid;
    place-items: center;
    padding: 32px;
    color: var(--text-2);
  }
  .start {
    padding: 8px 10px;
    min-height: 56px;
    background: var(--bg-1);
  }
  .start-head {
    display: flex;
    gap: var(--space-4);
    align-items: center;
    margin-bottom: var(--space-5);
  }
  .start-head h2 {
    font-size: var(--fs-lg);
  }
  .start-head p {
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .rocket {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border-radius: 6px;
    color: var(--accent);
    background: var(--accent-subtle);
    flex: none;
  }
  .steps {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    gap: var(--space-4);
  }
  .steps li {
    display: flex;
    gap: var(--space-4);
    padding: var(--space-4);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-1);
    min-width: 0;
  }
  .steps li.done {
    opacity: 0.75;
  }
  .num {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    border: 1px solid var(--border-strong);
    font-size: var(--fs-xs);
    font-weight: 600;
    color: var(--text-1);
    flex: none;
  }
  .done .num {
    background: var(--ok);
    border-color: var(--ok);
    color: var(--accent-fg);
  }
  .step-body {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    align-items: flex-start;
    min-width: 0;
    flex: 1;
  }
  .step-body strong {
    font-size: var(--fs-md);
  }
  .step-body span {
    color: var(--text-2);
    font-size: var(--fs-sm);
    line-height: 1.45;
  }
  .step-body .err-text {
    color: var(--err);
    font-size: var(--fs-xs);
  }
  .quick {
    display: flex;
    gap: var(--space-2);
    width: 100%;
    margin-top: var(--space-1);
  }
  .quick .input-wrap {
    flex: 1;
    min-width: 0;
  }
  @container (max-width: 640px) {
    .list > li {
      flex-wrap: wrap;
    }
    .res {
      display: none;
    }
    .btns {
      width: 100%;
      padding-left: 20px;
    }
  }
</style>
