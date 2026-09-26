<script lang="ts">
  import { onMount } from 'svelte';
  import { ProjectService } from '../../lib/api';
  import { scope } from '../../lib/scope.svelte';

  interface ProjectRow {
    id: string;
    name: string;
    configMode?: string;
    lastUsedAt?: number;
    workspaceCount?: number;
  }
  interface WorkspaceRow {
    id: string;
    path: string;
    confirmed?: boolean;
    lastSeenAt?: number;
  }

  let projects = $state<ProjectRow[]>([]);
  let expanded = $state<Record<string, WorkspaceRow[]>>({});
  let loading = $state(true);
  let newPath = $state('');
  let gcResult = $state<string[] | null>(null);

  async function load() {
    loading = true;
    try {
      const res = await ProjectService.list();
      projects = (res.projects as ProjectRow[]) ?? [];
    } finally {
      loading = false;
    }
  }

  async function toggle(p: ProjectRow) {
    if (expanded[p.id]) {
      const next = { ...expanded };
      delete next[p.id];
      expanded = next;
      return;
    }
    const res = await ProjectService.workspaces(p.id);
    expanded = { ...expanded, [p.id]: (res.workspaces as WorkspaceRow[]) ?? [] };
  }

  async function useWorkspace(w: WorkspaceRow) {
    await scope.resolve(w.path);
  }

  async function forget(p: ProjectRow) {
    if (!confirm(`Forget project "${p.name}"? Workspaces move to a 30-day trash.`)) return;
    await ProjectService.forget(p.id);
    await load();
  }

  async function gc() {
    const res = await ProjectService.gc();
    gcResult = res.removedWorkspaces ?? [];
  }

  async function addPath(e: Event) {
    e.preventDefault();
    if (!newPath.trim()) return;
    await scope.resolve(newPath.trim());
    newPath = '';
    await load();
  }

  onMount(load);
</script>

<div class="page">
  <div class="toolbar">
    <form onsubmit={addPath}>
      <input placeholder="Path to open or link…" bind:value={newPath} aria-label="Workspace path" />
      <button type="submit" disabled={scope.resolving}>{scope.resolving ? 'Opening…' : 'Open'}</button>
    </form>
    <button class="ghost" onclick={gc}>Garbage collect missing workspaces</button>
  </div>
  {#if gcResult}
    <p class="hint">Removed {gcResult.length} missing workspace(s).</p>
  {/if}
  {#if loading}
    <p class="muted">Loading…</p>
  {:else if projects.length === 0}
    <p class="muted">No projects yet.</p>
  {:else}
    <ul class="projects">
      {#each projects as p (p.id)}
        <li>
          <div class="row">
            <button class="expand" onclick={() => toggle(p)}>{expanded[p.id] ? '▾' : '▸'}</button>
            <span class="name">{p.name}</span>
            <span class="meta">{p.workspaceCount ?? 0} workspace(s)</span>
            <span class="meta mono">{p.configMode ?? ''}</span>
            <button class="danger" onclick={() => forget(p)}>Forget</button>
          </div>
          {#if expanded[p.id]}
            <ul class="workspaces">
              {#each expanded[p.id] as w (w.id)}
                <li>
                  <span class="path mono">{w.path}</span>
                  {#if !w.confirmed}<span class="badge">unconfirmed</span>{/if}
                  <button onclick={() => useWorkspace(w)}>Use as scope</button>
                </li>
              {/each}
            </ul>
          {/if}
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
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    margin-bottom: var(--space-5);
  }
  .toolbar form {
    display: flex;
    gap: var(--space-2);
  }
  .toolbar input {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    color: var(--text-0);
    width: 320px;
  }
  .toolbar button,
  .ghost {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-4);
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .hint {
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  .muted {
    color: var(--text-2);
  }
  .projects {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .projects > li {
    border-bottom: 1px solid var(--border);
    padding: var(--space-3) 0;
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }
  .expand {
    background: transparent;
    border: none;
    color: var(--text-1);
    width: 20px;
  }
  .name {
    font-weight: 500;
  }
  .meta {
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .danger {
    margin-left: auto;
    background: transparent;
    border: 1px solid var(--err);
    color: var(--err);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-3);
    font-size: var(--fs-xs);
  }
  .workspaces {
    list-style: none;
    margin: var(--space-2) 0 0 var(--space-8);
    padding: 0;
  }
  .workspaces li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-1) 0;
    font-size: var(--fs-sm);
  }
  .workspaces button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-3);
    color: var(--text-0);
    font-size: var(--fs-xs);
  }
  .badge {
    background: color-mix(in srgb, var(--warn) 20%, transparent);
    color: var(--warn);
    border-radius: var(--radius-sm);
    padding: 0 var(--space-2);
    font-size: var(--fs-xs);
  }
  .mono {
    font-family: var(--font-mono);
  }
</style>
