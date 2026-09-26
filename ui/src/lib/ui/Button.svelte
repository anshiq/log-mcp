<script lang="ts">
  import type { Snippet } from 'svelte';
  import Spinner from './Spinner.svelte';

  let {
    variant = 'secondary',
    size = 'md',
    loading = false,
    disabled = false,
    type = 'button',
    icon = false,
    label,
    onclick,
    children
  }: {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'link';
    size?: 'sm' | 'md';
    loading?: boolean;
    disabled?: boolean;
    type?: 'button' | 'submit';
    icon?: boolean;
    label?: string;
    onclick?: (e: MouseEvent) => void;
    children?: Snippet;
  } = $props();
</script>

<button
  class="btn {variant} {size}"
  class:icon-only={icon}
  {type}
  disabled={disabled || loading}
  aria-label={label}
  title={label}
  onclick={(e) => !loading && onclick?.(e)}
>
  {#if loading}
    <Spinner size={size === 'sm' ? 12 : 14} />
  {/if}
  {@render children?.()}
</button>

<style>
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    border-radius: var(--radius-sm);
    font-size: var(--fs-sm);
    font-family: var(--font-ui);
    white-space: nowrap;
    transition: background var(--dur-fast) var(--ease), border-color var(--dur-fast) var(--ease);
    border: 1px solid transparent;
  }
  .btn:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .btn.md {
    padding: var(--space-2) var(--space-4);
  }
  .btn.sm {
    padding: var(--space-1) var(--space-3);
    font-size: var(--fs-xs);
  }
  .btn.icon-only.md {
    padding: var(--space-2);
  }
  .btn.icon-only.sm {
    padding: var(--space-1);
  }
  .primary {
    background: var(--accent);
    color: #fff;
  }
  .primary:not(:disabled):hover {
    background: var(--accent-hover);
  }
  .secondary {
    background: var(--bg-3);
    border-color: var(--border);
    color: var(--text-0);
  }
  .secondary:not(:disabled):hover {
    border-color: var(--border-strong);
  }
  .ghost {
    background: transparent;
    color: var(--text-1);
  }
  .ghost:not(:disabled):hover {
    background: var(--bg-3);
    color: var(--text-0);
  }
  .danger {
    background: transparent;
    border-color: var(--err);
    color: var(--err);
  }
  .danger:not(:disabled):hover {
    background: color-mix(in srgb, var(--err) 12%, transparent);
  }
  .link {
    background: transparent;
    color: var(--accent);
    padding: 0;
  }
  .link:not(:disabled):hover {
    color: var(--accent-hover);
    text-decoration: underline;
  }
</style>
