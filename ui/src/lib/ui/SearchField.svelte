<script lang="ts">
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';

  let {
    value = $bindable(''),
    placeholder = 'Search…',
    label = 'Search',
    size = 'md',
    el = $bindable(null),
    oninput
  }: { value?: string; placeholder?: string; label?: string; size?: 'sm' | 'md'; el?: HTMLInputElement | null; oninput?: (e: Event) => void } = $props();

  function clear() {
    value = '';
    el?.focus();
  }
</script>

<div class="wrap {size}">
  <span class="lead"><Search size={14} /></span>
  <input
    class="search"
    type="text"
    bind:value
    bind:this={el}
    {placeholder}
    {oninput}
    aria-label={label}
    spellcheck="false"
    autocomplete="off"
    onkeydown={(e) => {
      if (e.key === 'Escape' && value) {
        e.stopPropagation();
        value = '';
      }
    }}
  />
  {#if value}
    <button class="clear" type="button" aria-label="Clear search" onclick={clear}><X size={14} /></button>
  {/if}
</div>

<style>
  .wrap {
    position: relative;
    display: flex;
    align-items: center;
    width: 100%;
    height: var(--input-h);
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    transition:
      border-color var(--dur-fast) var(--ease),
      box-shadow var(--dur-fast) var(--ease);
  }
  .wrap.sm {
    height: var(--control-h);
  }
  .wrap:hover {
    border-color: var(--border-strong);
  }
  .wrap:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-subtle);
  }
  .lead {
    display: inline-flex;
    padding-left: 10px;
    color: var(--text-2);
  }
  .search {
    flex: 1;
    min-width: 0;
    height: 100%;
    padding: 0 8px;
    background: transparent;
    border: none;
    outline: none;
    box-shadow: none;
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .clear {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    margin-right: 5px;
    padding: 0;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
  }
  .clear:hover {
    background: var(--bg-hover);
    color: var(--text-0);
  }
</style>
