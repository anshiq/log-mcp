<script lang="ts" module>
  export type DetailTab = 'logs' | 'info' | 'env' | 'resources' | 'terminal';
</script>

<script lang="ts">
  import { onMount } from 'svelte';
  import X from '@lucide/svelte/icons/x';
  import Copy from '@lucide/svelte/icons/copy';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import Square from '@lucide/svelte/icons/square';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import Info from '@lucide/svelte/icons/info';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import Activity from '@lucide/svelte/icons/activity';
  import SquareTerminal from '@lucide/svelte/icons/square-terminal';
  import SearchX from '@lucide/svelte/icons/search-x';
  import type { Component } from 'svelte';
  import type { Process } from '../lib/api/types';
  import { scopeState } from '../lib/state/scope.svelte';
  import ProcessStatus from './ProcessStatus.svelte';
  import ProcessLogs from './ProcessLogs.svelte';
  import ProcessInfo from './ProcessInfo.svelte';
  import ProcessEnv from './ProcessEnv.svelte';
  import ProcessResources from './ProcessResources.svelte';
  import TerminalTab from './Terminal.svelte';
  import ActionMenu from './ActionMenu.svelte';
  import { processMenu } from './processMenu';
  import { procActions } from './processActions.svelte';
  import { commandLine, displayName, isLive } from './processView';

  let {
    processId,
    process,
    tab = $bindable('logs'),
    onClose,
    onCopyId,
    onRemoved
  }: {
    processId: string;
    process?: Process;
    tab?: DetailTab;
    onClose: () => void;
    onCopyId: (id: string) => void;
    onRemoved: (ids: string[]) => void;
  } = $props();

  let waited = $state(false);
  const tabs: { id: DetailTab; label: string; icon: Component<{ size?: number | string }> }[] = [
    { id: 'logs', label: 'Logs', icon: ScrollText },
    { id: 'info', label: 'Overview', icon: Info },
    { id: 'env', label: 'Env', icon: KeyRound },
    { id: 'resources', label: 'Resources', icon: Activity },
    { id: 'terminal', label: 'Terminal', icon: SquareTerminal }
  ];

  onMount(() => {
    const t = setTimeout(() => (waited = true), 2500);
    return () => clearTimeout(t);
  });

  const live = $derived(process ? isLive(process) : false);
  const pending = $derived(procActions.busy.get(processId) ?? '');
  const line = $derived(process ? commandLine(process) : '');
  const items = $derived(
    process ? processMenu(process, { onCopyId, afterRemove: onRemoved }) : []
  );

  function tabKey(e: KeyboardEvent) {
    const idx = tabs.findIndex((t) => t.id === tab);
    let next = idx;
    if (e.key === 'ArrowRight') next = (idx + 1) % tabs.length;
    else if (e.key === 'ArrowLeft') next = (idx - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = tabs.length - 1;
    else return;
    e.preventDefault();
    tab = tabs[next]!.id;
    (e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('[role="tab"]')[next]?.focus();
  }
</script>

<div class="detail">
  <header class="head">
    <div class="title">
      <div class="row1">
        <h2 class="name" title={process ? displayName(process) : processId}>{process ? displayName(process) : 'Process'}</h2>
        {#if process}<ProcessStatus {process} {pending} />{/if}
      </div>
      <div class="row2">
        <button type="button" class="idchip mono" title="Copy process id" aria-label="Copy process id" onclick={() => onCopyId(processId)}>
          <Copy size={11} />{processId}
        </button>
        {#if process}
          <span class="dotsep"></span>
          <span class="ws truncate">{scopeState.workspaceLabel(process.workspaceId)}</span>
        {/if}
      </div>
    </div>
    <div class="head-actions">
      {#if process}
        <button
          type="button"
          class="btn sm"
          title="Restart"
          disabled={!!pending}
          onclick={() => void procActions.restart([processId])}
        >
          <RotateCw size={13} /> Restart
        </button>
        <button
          type="button"
          class="btn sm stop"
          title="Stop"
          disabled={!!pending || !live}
          onclick={() => void procActions.stop([processId])}
        >
          <Square size={12} /> Stop
        </button>
        <ActionMenu {items} label="More process actions" triggerClass="btn icon sm">
          <Ellipsis size={15} />
        </ActionMenu>
      {/if}
      <button type="button" class="btn ghost icon sm" aria-label="Close details" title="Close (Esc)" onclick={onClose}>
        <X size={16} />
      </button>
    </div>
  </header>

  {#if process && line}
    <div class="cmdrow mono truncate" title={line}>{line}</div>
  {/if}

  <div class="tablist" role="tablist" aria-label="Process details" tabindex="-1" onkeydown={tabKey}>
    {#each tabs as t (t.id)}
      {@const Icon = t.icon}
      <button
        type="button"
        role="tab"
        id="tab-{t.id}"
        class="tab"
        class:active={tab === t.id}
        aria-selected={tab === t.id}
        aria-controls="panel"
        tabindex={tab === t.id ? 0 : -1}
        onclick={() => (tab = t.id)}
      >
        <Icon size={13} />{t.label}
      </button>
    {/each}
  </div>

  <div class="panel" id="panel" role="tabpanel" aria-labelledby="tab-{tab}">
    {#if !process}
      {#if waited}
        <div class="missing">
          <div class="icon-wrap"><SearchX size={20} /></div>
          <h3>Process not found</h3>
          <p>It may have been removed, or it belongs to a workspace that is not loaded.</p>
          <button type="button" class="btn" onclick={onClose}>Close</button>
        </div>
      {:else}
        <div class="missing"><span class="muted">Loading process…</span></div>
      {/if}
    {:else if tab === 'logs'}
      <ProcessLogs {processId} {live} />
    {:else if tab === 'info'}
      <ProcessInfo {process} />
    {:else if tab === 'env'}
      <ProcessEnv {processId} />
    {:else if tab === 'resources'}
      <ProcessResources {processId} {live} />
    {:else}
      <TerminalTab {processId} interactive={live} />
    {/if}
  </div>
</div>

<style>
  .detail {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-1);
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-4) var(--space-3) var(--space-5);
  }
  .title {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .row1 {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }
  .name {
    font-size: var(--fs-lg);
    font-weight: 600;
    letter-spacing: -0.01em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .row2 {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .idchip {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 1px 7px;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-1);
    font-size: 10.5px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .idchip:hover {
    color: var(--text-0);
    border-color: var(--border-strong);
  }
  .dotsep {
    width: 3px;
    height: 3px;
    border-radius: 50%;
    background: var(--text-2);
    flex: none;
  }
  .head-actions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
  }
  .stop:hover:not(:disabled) {
    color: var(--err);
  }
  .cmdrow {
    margin: 0 var(--space-5) var(--space-3);
    padding: 5px 10px;
    border-radius: var(--radius-sm);
    background: var(--bg-0);
    border: 1px solid var(--border);
    color: var(--text-1);
    font-size: 11.5px;
  }
  .tablist {
    display: flex;
    gap: 2px;
    padding: 0 var(--space-4);
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tab {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 9px 10px;
    margin-bottom: -1px;
    border: none;
    border-bottom: 2px solid transparent;
    border-radius: 0;
    background: transparent;
    color: var(--text-1);
    font-size: var(--fs-sm);
    font-weight: 500;
    white-space: nowrap;
  }
  .tab:hover {
    color: var(--text-0);
    background: transparent;
  }
  .tab.active {
    color: var(--text-0);
    border-bottom-color: var(--accent);
  }
  .tab :global(svg) {
    color: var(--text-2);
  }
  .tab.active :global(svg) {
    color: var(--accent);
  }
  .panel {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .missing {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-8);
    text-align: center;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .missing .icon-wrap {
    width: 44px;
    height: 44px;
    border-radius: 12px;
    display: grid;
    place-items: center;
    background: var(--bg-3);
    color: var(--text-1);
  }
  .missing h3 {
    color: var(--text-0);
    font-size: var(--fs-lg);
  }
  .missing p {
    margin: 0;
    max-width: 300px;
  }
</style>
