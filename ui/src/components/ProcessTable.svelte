<script lang="ts">
  import ChevronUp from '@lucide/svelte/icons/chevron-up';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import ChevronsUpDown from '@lucide/svelte/icons/chevrons-up-down';
  import SquareTerminal from '@lucide/svelte/icons/square-terminal';
  import SearchX from '@lucide/svelte/icons/search-x';
  import Plus from '@lucide/svelte/icons/plus';
  import FolderOpen from '@lucide/svelte/icons/folder-open';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import type { Process } from '../lib/api/types';
  import ProcessStatus from './ProcessStatus.svelte';
  import RowActions from './RowActions.svelte';
  import { procActions } from './processActions.svelte';
  import {
    commandLine,
    displayName,
    healthTone,
    isExited,
    isFailed,
    isLive,
    sortRows,
    uptimeText,
    type SortDir,
    type SortKey
  } from './processView';

  let {
    rows,
    totalCount,
    loading = false,
    selected = $bindable(new Set<string>()),
    openId = '',
    showWorkspace = false,
    workspaceLabel,
    onOpen,
    onLogs,
    onCopyId,
    onRemoved,
    onStart,
    onClearFilters
  }: {
    rows: Process[];
    totalCount: number;
    loading?: boolean;
    selected?: Set<string>;
    openId?: string;
    showWorkspace?: boolean;
    workspaceLabel: (id: string) => string;
    onOpen: (id: string) => void;
    onLogs: (id: string) => void;
    onCopyId: (id: string) => void;
    onRemoved: (ids: string[]) => void;
    onStart: () => void;
    onClearFilters: () => void;
  } = $props();

  let sortKey = $state<SortKey | null>(null);
  let sortDir = $state<SortDir>('asc');
  let now = $state(Date.now());
  let body: HTMLTableSectionElement | null = $state(null);
  let lastChecked = $state('');

  $effect(() => {
    const t = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(t);
  });

  const sorted = $derived(sortRows(rows, sortKey, sortDir));
  const allChecked = $derived(sorted.length > 0 && sorted.every((p) => selected.has(p.id)));
  const someChecked = $derived(!allChecked && sorted.some((p) => selected.has(p.id)));

  function sortBy(key: SortKey) {
    if (sortKey !== key) {
      sortKey = key;
      sortDir = 'asc';
    } else if (sortDir === 'asc') {
      sortDir = 'desc';
    } else {
      sortKey = null;
      sortDir = 'asc';
    }
  }

  function ariaSort(key: SortKey): 'ascending' | 'descending' | 'none' {
    if (sortKey !== key) return 'none';
    return sortDir === 'asc' ? 'ascending' : 'descending';
  }

  function toggle(id: string, range: boolean) {
    const next = new Set(selected);
    const on = !next.has(id);
    if (range && lastChecked) {
      const a = sorted.findIndex((p) => p.id === lastChecked);
      const b = sorted.findIndex((p) => p.id === id);
      if (a >= 0 && b >= 0) {
        for (const p of sorted.slice(Math.min(a, b), Math.max(a, b) + 1)) {
          if (on) next.add(p.id);
          else next.delete(p.id);
        }
      }
    } else if (on) next.add(id);
    else next.delete(id);
    lastChecked = id;
    selected = next;
  }

  function toggleAll() {
    selected = allChecked ? new Set() : new Set(sorted.map((p) => p.id));
  }

  function indeterminate(node: HTMLInputElement, value: boolean) {
    node.indeterminate = value;
    return {
      update(v: boolean) {
        node.indeterminate = v;
      }
    };
  }

  function rowClick(e: MouseEvent) {
    const target = e.target as HTMLElement;
    if (target.closest('button, a, input, label')) return;
    const id = target.closest('tr')?.dataset['id'];
    if (id) onOpen(id);
  }

  function rowKey(e: KeyboardEvent) {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp' && e.key !== 'Home' && e.key !== 'End') return;
    const target = e.target as HTMLElement;
    if (!target.classList.contains('name-btn')) return;
    const all = [...(body?.querySelectorAll<HTMLElement>('.name-btn') ?? [])];
    const i = all.indexOf(target);
    let next = i;
    if (e.key === 'ArrowDown') next = Math.min(all.length - 1, i + 1);
    else if (e.key === 'ArrowUp') next = Math.max(0, i - 1);
    else if (e.key === 'Home') next = 0;
    else next = all.length - 1;
    e.preventDefault();
    all[next]?.focus();
  }

  $effect(() => {
    const el = body;
    if (!el) return;
    el.addEventListener('click', rowClick);
    el.addEventListener('keydown', rowKey);
    return () => {
      el.removeEventListener('click', rowClick);
      el.removeEventListener('keydown', rowKey);
    };
  });

  const columns: { key: SortKey; label: string; cls: string; num?: boolean }[] = [
    { key: 'status', label: 'Status', cls: 'c-status' },
    { key: 'name', label: 'Process', cls: 'c-name' },
    { key: 'pid', label: 'PID', cls: 'c-pid', num: true },
    { key: 'uptime', label: 'Uptime', cls: 'c-uptime' },
    { key: 'restarts', label: 'Restarts', cls: 'c-restarts', num: true },
    { key: 'health', label: 'Health', cls: 'c-health' }
  ];
