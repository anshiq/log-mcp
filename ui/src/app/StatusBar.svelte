<script lang="ts">
  import { connection } from '../lib/state/connection.svelte';
  import { processes } from '../lib/state/processes.svelte';
  import { scopeState } from '../lib/state/scope.svelte';

  const version = $derived(connection.version ? (connection.version.startsWith('v') ? connection.version : `v${connection.version}`) : '');
</script>

<footer class="statusbar" class:offline={!connection.reachable} aria-live="polite">
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
    gap: 12px;
    align-items: center;
    height: var(--statusbar-h);
    flex: none;
    padding: 0 12px;
    background: var(--bg-1);
    border-top: 1px solid var(--border);
    font-size: var(--fs-micro);
    color: var(--text-2);
  }
  .statusbar.offline {
    background: color-mix(in srgb, var(--err) 12%, var(--bg-1));
  }
  .item {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
    font-family: var(--font-mono);
  }
  .item .dot {
    width: 6px;
    height: 6px;
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
