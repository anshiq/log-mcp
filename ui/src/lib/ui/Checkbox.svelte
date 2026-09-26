<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    checked = $bindable(false),
    indeterminate = false,
    label,
    onchange,
    children
  }: {
    checked?: boolean;
    indeterminate?: boolean;
    label?: string;
    onchange?: (e: Event) => void;
    children?: Snippet;
  } = $props();

  let node: HTMLInputElement | null = $state(null);

  $effect(() => {
    if (node) node.indeterminate = indeterminate;
  });
</script>

<label class="wrap">
  <input type="checkbox" bind:this={node} bind:checked {onchange} aria-label={label} />
  {#if children}<span class="text">{@render children()}</span>{/if}
</label>

<style>
  .wrap {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  input {
    accent-color: var(--accent);
  }
</style>
