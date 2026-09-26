<script lang="ts">
  import { onMount } from 'svelte';
  import ProcessTable from '../components/ProcessTable.svelte';
  import ProcessDetail from '../components/ProcessDetail.svelte';
  import Login from './Login.svelte';
  import { createProcessStore, type Process } from '../lib/stores';
  import { ProcessService, ProjectService, SystemService, hasAuthToken, setUnauthorizedHandler } from '../lib/api';

  const store = createProcessStore();
  let processes = $state<Process[]>([]);
  let streamState = $state<'connecting' | 'open' | 'reconnecting' | 'closed'>('connecting');
  let selectedProcess = $state('');
  let tab = $state<'processes' | 'config'>('processes');
  let workspacePath = $state('');
  let workspaceId = $state('');
  let projectName = $state('');
  let resolveError = $state('');
  let resolving = $state(false);
  let daemonVersion = $state('');
  let authed = $state(__APP_TARGET__ !== 'web' || hasAuthToken());

  onMount(() => {
    if (__APP_TARGET__ === 'web') {
      setUnauthorizedHandler(() => (authed = false));
    }
    if (!authed) return;
    void SystemService.version().then((v) => (daemonVersion = v.daemonVersion));
    const off = store.connect('', true);
    const unsub = store.processes.subscribe((m) => {
      processes = [...m.values()];
    });
    const unsubState = store.streamState.subscribe((s) => (streamState = s));
    return () => {
      off();
      unsub();
      unsubState();
    };
  });

  async function resolveWorkspace() {
    if (!workspacePath.trim()) return;
    resolving = true;
    resolveError = '';
    try {
      const res = await ProjectService.resolve(workspacePath.trim());
      workspaceId = res.workspaceId;
      projectName = res.projectName || workspacePath.trim();
    } catch (err) {
      resolveError = err instanceof Error ? err.message : String(err);
    } finally {
      resolving = false;
    }
  }

  async function act(action: string, ids: string[]) {
    for (const id of ids) {
      if (action === 'stop') await ProcessService.stop(id);
      if (action === 'restart') await ProcessService.restart(id);
      if (action === 'remove') await ProcessService.remove(id, true);
    }
  }

  function openProcess(id: string) {
    selectedProcess = id;
  }
</script>

{#if !authed}
  <Login onAuthed={() => (authed = true)} />
{:else}
  <main>
    <header class="topbar">
      <span class="brand">agent-runtime</span>
      <form class="scope" onsubmit={(e) => (e.preventDefault(), resolveWorkspace())}>
        <input placeholder="Workspace path…" bind:value={workspacePath} aria-label="Workspace path" />
        <button type="submit" disabled={resolving}>{resolving ? 'Resolving…' : 'Open'}</button>
      </form>
      {#if projectName}
        <span class="scope-name">{projectName}</span>
      {/if}
      {#if resolveError}
        <span class="scope-error">{resolveError}</span>
      {/if}
      <div class="spacer"></div>
      <span class="status" class:ok={streamState === 'open'} class:bad={streamState !== 'open'}>
        <span class="dot"></span>
        {daemonVersion ? `daemon ${daemonVersion}` : 'connecting…'}
      </span>
    </header>
    <div class="body">
      <nav class="sidebar">
        <button class:active={tab === 'processes'} onclick={() => (tab = 'processes')}>Processes</button>
        <button class:active={tab === 'config'} onclick={() => (tab = 'config')}>Config</button>
      </nav>
      <div class="content">
        {#if tab === 'processes'}
          <ProcessTable {processes} onAction={act} onOpen={openProcess} />
        {:else}
          {#await import('../components/ConfigEditor.svelte') then { default: ConfigEditor }}
            <ConfigEditor {workspaceId} />
          {/await}
        {/if}
      </div>
      {#if selectedProcess}
        <div class="drawer">
          <ProcessDetail processId={selectedProcess} onClose={() => (selectedProcess = '')} />
        </div>
      {/if}
    </div>
  </main>
{/if}

<style>
  main {
    display: flex;
    flex-direction: column;
    height: 100vh;
  }
  .topbar {
    height: var(--topbar-h);
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: 0 var(--space-5);
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
  }
  .brand {
    font-weight: 600;
    color: var(--text-0);
  }
  .scope {
    display: flex;
    gap: var(--space-2);
  }
  .scope input {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    color: var(--text-0);
    width: 260px;
  }
  .scope button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-4);
    color: var(--text-0);
  }
  .scope-name {
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  .scope-error {
    color: var(--err);
    font-size: var(--fs-sm);
  }
  .spacer {
    flex: 1;
  }
  .status {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  .status .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--neutral);
  }
  .status.ok .dot {
    background: var(--ok);
  }
  .status.bad .dot {
    background: var(--warn);
  }
  .body {
    flex: 1;
    display: flex;
    min-height: 0;
  }
  .sidebar {
    width: var(--sidebar-w);
    flex: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-4);
    background: var(--bg-1);
    border-right: 1px solid var(--border);
  }
  .sidebar button {
    text-align: left;
    background: transparent;
    border: none;
    border-radius: var(--radius);
    padding: var(--space-3) var(--space-4);
    color: var(--text-1);
    font-size: var(--fs-md);
  }
  .sidebar button.active {
    background: var(--accent-subtle);
    color: var(--text-0);
  }
  .sidebar button:hover:not(.active) {
    background: var(--bg-2);
  }
  .content {
    flex: 1;
    min-width: 0;
    overflow: auto;
  }
  .drawer {
    width: 480px;
    flex: none;
  }
</style>
