<script lang="ts">
  import type { Component } from 'svelte';
  import FolderOpen from '@lucide/svelte/icons/folder-open';
  import FolderPlus from '@lucide/svelte/icons/folder-plus';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import Spinner from '../lib/ui/Spinner.svelte';
  import { scopeState } from '../lib/state/scope.svelte';
  import { processes } from '../lib/state/processes.svelte';

  let {
    title,
    body,
    icon: Icon = FolderOpen
  }: { title: string; body: string; icon?: Component<{ size?: number }> } = $props();

  let path = $state('');

  const items = $derived(
    scopeState.workspaces.map((w) => {
      const procs = processes.byWorkspace(w.id);
      return {
        ...w,
        label: scopeState.workspaceLabel(w.id),
        running: procs.filter((p) => p.status === 'running' || p.status === 'ready' || p.status === 'starting').length
      };
    })
  );

  async function open(e: Event) {
    e.preventDefault();
    await scopeState.open(path);
    if (!scopeState.error) path = '';
  }
</script>

<div class="picker">
  <div class="intro">
    <div class="icon"><Icon size={22} /></div>
    <h2>{title}</h2>
    <p>{body}</p>
  </div>

  {#if items.length > 0}
    <ul class="list">
      {#each items as w (w.id)}
        <li>
        <button class="ws" disabled={w.missing} onclick={() => scopeState.select(w.id)}>
          <span class="glyph"><FolderOpen size={16} /></span>
          <span class="meta">
            <span class="name">{w.label}</span>
            <span class="path mono truncate" title={w.path}>{w.path}</span>
          </span>
          {#if w.missing}
            <span class="badge warn">missing</span>
          {:else if w.running > 0}
            <span class="badge ok"><span class="dot ok"></span>{w.running} running</span>
          {/if}
          <ChevronRight size={16} />
        </button>
        </li>
      {/each}
    </ul>
  {:else if scopeState.loaded}
    <p class="none">No workspaces yet. Open a project folder to get started.</p>
  {/if}

  <form class="open" onsubmit={open}>
    <div class="input-wrap">
      <FolderPlus size={15} />
      <input bind:value={path} placeholder="Open another folder, e.g. /home/me/projects/app" aria-label="Open workspace by path" spellcheck="false" />
    </div>
    <button class="btn primary" type="submit" disabled={!path.trim() || scopeState.resolving}>
      {#if scopeState.resolving}<Spinner size={14} />{/if}
      Open
    </button>
  </form>
  {#if scopeState.error}
    <p class="err-text">{scopeState.error}</p>
  {/if}
</div>

<style>
  .picker {
    width: min(560px, 100%);
    margin: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    padding: var(--space-6) 0;
  }
  .intro {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: var(--space-2);
  }
  .icon {
    width: 48px;
    height: 48px;
    border-radius: 14px;
    display: grid;
    place-items: center;
    background: var(--accent-subtle);
    color: var(--accent);
    margin-bottom: var(--space-3);
  }
  h2 {
    font-size: var(--fs-xl);
  }
  .intro p {
    color: var(--text-2);
    font-size: var(--fs-sm);
    max-width: 420px;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    max-height: 320px;
    overflow: auto;
  }
  .ws {
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-5);
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    text-align: left;
    color: var(--text-2);
    transition:
      border-color var(--dur-fast) var(--ease),
      background var(--dur-fast) var(--ease),
      transform var(--dur-fast) var(--ease);
  }
  .ws:hover:not(:disabled) {
    border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
    background: var(--bg-2);
    color: var(--accent);
  }
  .ws:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }
  .glyph {
    width: 32px;
    height: 32px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: var(--bg-3);
    color: var(--text-1);
    flex: none;
  }
  .meta {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .name {
    color: var(--text-0);
    font-weight: 600;
    font-size: var(--fs-md);
  }
  .path {
    color: var(--text-2);
    font-size: var(--fs-xs);
    direction: rtl;
    text-align: left;
  }
  .none {
    text-align: center;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .open {
    display: flex;
    gap: var(--space-3);
  }
  .open .input-wrap {
    flex: 1;
    min-width: 0;
  }
  .err-text {
    color: var(--err);
    font-size: var(--fs-sm);
    text-align: center;
  }
</style>
