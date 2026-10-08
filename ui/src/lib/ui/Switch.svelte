<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    checked = $bindable(false),
    label,
    id,
    disabled = false,
    onchange,
    children
  }: { checked?: boolean; label?: string; id?: string; disabled?: boolean; onchange?: (e: Event) => void; children?: Snippet } = $props();
</script>

<label class="switch" class:disabled>
  <input type="checkbox" role="switch" {id} bind:checked {disabled} {onchange} aria-label={label} aria-checked={checked} />
  <span class="track"><span class="thumb"></span></span>
  {#if children}<span class="text">{@render children()}</span>{/if}
</label>

<style>
  .switch {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  .switch.disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .switch input {
    position: absolute;
    opacity: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    cursor: inherit;
  }
  .track {
    position: relative;
    flex: none;
    width: 26px;
    height: 14px;
    border-radius: 999px;
    background: var(--bg-3);
    border: 1px solid var(--border-strong);
    transition:
      background var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease);
  }
  .thumb {
    position: absolute;
    top: 1px;
    left: 1px;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--text-1);
    box-shadow: var(--shadow-1);
    transition:
      transform var(--dur-fast) var(--ease),
      background var(--dur-fast) var(--ease);
  }
  input:checked + .track {
    background: var(--accent);
    border-color: var(--accent);
  }
  input:checked + .track .thumb {
    transform: translateX(12px);
    background: var(--accent-fg);
  }
  .switch:hover input:not(:disabled):not(:checked) + .track {
    border-color: var(--text-2);
  }
  input:focus-visible + .track {
    box-shadow: 0 0 0 3px var(--accent-subtle);
    border-color: var(--accent);
  }
</style>
