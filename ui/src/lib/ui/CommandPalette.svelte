<script lang="ts">
  import { tick } from 'svelte';
  import { palette } from '../palette.svelte';

  let inputEl: HTMLInputElement | null = $state(null);

  $effect(() => {
    if (palette.open) {
      void tick().then(() => inputEl?.focus());
    }
  });

  const items = $derived(palette.items());

  function run(index: number) {
    const item = items[index];
    if (!item) return;
    item.run();
    palette.hide();
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      palette.hide();
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      palette.activeIndex = Math.min(palette.activeIndex + 1, items.length - 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      palette.activeIndex = Math.max(palette.activeIndex - 1, 0);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      run(palette.activeIndex);
    }
  }

</script>

{#if palette.open}
  <div class="overlay" onclick={() => palette.hide()} role="presentation">
    <div
      class="palette"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => e.stopPropagation()}
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
      tabindex="-1"
    >
      <input
        bind:this={inputEl}
        bind:value={palette.query}
        onkeydown={onKeydown}
        placeholder="Search pages, processes…"
        aria-label="Command palette search"
      />
      <div class="list" role="listbox">
        {#if items.length === 0}
          <p class="empty">No matches.</p>
        {:else}
          {#each items as item, i (item.id)}
            {@const prev = i > 0 ? items[i - 1] : null}
            {@const header = i === 0 || prev?.group !== item.group ? item.group : null}
            {#if header}
              <div class="group">{header}</div>
            {/if}
            <button
              class="item"
              class:active={i === palette.activeIndex}
              onmouseenter={() => (palette.activeIndex = i)}
              onclick={() => run(i)}
              role="option"
              aria-selected={i === palette.activeIndex}
            >
              <span class="title">{item.title}</span>
              {#if item.hint}<span class="hint">{item.hint}</span>{/if}
            </button>
          {/each}
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 12vh;
    z-index: 400;
  }
  .palette {
    width: 560px;
    max-width: 90vw;
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
  }
  input {
    width: 100%;
    background: var(--bg-2);
    border: none;
    border-bottom: 1px solid var(--border);
    padding: var(--space-4) var(--space-5);
    color: var(--text-0);
    font-size: var(--fs-lg);
  }
  input:focus {
    outline: none;
  }
  .list {
    max-height: 360px;
    overflow: auto;
    padding: var(--space-2);
  }
  .empty {
    padding: var(--space-5);
    color: var(--text-2);
    text-align: center;
    margin: 0;
  }
  .group {
    padding: var(--space-2) var(--space-3);
    color: var(--text-2);
    font-size: var(--fs-xs);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    padding: var(--space-3);
    color: var(--text-0);
    font-size: var(--fs-sm);
    text-align: left;
  }
  .item.active {
    background: var(--accent-subtle);
  }
  .hint {
    color: var(--text-2);
    font-size: var(--fs-xs);
    font-family: var(--font-mono);
  }
</style>
