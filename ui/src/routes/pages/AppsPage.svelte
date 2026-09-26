<script lang="ts">
  import { onMount } from 'svelte';
  import { ConfigService, ProcessService } from '../../lib/api';
  import { scope } from '../../lib/scope.svelte';
  import { router } from '../../lib/router.svelte';
  import { createProcessStore, type Process } from '../../lib/stores';

  interface AppRow {
    name: string;
    command: string[];
    workdir: string;
    dependsOn: string[];
    autostart: boolean;
    lifetime: string;
  }

  const store = createProcessStore();
  let processes = $state<Process[]>([]);
  let apps = $state<AppRow[]>([]);
  let loading = $state(false);
  let selected = $state(new Set<string>());
  let stackResult = $state<{ started?: unknown[]; failed?: string; order?: string[] } | null>(null);
  let starting = $state(false);

  async function loadApps() {
    if (!scope.workspaceId) {
      apps = [];
      return;
    }
    loading = true;
    try {
      const cfg = await ConfigService.get(scope.workspaceId);
      const raw = (cfg.apps as Record<
        string,
        { command?: string[]; workdir?: string; depends_on?: string[]; autostart?: boolean; lifetime?: string }
      >) ?? {};
      apps = Object.entries(raw).map(([name, a]) => ({
        name,
        command: a.command ?? [],
        workdir: a.workdir ?? '',
        dependsOn: a.depends_on ?? [],
        autostart: !!a.autostart,
        lifetime: a.lifetime ?? 'persistent'
      }));
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    const off = store.connect(scope.workspaceId, false);
    const unsub = store.processes.subscribe((m) => {
      processes = [...m.values()];
    });
    void loadApps();
    return () => {
      off();
      unsub();
    };
  });

  $effect(() => {
    void scope.workspaceId;
    void loadApps();
  });

  function runningFor(appName: string): Process | undefined {
    return processes.find((p) => p.app === appName);
  }

  async function startOne(app: AppRow) {
    await ProcessService.start({ workspaceId: scope.workspaceId, app: app.name });
  }

  function toggle(name: string) {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    selected = next;
  }

  async function startStack() {
    starting = true;
    stackResult = null;
    try {
      stackResult = await ProcessService.startStack(scope.workspaceId, [...selected]);
    } finally {
      starting = false;
    }
  }
</script>

<div class="page">
  {#if !scope.workspaceId}
    <p class="hint">Open a workspace from the top bar to see its configured apps.</p>
  {:else if loading}
    <p class="muted">Loading…</p>
  {:else if apps.length === 0}
    <p class="muted">No apps configured for this workspace.</p>
  {:else}
    <div class="toolbar">
      <span>{selected.size} selected</span>
      <button disabled={selected.size === 0 || starting} onclick={startStack}>
        {starting ? 'Starting…' : 'Start stack'}
      </button>
    </div>
    {#if stackResult}
      <div class="stack-result" class:err={!!stackResult.failed}>
        {#if stackResult.failed}
          Failed at <strong>{stackResult.failed}</strong>
        {:else}
          Started in order: {stackResult.order?.join(' → ')}
        {/if}
      </div>
    {/if}
    <ul class="apps">
      {#each apps as app (app.name)}
        {@const running = runningFor(app.name)}
        <li>
          <input type="checkbox" checked={selected.has(app.name)} onchange={() => toggle(app.name)} />
          <div class="info">
            <div class="row">
              <span class="name">{app.name}</span>
              {#if app.autostart}<span class="badge">autostart</span>{/if}
              <span class="badge">{app.lifetime}</span>
              {#if running}<span class="badge ok">{running.status}</span>{/if}
            </div>
            <span class="command mono">{app.command.join(' ')}</span>
            {#if app.dependsOn.length}
              <span class="deps">depends on: {app.dependsOn.join(', ')}</span>
            {/if}
          </div>
          <div class="actions">
            {#if running}
              <button onclick={() => router.navigate(`/processes/${running.id}`)}>View</button>
            {:else}
              <button onclick={() => startOne(app)}>Start</button>
            {/if}
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .page {
    padding: var(--space-5);
    overflow: auto;
    height: 100%;
  }
  .hint,
  .muted {
    color: var(--text-2);
  }
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    margin-bottom: var(--space-4);
  }
  .toolbar button {
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-4);
    font-size: var(--fs-sm);
  }
  .toolbar button:disabled {
    opacity: 0.5;
  }
  .stack-result {
    padding: var(--space-3) var(--space-4);
    background: var(--bg-2);
    border-radius: var(--radius);
    margin-bottom: var(--space-4);
    font-size: var(--fs-sm);
  }
  .stack-result.err {
    color: var(--err);
  }
  .apps {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .apps li {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-3) 0;
    border-bottom: 1px solid var(--border);
  }
  .info {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .name {
    font-weight: 500;
  }
  .badge {
    background: var(--bg-3);
    border-radius: var(--radius-sm);
    padding: 0 var(--space-2);
    font-size: var(--fs-xs);
    color: var(--text-1);
  }
  .badge.ok {
    color: var(--ok);
  }
  .command {
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .deps {
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .actions button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-4);
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
</style>
