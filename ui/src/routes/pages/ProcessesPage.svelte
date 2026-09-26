<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import ProcessTable from '../../components/ProcessTable.svelte';
  import ProcessDetail from '../../components/ProcessDetail.svelte';
  import { createProcessStore, type Process } from '../../lib/stores';
  import { ProcessService } from '../../lib/api';
  import { router } from '../../lib/router.svelte';

  const store = createProcessStore();
  let processes = $state<Process[]>([]);

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

  async function act(action: string, ids: string[]) {
    for (const id of ids) {
      if (action === 'stop') await ProcessService.stop(id);
      if (action === 'restart') await ProcessService.restart(id);
      if (action === 'remove') await ProcessService.remove(id, true);
    }
  }

  const openId = $derived(router.match('/processes/:id')?.params.id ?? '');
</script>

<div class="page">
  <div class="main">
    <ProcessTable {processes} onAction={act} onOpen={(id) => router.navigate(`/processes/${id}`)} />
  </div>
  {#if openId}
    <div class="drawer">
      <ProcessDetail processId={openId} onClose={() => router.navigate('/processes')} />
    </div>
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