</script>

<div class="table-wrap">
  {#if loading}
    <div class="skeleton" aria-busy="true" aria-label="Loading processes">
      {#each [0, 1, 2, 3, 4, 5] as i (i)}
        <div class="sk-row" style="--d:{i * 90}ms">
          <span class="sk sk-dot"></span>
          <span class="sk sk-pill"></span>
          <span class="sk-stack">
            <span class="sk sk-line" style="width:{34 + ((i * 13) % 30)}%"></span>
            <span class="sk sk-line thin" style="width:{52 + ((i * 17) % 34)}%"></span>
          </span>
          <span class="sk sk-line short"></span>
        </div>
      {/each}
    </div>
  {:else if totalCount === 0}
    <div class="empty-state">
      <div class="icon-wrap"><SquareTerminal size={20} /></div>
      <h3>No processes yet</h3>
      <p>
        Run a dev server, worker or any long-running command and it shows up here with live logs and health. You can
        also start apps from the Apps page or from a terminal with <span class="mono inline-code">agent-runtime start -- &lt;command&gt;</span>.
      </p>
      <div class="cta">
        <button class="btn primary" onclick={onStart}><Plus size={14} /> Start your first process</button>
        <a class="btn" href="#/apps"><FolderOpen size={14} /> Browse apps</a>
      </div>
    </div>
  {:else if rows.length === 0}
    <div class="empty-state">
      <div class="icon-wrap"><SearchX size={20} /></div>
      <h3>No matching processes</h3>
      <p>Nothing matches the current search and status filter.</p>
      <button class="btn" onclick={onClearFilters}>Clear filters</button>
    </div>
  {:else}
    <table class="data procs-table">
      <thead>
        <tr>
          <th class="c-check">
            <input
              type="checkbox"
              checked={allChecked}
              use:indeterminate={someChecked}
              onchange={toggleAll}
              aria-label="Select all processes"
            />
          </th>
          {#each columns as col (col.key)}
            <th class="{col.cls} sortable" class:num={col.num} class:sorted={sortKey === col.key} aria-sort={ariaSort(col.key)}>
              <button type="button" class="sort" onclick={() => sortBy(col.key)}>
                <span>{col.label}</span>
                <span class="chev">
                  {#if sortKey === col.key}
                    {#if sortDir === 'asc'}<ChevronUp size={12} />{:else}<ChevronDown size={12} />{/if}
                  {:else}
                    <ChevronsUpDown size={12} />
                  {/if}
                </span>
              </button>
            </th>
          {/each}
          <th class="c-ports">Ports</th>
          <th class="c-actions"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody bind:this={body}>
        {#each sorted as p (p.id)}
          {@const pending = procActions.busy.get(p.id) ?? ''}
          {@const tone = healthTone(p.health)}
          {@const line = commandLine(p)}
          {@const time = uptimeText(p, now)}
          <tr
            data-id={p.id}
            class:open={openId === p.id}
            class:checked={selected.has(p.id)}
            class:dim={!pending && isExited(p)}
            class:stale={p.stale}
          >
            <td class="c-check">
              <input
                type="checkbox"
                checked={selected.has(p.id)}
                onclick={(e) => toggle(p.id, e.shiftKey)}
                aria-label="Select {displayName(p)}"
              />
            </td>
            <td class="c-status"><ProcessStatus process={p} {pending} /></td>
            <td class="c-name">
              <button
                type="button"
                class="name-btn"
                aria-current={openId === p.id ? 'true' : undefined}
                aria-label="Open {displayName(p)}"
                title={line}
                onclick={() => onOpen(p.id)}
              >
                <span class="name-line">
                  <span class="name">{displayName(p)}</span>
                  {#if p.stale}<span class="badge warn" title="Config changed since this process started">stale</span>{/if}
                  {#if showWorkspace}<span class="badge ws" title={p.workdir || p.workspaceId}>{workspaceLabel(p.workspaceId)}</span>{/if}
                </span>
                <span class="mono link cmd">{line}</span>
              </button>
            </td>
            <td class="c-pid num mono muted">{p.pid && isLive(p) ? p.pid : '—'}</td>
            <td class="c-uptime mono" class:muted={!isLive(p)}>
              {#if isLive(p) && p.startedAt}
                <span class="uptime">{time}</span>
              {:else if isFailed(p) || isExited(p)}
                <span class="ended">{time || '—'}</span>
              {:else}
                <span class="muted">{time || '—'}</span>
              {/if}
            </td>
            <td class="c-restarts num">
              {#if p.restarts > 0}<span class="badge warn"><RotateCw size={12} />{p.restarts}</span>{:else}<span class="muted">0</span>{/if}
            </td>
            <td class="c-health">
              {#if tone}
                <span class="badge {tone === 'busy' ? 'info' : tone === 'off' ? '' : tone}">{p.health}</span>
              {:else}
                <span class="muted">—</span>
              {/if}
            </td>
            <td class="c-ports">
              {#if p.ports.length > 0}
                <span class="ports">
                  {#each p.ports.slice(0, 2) as port (port)}<span class="badge mono port">:{port}</span>{/each}
                  {#if p.ports.length > 2}<span class="badge mono" title={p.ports.join(', ')}>+{p.ports.length - 2}</span>{/if}
                </span>
              {:else}
                <span class="muted">—</span>
              {/if}
            </td>
            <td class="c-actions">
              <RowActions process={p} {onLogs} {onCopyId} {onRemoved} />
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .table-wrap {
    flex: 1;
    min-height: 0;
    min-width: 0;
    position: relative;
  }
  .procs-table {
    table-layout: fixed;
    min-width: 0;
  }
  .procs-table :global(th),
  .procs-table :global(td) {
    padding: 0 var(--cell-px);
    overflow: hidden;
  }
  .procs-table :global(th) {
    height: var(--row-h);
    padding-top: 0;
    padding-bottom: 0;
  }
  .procs-table tbody tr {
    height: var(--row-h);
    cursor: pointer;
  }
  .procs-table tbody :global(td) {
    padding-top: var(--cell-py);
    padding-bottom: var(--cell-py);
    transition: background var(--dur-fast) var(--ease);
  }
  .procs-table input[type='checkbox'] {
    appearance: none;
    display: inline-grid;
    place-content: center;
    width: 13px;
    height: 13px;
    margin: 0;
    border: 1.5px solid var(--border-strong);
    border-radius: 4px;
    background: var(--bg-2);
    cursor: pointer;
    vertical-align: middle;
    transition: background var(--dur-fast) var(--ease), border-color var(--dur-fast) var(--ease);
  }
  .procs-table input[type='checkbox']:hover {
    border-color: var(--text-2);
  }
  .procs-table input[type='checkbox']:checked,
  .procs-table input[type='checkbox']:indeterminate {
    background: var(--accent);
    border-color: var(--accent);
  }
  .procs-table input[type='checkbox']:checked::after {
    content: '';
    width: 8px;
    height: 4px;
    border-left: 2px solid var(--accent-fg);
    border-bottom: 2px solid var(--accent-fg);
    transform: translateY(-1px) rotate(-45deg);
  }
  .procs-table input[type='checkbox']:indeterminate::after {
    content: '';
    width: 8px;
    height: 0;
    border-bottom: 2px solid var(--accent-fg);
  }
  .c-check {
    width: 44px;
    padding-right: 0 !important;
  }
  .c-status {
    width: 128px;
  }
  .c-pid {
    width: 82px;
  }
  .c-uptime {
    width: 128px;
  }
  .c-restarts {
    width: 92px;
  }
  .c-health {
    width: 104px;
  }
  .c-ports {
    width: 148px;
  }
  .c-actions {
    width: 132px;
    text-align: right;
  }
  td.c-actions :global(.row-actions) {
    opacity: 0;
  }
  tr:hover td.c-actions :global(.row-actions),
  tr:focus-within td.c-actions :global(.row-actions) {
    opacity: 1;
  }
  th.c-actions {
    padding-right: var(--space-4);
  }
  td.c-actions {
    padding-left: 0 !important;
    padding-right: var(--space-3) !important;
  }
  .sort {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 4px;
    margin: 0 -4px;
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    color: inherit;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
    cursor: pointer;
  }
  .sort:hover {
    color: var(--text-0);
    background: var(--bg-hover);
  }
  th.num .sort {
    flex-direction: row-reverse;
  }
  .chev {
    display: inline-grid;
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease);
  }
  .sort:hover .chev,
  .sort:focus-visible .chev,
  th.sorted .chev {
    opacity: 1;
  }
  th.sorted {
    color: var(--text-0);
  }
  th.sorted .chev {
    color: var(--accent);
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
  }
  tr.checked :global(td) {
    background: color-mix(in srgb, var(--accent) 7%, transparent);
  }
  tr.open :global(td) {
    background: var(--accent-subtle) !important;
  }
  tr.open :global(td.c-check) {
    box-shadow: inset 2px 0 0 var(--accent);
  }
  tr.dim .name,
  tr.dim .cmd {
    opacity: 0.8;
  }
  tr.stale :global(td.c-check) {
    box-shadow: inset 2px 0 0 var(--warn);
  }
  .name-btn {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    width: 100%;
    min-width: 0;
    padding: 3px 6px;
    margin: -3px -6px;
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-0);
    text-align: left;
    cursor: pointer;
  }
  .name-btn:hover {
    background: transparent;
    border-color: transparent;
  }
  .name-line {
    display: flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    min-width: 0;
  }
  .name {
    font-weight: 500;
    font-size: var(--fs-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .name-line .badge {
    flex: none;
    font-size: var(--fs-micro);
    padding: 0 7px;
    max-width: 130px;
    overflow: hidden;
    text-overflow: ellipsis;
    display: inline-block;
  }
  .badge.ws {
    color: var(--text-1);
  }
  .cmd {
    display: block;
    width: 100%;
    font-size: var(--fs-xs);
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 400;
  }
  tr:hover .cmd,
  .name-btn:focus-visible .cmd {
    color: var(--text-1);
  }
  .uptime {
    color: var(--text-0);
    font-variant-numeric: tabular-nums;
  }
  .ended {
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .c-uptime {
    font-size: var(--fs-xs);
    font-family: var(--font-mono);
  }
  .c-pid,
  .c-restarts {
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
  }
  .num {
    font-family: var(--font-mono);
    font-variant-numeric: tabular-nums;
  }
  .badge :global(svg) {
    flex: none;
  }
  .ports {
    display: inline-flex;
    gap: 4px;
    flex-wrap: nowrap;
  }
  .port {
    font-size: var(--fs-micro);
  }
  .badge.port {
    color: var(--info);
    background: color-mix(in srgb, var(--info) 10%, transparent);
    border-color: color-mix(in srgb, var(--info) 26%, transparent);
  }

  .empty-state {
    min-height: 100%;
    padding-block: 72px;
  }
  .empty-state p {
    max-width: 460px;
    line-height: 1.55;
  }
  .inline-code {
    font-size: var(--fs-xs);
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    color: var(--text-1);
    white-space: nowrap;
  }
  .cta {
    display: flex;
    gap: var(--space-3);
    margin-top: var(--space-3);
    flex-wrap: wrap;
    justify-content: center;
  }
  a.btn {
    text-decoration: none;
  }

  .skeleton {
    display: flex;
    flex-direction: column;
  }
  .sk-row {
    display: grid;
    grid-template-columns: 14px 96px 1fr 80px;
    align-items: center;
    gap: var(--space-5);
    height: var(--row-h);
    padding: 0 var(--space-5);
    border-bottom: 1px solid var(--border);
  }
  .sk-stack {
    display: flex;
    flex-direction: column;
    gap: 7px;
  }
  .sk {
    display: block;
    border-radius: 999px;
    background: linear-gradient(90deg, var(--bg-3) 25%, var(--bg-hover) 50%, var(--bg-3) 75%);
    background-size: 200% 100%;
    animation: shimmer 1.4s linear infinite;
    animation-delay: var(--d, 0ms);
  }
  .sk-dot {
    width: 14px;
    height: 14px;
    border-radius: 4px;
  }
  .sk-pill {
    height: 20px;
  }
  .sk-line {
    height: 12px;
  }
  .sk-line.thin {
    height: 9px;
  }
  .sk-line.short {
    width: 100%;
  }
  @keyframes shimmer {
    from {
      background-position: 200% 0;
    }
    to {
      background-position: -200% 0;
    }
  }

  @container procs (max-width: 1000px) {
    .c-health {
      display: none;
    }
  }
  @container procs (max-width: 880px) {
    .c-ports {
      display: none;
    }
  }
  @container procs (max-width: 780px) {
    .c-restarts {
      display: none;
    }
  }
  @container procs (max-width: 680px) {
    .c-pid {
      display: none;
    }
  }
  @container procs (max-width: 620px) {
    .c-actions {
      width: 52px;
    }
    .c-status {
      width: 116px;
    }
    .c-uptime {
      width: 108px;
    }
  }
  @container procs (max-width: 520px) {
    .c-uptime {
      display: none;
    }
    .procs-table :global(th),
    .procs-table :global(td) {
      padding-inline: var(--space-3);
    }
  }
</style>
