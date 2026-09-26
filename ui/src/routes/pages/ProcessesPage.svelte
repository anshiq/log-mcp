<script lang="ts">
  import { onMount } from 'svelte';
  import ProcessTable from '../../components/ProcessTable.svelte';
  import ProcessDetail from '../../components/ProcessDetail.svelte';
  import StartProcessDialog from '../../components/StartProcessDialog.svelte';
  import { createProcessStore, type Process } from '../../lib/stores';
  import { ProcessService } from '../../lib/api';
  import { router } from '../../lib/router.svelte';
  import { scope } from '../../lib/scope.svelte';

  const store = createProcessStore();
  let processes = $state<Process[]>([]);
  let showStart = $state(false);

  onMount(() => {
    const off = store.connect('', true);
    const unsub = store.processes.subscribe((m) => {
      processes = [...m.values()];
    });
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && router.match('/processes/:id')) {
        router.navigate('/processes');
      }
    }
    window.addEventListener('keydown', onKey);
    return () => {
      off();
      unsub();
      window.removeEventListener('keydown', onKey);
    };
  });

  async function act(action: string, ids: string[]) {
    for (const id of ids) {
      if (action === 'stop') await ProcessService.stop(id);
      if (action === 'restart') await ProcessService.restart(id);
      if (action === 'remove') await ProcessService.remove(id, true);
    }
  }

  async function signal(sig: string, ids: string[]) {
    if (sig === 'SIGKILL' && !confirm(`Send SIGKILL to ${ids.length} process(es)? This does not allow graceful shutdown.`)) return;
    for (const id of ids) await ProcessService.signal(id, sig);
  }

  function requestStart() {
    if (!scope.workspaceId) {
      alert('Open a workspace from the top bar first.');
      return;
    }
    showStart = true;
  }

  function started(processId: string) {
    showStart = false;
    router.navigate(`/processes/${processId}`);
  }

  const openId = $derived(router.match('/processes/:id')?.params.id ?? '');
</script>

<div class="page">
  <div class="main">
    <ProcessTable
      {processes}
      onAction={act}
      onSignal={signal}
      onOpen={(id) => router.navigate(`/processes/${id}`)}
      onStart={requestStart}
    />
  </div>
  {#if openId}
    <div class="drawer">
      <ProcessDetail processId={openId} onClose={() => router.navigate('/processes')} />
    </div>
  {/if}
  {#if showStart}
    <StartProcessDialog workspaceId={scope.workspaceId} onClose={() => (showStart = false)} onStarted={started} />
  {/if}
</div>

<style>
  .page {
    display: flex;
    height: 100%;
  }
  .main {
    flex: 1;
    min-width: 0;
  }
  .drawer {
    width: 480px;
    flex: none;
  }
</style>
