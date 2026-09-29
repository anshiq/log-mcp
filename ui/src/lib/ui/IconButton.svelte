<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    label = '',
    onclick = () => {},
    size = 'md',
    variant = 'ghost',
    disabled = false,
    pressed,
    class: klass = '',
    children
  }: {
    label?: string;
    onclick?: (e: MouseEvent) => void;
    size?: 'sm' | 'md' | 'lg';
    variant?: 'ghost' | 'secondary' | 'danger';
    disabled?: boolean;
    pressed?: boolean;
    class?: string;
    children?: Snippet;
  } = $props();
</script>

<button
  class="ib {size} {variant} {klass}"
  class:on={pressed}
  type="button"
  aria-label={label}
  aria-pressed={pressed}
  title={label}
  {disabled}
  {onclick}
>
  {#if children}{@render children()}{/if}
</button>

<style>
  .ib {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: var(--control-h);
    height: var(--control-h);
    padding: 0;
    border-radius: var(--radius);
    border: 1px solid transparent;
    background: transparent;
    color: var(--text-1);
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease),
      color var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease),
      box-shadow var(--dur-fast) var(--ease);
  }
  .ib.sm {
    width: calc(var(--control-h) - 4px);
    height: calc(var(--control-h) - 4px);
    border-radius: var(--radius-sm);
  }
  .ib.lg {
    width: var(--input-h);
    height: var(--input-h);
  }
  .ib:hover:not(:disabled) {
    background: var(--bg-hover);
    color: var(--text-0);
  }
  .ib.secondary {
    background: var(--bg-2);
    border-color: var(--border);
  }
  .ib.secondary:hover:not(:disabled) {
    background: var(--bg-3);
    border-color: var(--border-strong);
  }
  .ib.danger {
    color: var(--err);
  }
  .ib.danger:hover:not(:disabled) {
    background: color-mix(in srgb, var(--err) 12%, transparent);
    color: var(--err);
  }
  .ib.on {
    background: var(--accent-subtle);
    color: var(--accent);
  }
  .ib:focus-visible {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-subtle);
  }
  .ib:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
</style>
