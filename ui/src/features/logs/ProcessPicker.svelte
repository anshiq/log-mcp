<script lang="ts">
  import Layers from '@lucide/svelte/icons/layers';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import Check from '@lucide/svelte/icons/check';
  import Search from '@lucide/svelte/icons/search';
  import type { Process } from '../../lib/api/types';
  import { commandPreview, processHue } from '../../lib/logproc';
  import { scopeState } from '../../lib/state/scope.svelte';
  import LogPopover from './LogPopover.svelte';

  interface Props {
    list: Process[];
    selected: string[];
    labels: Map<string, string>;
    runningCount: number;
    onToggle: (id: string) => void;
    onClear: () => void;
    onSelectRunning: () => void;
  }

  let { list, selected, labels, runningCount, onToggle, onClear, onSelectRunning }: Props = $props();

  let open = $state(false);
  let filter = $state('');
  let search = $state<HTMLInputElement | null>(null);

  const shown = $derived.by(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return list;
    return list.filter((p) => (labels.get(p.id) ?? '').toLowerCase().includes(q) || commandPreview(p).toLowerCase().includes(q) || p.id.toLowerCase().includes(q));
  });

  const summary = $derived.by(() => {
    if (selected.length === 0) return 'All running';
    const names = selected.map((id) => labels.get(id) ?? id.slice(-6));
    if (names.length <= 2) return names.join(', ');
    return `${names.length} processes`;
  });

  function dotClass(status: string): string {
    if (status === 'running' || status === 'ready') return 'ok';
    if (status === 'starting') return 'busy';
    if (status === 'failed' || status === 'crashed') return 'err';
    return '';
  }

  function statusText(p: Process): string {
    if (p.status === 'exited' || p.status === 'stopped') return p.exitCode !== null ? `exit ${p.exitCode}` : p.status;
    return p.status;
  }

  function focusSearch() {
    filter = '';
    queueMicrotask(() => search?.focus());
  }

  function workspaceName(p: Process): string {
    return scopeState.workspaceLabel(p.workspaceId);
  }
</script>

<LogPopover bind:open width={400} label="Choose processes" onOpen={focusSearch}>
  {#snippet trigger({ toggle, attrs })}
    <button type="button" class="trigger" class:on={selected.length > 0} onclick={toggle} aria-label="Choose processes to tail" {...attrs}>
      <Layers size={15} />
      <span class="sum truncate">{summary}</span>
      {#if selected.length === 0}
        <span class="badge">{runningCount}</span>
      {:else}
        <span class="swatches">
          {#each selected.slice(0, 4) as id (id)}
            <i style="--h:{processHue(id)}"></i>
          {/each}
        </span>
      {/if}
      <ChevronDown size={14} class="chev" />
    </button>
  {/snippet}
  {#snippet children(close)}
    <div class="head">
      <div class="input-wrap">
        <Search size={14} />
        <input bind:this={search} bind:value={filter} placeholder="Filter processes" aria-label="Filter processes" />
      </div>
    </div>
    <div class="list" role="listbox" aria-multiselectable="true" aria-label="Processes">
      <button type="button" class="opt all" role="option" aria-selected={selected.length === 0} onclick={() => { onClear(); }}>
        <span class="box" class:on={selected.length === 0}>{#if selected.length === 0}<Check size={12} strokeWidth={3} />{/if}</span>
        <span class="main">
          <span class="name">All running</span>
          <span class="sub">Tail every running process in scope, including new ones as they start</span>
        </span>
        <span class="badge">{runningCount}</span>
      </button>
      {#if shown.length === 0}
        <p class="none">No processes match</p>
      {/if}
      {#each shown as p (p.id)}
        {@const on = selected.includes(p.id)}
        <button type="button" class="opt" role="option" aria-selected={on} onclick={() => onToggle(p.id)}>
          <span class="box" class:on>{#if on}<Check size={12} strokeWidth={3} />{/if}</span>
          <span class="main">
            <span class="line">
              <i class="sw" style="--h:{processHue(p.id)}"></i>
              <span class="name truncate">{labels.get(p.id) ?? p.id}</span>
              {#if scopeState.all}<span class="ws truncate">{workspaceName(p)}</span>{/if}
            </span>
            <span class="sub mono truncate">{commandPreview(p)}</span>
          </span>
          <span class="st"><span class="dot {dotClass(p.status)}"></span>{statusText(p)}</span>
        </button>
      {/each}
    </div>
    <div class="foot">
      <span class="muted">{selected.length === 0 ? 'Following all running' : `${selected.length} selected`}</span>
      <span class="grow"></span>
      {#if runningCount > 0}
        <button type="button" class="btn sm ghost" onclick={onSelectRunning}>Pin running</button>
      {/if}
      <button type="button" class="btn sm ghost" disabled={selected.length === 0} onclick={onClear}>Clear</button>
      <button type="button" class="btn sm" onclick={close}>Done</button>
    </div>
  {/snippet}
</LogPopover>

<style>
  .trigger {
    max-width: 260px;
    padding: 5px 10px;
    gap: 8px;
    height: 32px;
  }

  .trigger.on {
    border-color: color-mix(in srgb, var(--accent) 50%, var(--border));
    background: var(--accent-subtle);
  }

  .trigger :global(.chev) {
    color: var(--text-2);
    margin-left: -2px;
  }

  .sum {
    min-width: 0;
  }

  .swatches {
    display: inline-flex;
  }

  .swatches i,
  .sw {
    display: block;
    width: 9px;
    height: 9px;
    border-radius: 3px;
    background: hsl(var(--h) 70% 58%);
    flex: none;
  }

  .swatches i {
    margin-left: -3px;
    box-shadow: 0 0 0 2px var(--bg-2);
  }

  .swatches i:first-child {
    margin-left: 0;
  }

  .head {
    padding: var(--space-3);
    border-bottom: 1px solid var(--border);
  }

  .list {
    max-height: 340px;
    overflow: auto;
    padding: var(--space-2);
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .opt {
    width: 100%;
    justify-content: flex-start;
    align-items: center;
    gap: 10px;
    padding: 7px 8px;
    border: none;
    background: transparent;
    text-align: left;
    border-radius: var(--radius);
    font-weight: 400;
  }

  .opt:hover:not(:disabled) {
    background: var(--bg-hover);
  }

  .opt[aria-selected='true'] {
    background: var(--accent-subtle);
  }

  .box {
    width: 16px;
    height: 16px;
    flex: none;
    border-radius: 4px;
    border: 1px solid var(--border-strong);
    display: grid;
    place-items: center;
    color: var(--accent-fg);
  }

  .box.on {
    background: var(--accent);
    border-color: var(--accent);
  }

  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .line {
    display: flex;
    align-items: center;
    gap: 7px;
    min-width: 0;
  }

  .name {
    font-weight: 600;
    color: var(--text-0);
  }

  .ws {
    color: var(--text-2);
    font-size: var(--fs-xs);
    padding: 0 6px;
    border-radius: 999px;
    background: var(--bg-3);
    flex: 0 1 auto;
  }

  .sub {
    color: var(--text-2);
    font-size: var(--fs-xs);
    white-space: normal;
  }

  .sub.truncate {
    white-space: nowrap;
  }

  .st {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-1);
    font-size: var(--fs-xs);
    flex: none;
  }

  .none {
    padding: var(--space-5);
    text-align: center;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }

  .foot {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3);
    border-top: 1px solid var(--border);
    font-size: var(--fs-xs);
  }

  .grow {
    flex: 1;
  }
</style>
