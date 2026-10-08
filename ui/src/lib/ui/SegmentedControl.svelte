<script lang="ts">
  import type { Component } from 'svelte';

  type Opt = { value: string; label: string; icon?: Component<{ size?: number }>; disabled?: boolean };

  let {
    options = [],
    value = $bindable(''),
    label = '',
    size = 'md',
    onchange
  }: {
    options: Opt[];
    value?: string;
    label?: string;
    size?: 'sm' | 'md';
    onchange?: (value: string) => void;
  } = $props();

  let group: HTMLDivElement | null = $state(null);

  function pick(v: string) {
    if (v === value) return;
    value = v;
    onchange?.(v);
  }

  function onKey(e: KeyboardEvent) {
    const enabled = options.filter((o) => !o.disabled);
    const i = enabled.findIndex((o) => o.value === value);
    let next = -1;
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (i + 1) % enabled.length;
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (i - 1 + enabled.length) % enabled.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = enabled.length - 1;
    if (next < 0) return;
    e.preventDefault();
    const target = enabled[next];
    if (!target) return;
    pick(target.value);
    queueMicrotask(() => group?.querySelector<HTMLElement>('[aria-checked="true"]')?.focus());
  }
</script>

<div class="seg {size}" role="radiogroup" aria-label={label || undefined} bind:this={group} onkeydown={onKey} tabindex="-1">
  {#each options as o (o.value)}
    {@const Icon = o.icon}
    <button
      type="button"
      role="radio"
      aria-checked={value === o.value}
      tabindex={value === o.value ? 0 : -1}
      class:active={value === o.value}
      disabled={o.disabled}
      onclick={() => pick(o.value)}
    >
      {#if Icon}<Icon size={14} />{/if}
      {o.label}
    </button>
  {/each}
</div>

<style>
  .seg {
    display: inline-flex;
    padding: 2px;
    gap: 2px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    outline: none;
  }
  .seg button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: calc(var(--control-h) - 6px);
    padding: 0 12px;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-1);
    font-size: var(--fs-sm);
    font-weight: 500;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease),
      color var(--dur-fast) var(--ease);
  }
  .seg.md button {
    height: calc(var(--input-h) - 6px);
  }
  .seg button:hover:not(:disabled):not(.active) {
    background: var(--bg-hover);
    color: var(--text-0);
  }
  .seg button.active {
    background: var(--bg-3);
    border-color: var(--border);
    color: var(--text-0);
    box-shadow: var(--shadow-1);
  }
  .seg button:focus-visible {
    outline: none;
    box-shadow: 0 0 0 2px var(--accent);
  }
  .seg button:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
</style>
