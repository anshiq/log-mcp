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
    title,
    autofocus = false,
    full = false,
    class: klass = '',
    onclick,
    children
  }: {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'destructive' | 'link';
    size?: 'sm' | 'md' | 'lg';
    loading?: boolean;
    disabled?: boolean;
    type?: 'button' | 'submit' | 'reset';
    icon?: boolean;
    label?: string;
    title?: string;
    autofocus?: boolean;
    full?: boolean;
    class?: string;
    onclick?: (e: MouseEvent) => void;
    children?: Snippet;
  } = $props();
</script>

<button
  class="btn {variant} {size} {klass}"
  class:icon-only={icon}
  class:full
  class:loading
  {type}
  disabled={disabled || loading}
  aria-label={label}
  aria-busy={loading || undefined}
  title={title ?? label}
  data-autofocus={autofocus ? '' : undefined}
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
    gap: 6px;
    height: var(--control-h);
    padding: 0 12px;
    border-radius: var(--radius);
    border: 1px solid transparent;
    font-family: var(--font-ui);
    font-size: var(--fs-sm);
    font-weight: 500;
    line-height: 1;
    white-space: nowrap;
    cursor: pointer;
    user-select: none;
    transition:
      background var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease),
      color var(--dur-fast) var(--ease),
      box-shadow var(--dur-fast) var(--ease);
  }
  .btn:focus-visible {
    outline: none;
    box-shadow: 0 0 0 3px var(--accent-subtle);
    border-color: var(--accent);
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .btn.loading {
    cursor: progress;
    opacity: 0.85;
  }
  .btn.sm {
    height: calc(var(--control-h) - 4px);
    padding: 0 9px;
    font-size: var(--fs-xs);
    gap: 5px;
  }
  .btn.lg {
    height: var(--input-h);
    padding: 0 16px;
    font-size: var(--fs-md);
  }
  .btn.full {
    width: 100%;
  }
  .btn.icon-only {
    width: var(--control-h);
    padding: 0;
  }
  .btn.icon-only.sm {
    width: calc(var(--control-h) - 4px);
  }
  .btn.icon-only.lg {
    width: var(--input-h);
  }
  .primary {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-fg);
  }
  .primary:not(:disabled):hover {
    background: var(--accent-hover);
    border-color: var(--accent-hover);
  }
  .secondary {
    background: var(--bg-2);
    border-color: var(--border);
    color: var(--text-0);
  }
  .secondary:not(:disabled):hover {
    background: var(--bg-3);
    border-color: var(--border-strong);
  }
  .ghost {
    background: transparent;
    color: var(--text-1);
  }
  .ghost:not(:disabled):hover {
    background: var(--bg-hover);
    color: var(--text-0);
  }
  .danger {
    background: transparent;
    border-color: color-mix(in srgb, var(--err) 45%, var(--border));
    color: var(--err);
  }
  .danger:not(:disabled):hover {
    background: color-mix(in srgb, var(--err) 12%, transparent);
    border-color: var(--err);
  }
  .destructive {
    background: var(--err);
    border-color: var(--err);
    color: #fff;
  }
  .destructive:not(:disabled):hover {
    background: color-mix(in srgb, var(--err) 85%, #000);
    border-color: color-mix(in srgb, var(--err) 85%, #000);
  }
  .link {
    background: transparent;
    color: var(--accent);
    padding: 0;
    height: auto;
  }
  .link:not(:disabled):hover {
    color: var(--accent-hover);
    text-decoration: underline;
  }
</style>
