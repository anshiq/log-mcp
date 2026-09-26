<script lang="ts">
  import ProcessTable from '../components/ProcessTable.svelte';
  import LogViewer from '../components/LogViewer.svelte';
  import ConfigEditor from '../components/ConfigEditor.svelte';
  import { createProcessStore, type Process } from '../lib/stores';
  import { LogService, ProcessService } from '../lib/api';
  import { onMount } from 'svelte';

  const store = createProcessStore();
  let processes = $state<Process[]>([]);
  let selected = $state<string>('');
  let lines = $state<{ line: string; stream?: string }[]>([]);
  let tab = $state<'processes' | 'logs' | 'config'>('processes');
  let workspaceId = $state('');

  onMount(() => {
    const off = store.connect('', true);
    const unsub = store.processes.subscribe((m) => {
      processes = [...m.values()];
    });
    return () => {
      off();
      unsub();
    };
  });

  async function select(id: string) {
    selected = id;
    tab = 'logs';
    const res = (await LogService.get(id, 500)) as { entries?: { line: string; stream: string }[] };
    lines = (res.entries ?? []).map((e) => ({ line: e.line, stream: e.stream }));
  }

  async function act(action: string, ids: string[]) {
    for (const id of ids) {
      if (action === 'stop') await ProcessService.stop(id);
      if (action === 'restart') await ProcessService.restart(id);
    }
  }
</script>

<main>
  <nav>
    <button onclick={() => (tab = 'processes')}>Processes</button>
    <button onclick={() => (tab = 'logs')}>Logs</button>
    <button onclick={() => (tab = 'config')}>Config</button>
  </nav>
  {#if tab === 'processes'}
    <ProcessTable {processes} onAction={act} />
    <button onclick={() => selected && select(selected)}>Open logs</button>
  {:else if tab === 'logs'}
    <LogViewer {lines} />
  {:else}
    <ConfigEditor {workspaceId} />
  {/if}
</main>

<style>
  main { display: flex; flex-direction: column; height: 100vh; }
  nav { display: flex; gap: 8px; padding: 8px; border-bottom: 1px solid #30363d; }
</style>
