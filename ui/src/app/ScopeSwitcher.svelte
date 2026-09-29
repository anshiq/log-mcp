<script lang="ts">
  import FolderOpen from '@lucide/svelte/icons/folder-open';
  import ChevronsUpDown from '@lucide/svelte/icons/chevrons-up-down';
  import Check from '@lucide/svelte/icons/check';
  import Layers from '@lucide/svelte/icons/layers';
  import Plus from '@lucide/svelte/icons/plus';
  import { scopeState } from '../lib/state/scope.svelte';

  let open = $state(false);
  let path = $state('');
  let root: HTMLDivElement | null = $state(null);
  let input: HTMLInputElement | null = $state(null);

  function toggle() {
    open = !open;
    if (open) {
      scopeState.error = '';
      void scopeState.refresh();
      queueMicrotask(() => input?.focus());
    }
  }

  function choose(id: string) {
    scopeState.select(id);
    open = false;
  }

  async function submit(e: Event) {
    e.preventDefault();
    if (!path.trim()) return;
    const ok = await scopeState.open(path);
    if (ok) {
      path = '';
      open = false;
    }
  }

  function onWindowClick(e: MouseEvent) {
    if (open && root && !root.contains(e.target as Node)) open = false;
  }

  function onWindowKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') open = false;
  }

  function shorten(p: string): string {
    if (p.length <= 44) return p;
    return '…' + p.slice(p.length - 43);
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={onWindowKey} />

<div class="switcher" bind:this={root}>
  <button class="trigger" class:open onclick={toggle} aria-haspopup="listbox" aria-expanded={open} aria-label="Workspace scope">
    <span class="ico">
      {#if scopeState.all}<Layers size={15} />{:else}<FolderOpen size={15} />{/if}
    </span>
    <span class="text">
      <span class="name">{scopeState.label}</span>
      {#if !scopeState.all && scopeState.path}<span class="path">{shorten(scopeState.path)}</span>{/if}
    </span>
    <ChevronsUpDown size={14} />
  </button>

  {#if open}
    <div class="panel" role="listbox" aria-label="Workspaces">
      <div class="section">Scope</div>
      <button class="item" role="option" aria-selected={scopeState.all} onclick={() => choose('')}>
        <Layers size={15} />
        <span class="col"><span class="n">All workspaces</span><span class="p">Show everything across projects</span></span>
        {#if scopeState.all}<Check size={15} class="tick" />{/if}
      </button>
      {#each scopeState.workspaces as w (w.id)}
        <button class="item" role="option" aria-selected={scopeState.workspaceId === w.id} onclick={() => choose(w.id)}>
          <FolderOpen size={15} />
          <span class="col">
            <span class="n">{scopeState.workspaceLabel(w.id)}</span>
            <span class="p mono">{w.path}</span>
          </span>
          {#if w.missing}<span class="badge warn">missing</span>{/if}
          {#if scopeState.workspaceId === w.id}<Check size={15} class="tick" />{/if}
        </button>
      {/each}
      {#if scopeState.workspaces.length === 0}
        <div class="none">No workspaces yet. Open a project folder below.</div>
      {/if}
      <form class="open-form" onsubmit={submit}>
        <div class="section flush">Open workspace</div>
        <div class="row">
          <input bind:this={input} bind:value={path} placeholder="/path/to/project" aria-label="Workspace path" spellcheck="false" />
          <button class="btn primary" type="submit" disabled={scopeState.resolving || !path.trim()}>
            <Plus size={14} />{scopeState.resolving ? 'Opening…' : 'Open'}
          </button>
        </div>
        {#if scopeState.error}<div class="err">{scopeState.error}</div>{/if}
      </form>
    </div>
  {/if}
</div>

<style>
  .switcher {
    position: relative;
  }
  .trigger {
    gap: 10px;
    padding: 4px 10px 4px 8px;
    min-width: 220px;
    max-width: 380px;
    justify-content: flex-start;
    background: var(--bg-2);
    text-align: left;
    color: var(--text-1);
  }
  .trigger.open {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-subtle);
  }
  .ico {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border-radius: 6px;
    background: var(--accent-subtle);
    color: var(--accent);
    flex: none;
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
    line-height: 1.2;
  }
  .name {
    color: var(--text-0);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .path {
    font-size: 10.5px;
    color: var(--text-2);
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .panel {
    position: absolute;
    top: calc(100% + 8px);
    left: 0;
    width: 420px;
    max-width: 90vw;
    max-height: 70vh;
    overflow: auto;
    padding: var(--space-3);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    z-index: 60;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .section {
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-2);
    padding: 6px 8px 4px;
  }
  .section.flush {
    padding-left: 0;
  }
  .item {
    width: 100%;
    justify-content: flex-start;
    gap: 10px;
    border-color: transparent;
    background: transparent;
    padding: 7px 8px;
    text-align: left;
    color: var(--text-1);
    font-weight: 400;
  }
  .item[aria-selected='true'] {
    background: var(--accent-subtle);
    color: var(--text-0);
  }
  .col {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    line-height: 1.3;
  }
  .n {
    color: var(--text-0);
    font-weight: 550;
  }
  .p {
    font-size: 11px;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item :global(.tick) {
    color: var(--accent);
    flex: none;
  }
  .none {
    padding: 10px 8px;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .open-form {
    margin-top: var(--space-2);
    padding: var(--space-2) var(--space-2) 0;
    border-top: 1px solid var(--border);
  }
  .row {
    display: flex;
    gap: var(--space-3);
    padding-bottom: var(--space-2);
  }
  .row input {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
  }
  .err {
    color: var(--err);
    font-size: var(--fs-xs);
    padding: 0 0 var(--space-2);
  }
</style>
