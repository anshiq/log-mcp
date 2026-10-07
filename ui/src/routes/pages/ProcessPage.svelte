<script lang="ts">
  import { untrack } from 'svelte';
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';
  import ProcessDetail, { type DetailTab } from '../../components/ProcessDetail.svelte';
  import ProcessConfirm from '../../components/ProcessConfirm.svelte';
  import { processes } from '../../lib/state/processes.svelte';
  import { router } from '../../lib/router.svelte';
  import { toasts } from '../../lib/toasts.svelte';
  import { getPlatform } from '../../lib/platform';
  import { displayName } from '../../components/processView';

  const TABS: DetailTab[] = ['logs', 'info', 'env', 'resources', 'terminal'];

  function tabFromParams(v: string | undefined): DetailTab {
    return (TABS as string[]).includes(v ?? '') ? (v as DetailTab) : 'logs';
  }

  const match = $derived(router.match('/processes/:id'));
  const processId = $derived(match?.params['id'] ?? '');
  const urlTab = $derived(tabFromParams(match?.params['tab']));

  let tab = $state<DetailTab>('logs');
  let syncedFor = $state('');

  $effect(() => {
    const id = processId;
    const t = urlTab;
    untrack(() => {
      if (syncedFor !== `${id}?${t}`) {
        syncedFor = `${id}?${t}`;
        tab = t;
      }
    });
  });

  function setTab(next: DetailTab) {
    tab = next;
    syncedFor = `${processId}?${next}`;
    void router.navigate(`/processes/${processId}?tab=${next}`, { replace: true });
  }

  const process = $derived(processes.map.get(processId));
  const title = $derived(process ? displayName(process) : processId || 'Process');

  function goBack() {
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
    if (ids.includes(processId)) goBack();
  }

  function onKey(e: KeyboardEvent) {
    if (e.key !== 'Escape' || e.defaultPrevented) return;
    const target = e.target as HTMLElement | null;
    if (target?.closest('.xterm')) return;
    if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return;
    goBack();
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="page fill proc-page">
  <div class="crumbs">
    <button type="button" class="btn ghost sm back" onclick={goBack} aria-label="Back to processes">
      <ArrowLeft size={14} /> Processes
    </button>
    <span class="sep">/</span>
    <span class="cur truncate" title={title}>{title}</span>
  </div>

  <div class="card detail-card">
    {#key processId}
      <ProcessDetail
        {processId}
        {process}
        bind:tab
        onTabChange={setTab}
        onClose={goBack}
        onCopyId={copyId}
        onRemoved={afterRemove}
        fullPage
      />
    {/key}
  </div>
</div>

<ProcessConfirm />

<style>
  .proc-page {
    gap: var(--space-3);
    padding-bottom: var(--space-6);
  }
  .crumbs {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .crumbs .back {
    gap: 6px;
  }
  .crumbs .sep {
    opacity: 0.6;
  }
  .crumbs .cur {
    color: var(--text-0);
    font-weight: 600;
    min-width: 0;
  }
  .detail-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    padding: 0;
  }
  .detail-card > :global(*) {
    flex: 1;
    min-height: 0;
  }
</style>
