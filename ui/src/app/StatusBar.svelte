<script lang="ts">
  import { connection } from '../lib/state/connection.svelte';
  import { processes } from '../lib/state/processes.svelte';
  import { scopeState } from '../lib/state/scope.svelte';

  const version = $derived(connection.version ? (connection.version.startsWith('v') ? connection.version : `v${connection.version}`) : '');
</script>

<footer class="statusbar" aria-live="polite">
  <span class="item">
    <span class="dot" class:ok={connection.reachable} class:warn={!connection.reachable}></span>
    {connection.reachable ? 'Connected' : 'Disconnected'}
  </span>
  <span class="item">
    <span class="dot ok"></span>{processes.counts.running} running
  </span>
  <span class="item">
    <span class="dot" class:err={processes.counts.failed > 0}></span>{processes.counts.failed} failed
  </span>
  <span class="spacer"></span>
  <span class="item scope mono" title={scopeState.path}>{scopeState.all ? 'All workspaces' : scopeState.path}</span>
  {#if version}<span class="item mono">{version}</span>{/if}
</footer>

<style>
  .statusbar {
    display: flex;
    gap: var(--space-5);
    align-items: center;
    height: var(--statusbar-h);
    flex: none;
    padding: 0 var(--space-5);
    background: var(--bg-1);
    border-top: 1px solid var(--border);
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .item {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    white-space: nowrap;
  }
  .item .dot {
    width: 7px;
    height: 7px;
    box-shadow: none;
  }
  .spacer {
    flex: 1;
  }
  .scope {
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 50vw;
  }
</style>
