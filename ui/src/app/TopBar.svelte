<script lang="ts">
  import { scope } from '../lib/scope.svelte';
  import { scopeState } from '../lib/state/scope.svelte';
  import { palette } from '../lib/palette.svelte';
  import { connection } from '../lib/state/connection.svelte';
  import { prefs } from '../lib/state/prefs.svelte';
  import Combobox from '../lib/ui/Combobox.svelte';

  let { pathInput = $bindable('') }: { pathInput?: string } = $props();
  let opts = $derived([
    { value: 'all', label: 'All workspaces' },
    ...scopeState.workspaces.map((w) => ({ value: w.id, label: w.path, hint: w.id }))
  ]);
  let sel = $state('all');

  function applyScope(v: string) {
    if (v === 'all') scopeState.set(scopeState.projectId, '', true);
    else scopeState.set(scopeState.projectId, v, false);
  }

  function toggleTheme() {
    prefs.set('theme', prefs.data.theme === 'dark' ? 'light' : 'dark');
  }
</script>

<header class="topbar">
  <span class="brand">agent-runtime</span>
  <form class="scope" onsubmit={(e) => e.preventDefault()}>
    <input placeholder="Workspace path…" bind:value={pathInput} aria-label="Workspace path" />
    <button type="button" onclick={() => void scope.resolve(pathInput)}>{scope.resolving ? 'Opening…' : 'Open'}</button>
  </form>
  {#if scope.projectName}
    <span class="scope-name">{scope.projectName}</span>
  {/if}
  {#if scope.error}
    <span class="scope-error">{scope.error}</span>
  {/if}
  <div class="combo"><Combobox options={opts} bind:value={sel} placeholder="Scope…" /></div>
  <button class="side" onclick={() => applyScope(sel)}>Set scope</button>
  <div class="spacer"></div>
  <button class="palette-trigger" onclick={() => void palette.show()}><span>Search…</span><kbd>⌘K</kbd></button>
  <button onclick={toggleTheme} aria-label="Toggle theme">{prefs.data.theme === 'dark' ? '☾' : '☀'}</button>
  <span class="status" class:ok={connection.reachable} title={connection.socketPath}>
    <span class="dot"></span>{connection.version ? `daemon ${connection.version}` : 'connecting…'}
  </span>
</header>

<style>
  .topbar { height: var(--topbar-h); flex: none; display: flex; align-items: center; gap: var(--space-3); padding: 0 var(--space-4); border-bottom: 1px solid var(--border); background: var(--bg-1); }
  .brand { font-weight: 600; }
  .scope { display: flex; gap: var(--space-2); }
  .scope input { background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: var(--space-2); width: 200px; color: var(--text-0); }
  .scope-name { color: var(--text-1); font-size: var(--fs-sm); }
  .scope-error { color: var(--err); font-size: var(--fs-sm); }
  .combo { min-width: 200px; }
  .spacer { flex: 1; }
  .palette-trigger { display: flex; gap: var(--space-2); background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--radius); color: var(--text-2); }
  .status { display: flex; gap: var(--space-2); align-items: center; font-size: var(--fs-sm); color: var(--text-1); }
  .status .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--warn); }
  .status.ok .dot { background: var(--ok); }
  .side, .topbar > button { background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--radius); color: var(--text-0); padding: var(--space-2) var(--space-3); }
</style>
