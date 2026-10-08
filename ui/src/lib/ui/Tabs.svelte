<script lang="ts">
  import type { Component } from 'svelte';

  type Tab = { id: string; label: string; icon?: Component<{ size?: number }>; count?: number; disabled?: boolean };

  let {
    tabs = [],
    value = $bindable(''),
    label = '',
    onchange
  }: { tabs?: Tab[]; value?: string; label?: string; onchange?: (id: string) => void } = $props();

  let list: HTMLDivElement | null = $state(null);

  function pick(id: string) {
    if (id === value) return;
    value = id;
    onchange?.(id);
  }

  function onKey(e: KeyboardEvent) {
    const enabled = tabs.filter((t) => !t.disabled);
    const i = enabled.findIndex((t) => t.id === value);
    let next = -1;
    if (e.key === 'ArrowRight') next = (i + 1) % enabled.length;
    else if (e.key === 'ArrowLeft') next = (i - 1 + enabled.length) % enabled.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = enabled.length - 1;
    if (next < 0) return;
    e.preventDefault();
    const target = enabled[next];
    if (!target) return;
    pick(target.id);
    queueMicrotask(() => list?.querySelector<HTMLElement>('[aria-selected="true"]')?.focus());
  }
</script>

<div class="tabs" role="tablist" aria-label={label || undefined} bind:this={list} onkeydown={onKey} tabindex="-1">
  {#each tabs as t (t.id)}
    {@const Icon = t.icon}
    <button
      type="button"
      role="tab"
      id={`tab-${t.id}`}
      aria-selected={value === t.id}
      tabindex={value === t.id ? 0 : -1}
      class:active={value === t.id}
      disabled={t.disabled}
      onclick={() => pick(t.id)}
    >
      {#if Icon}<Icon size={14} />{/if}
      {t.label}
      {#if t.count !== undefined}<span class="count">{t.count}</span>{/if}
    </button>
  {/each}
</div>

<style>
  .tabs {
    display: flex;
    gap: 2px;
    border-bottom: 1px solid var(--border);
    outline: none;
    overflow-x: auto;
  }
  .tabs button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0 14px;
    height: 36px;
    margin-bottom: -1px;
    border: none;
    border-bottom: 2px solid transparent;
    border-radius: 0;
    background: transparent;
    color: var(--text-1);
    font-size: var(--fs-sm);
    font-weight: 500;
    cursor: pointer;
    white-space: nowrap;
    transition:
      color var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease);
  }
  .tabs button:hover:not(:disabled) {
    color: var(--text-0);
  }
  .tabs button.active {
    color: var(--text-0);
    border-bottom-color: var(--accent);
  }
  .tabs button:focus-visible {
    outline: none;
    background: var(--accent-subtle);
  }
  .tabs button:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .count {
    padding: 0 6px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-2);
    font-size: var(--fs-micro);
    font-variant-numeric: tabular-nums;
    line-height: 1.6;
  }
</style>
