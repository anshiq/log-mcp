<script lang="ts">
  import { scope } from '../../lib/scope.svelte';
  import { router } from '../../lib/router.svelte';

  const onRevisions = $derived(router.match('/config/revisions') !== null);
</script>

<div class="page">
  <div class="tabs">
    <button class:active={!onRevisions} onclick={() => router.navigate('/config')}>Editor</button>
    <button class:active={onRevisions} onclick={() => router.navigate('/config/revisions')}>Revisions</button>
  </div>
  {#if !scope.workspaceId}
    <p class="hint">Open a workspace from the top bar to edit its config.</p>
  {:else if onRevisions}
    {#await import('./RevisionsPanel.svelte') then { default: RevisionsPanel }}
      <RevisionsPanel projectId={scope.projectId} />
    {/await}
  {:else}
    {#await import('../../components/ConfigEditor.svelte') then { default: ConfigEditor }}
      <ConfigEditor workspaceId={scope.workspaceId} />
    {/await}
  {/if}
</div>

<style>
  .page {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .tabs {
    display: flex;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-5);
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
  }
  .tabs button {
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-4);
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  .tabs button.active {
    background: var(--accent-subtle);
    color: var(--text-0);
  }
  .hint {
    padding: var(--space-5);
    color: var(--text-2);
  }
</style>
