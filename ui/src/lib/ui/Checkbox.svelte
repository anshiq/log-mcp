<script lang="ts">
  import type { Snippet } from 'svelte';
  import Check from '@lucide/svelte/icons/check';
  import Minus from '@lucide/svelte/icons/minus';

  let {
    checked = $bindable(false),
    indeterminate = false,
    label,
    id,
    disabled = false,
    onchange,
    children
  }: {
    checked?: boolean;
    indeterminate?: boolean;
    label?: string;
    id?: string;
    disabled?: boolean;
    onchange?: (e: Event) => void;
    children?: Snippet;
  } = $props();

  let node: HTMLInputElement | null = $state(null);

  $effect(() => {
    if (node) node.indeterminate = indeterminate;
  });
</script>

<label class="wrap" class:disabled>
  <input type="checkbox" {id} bind:this={node} bind:checked {disabled} {onchange} aria-label={label} />
  <span class="box" aria-hidden="true">
    {#if indeterminate}<Minus size={12} strokeWidth={3} />{:else if checked}<Check size={12} strokeWidth={3.2} />{/if}
  </span>
  {#if children}<span class="text">{@render children()}</span>{/if}
</label>

<style>
  .wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: var(--fs-sm);
    color: var(--text-1);
    cursor: pointer;
  }
  .wrap.disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  input {
    position: absolute;
    opacity: 0;
    width: 13px;
    height: 13px;
    margin: 0;
    cursor: inherit;
  }
  .box {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 13px;
    height: 13px;
    border-radius: 3px;
    border: 1px solid var(--border-strong);
    background: var(--bg-2);
    color: var(--accent-fg);
    transition:
      background var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease),
      box-shadow var(--dur-fast) var(--ease);
  }
  .wrap:hover input:not(:disabled) + .box {
    border-color: var(--text-2);
  }
  input:checked + .box,
  input:indeterminate + .box {
    background: var(--accent);
    border-color: var(--accent);
  }
  input:focus-visible + .box {
    box-shadow: 0 0 0 3px var(--accent-subtle);
    border-color: var(--accent);
  }
</style>
