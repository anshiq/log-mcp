<script lang="ts">
  import { tick } from 'svelte';
  import Search from '@lucide/svelte/icons/search';
  import SearchX from '@lucide/svelte/icons/search-x';
  import CornerDownLeft from '@lucide/svelte/icons/corner-down-left';
  import ArrowUp from '@lucide/svelte/icons/arrow-up';
  import ArrowDown from '@lucide/svelte/icons/arrow-down';
  import { palette, highlight, GROUP_LABEL } from '../palette.svelte';
  import { layer } from './overlay';
  import { pop, fadeIn } from './motion';
  import Kbd from './Kbd.svelte';

  let inputEl: HTMLInputElement | null = $state(null);
  let listEl: HTMLDivElement | null = $state(null);

  $effect(() => {
    if (palette.open) void tick().then(() => inputEl?.focus());
  });

  const items = $derived(palette.items());
  const query = $derived(palette.query);

  $effect(() => {
    void query;
    palette.activeIndex = 0;
  });

  $effect(() => {
    const i = palette.activeIndex;
    if (!palette.open) return;
    void tick().then(() => listEl?.querySelector<HTMLElement>(`[data-index="${i}"]`)?.scrollIntoView({ block: 'nearest' }));
  });

  function run(index: number) {
    const item = items[index];
    if (!item) return;
    palette.hide();
    item.run();
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      palette.activeIndex = items.length ? (palette.activeIndex + 1) % items.length : 0;
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      palette.activeIndex = items.length ? (palette.activeIndex - 1 + items.length) % items.length : 0;
    } else if (e.key === 'Home') {
      e.preventDefault();
      palette.activeIndex = 0;
    } else if (e.key === 'End') {
      e.preventDefault();
      palette.activeIndex = Math.max(0, items.length - 1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      run(palette.activeIndex);
    }
  }
</script>

{#if palette.open}
  <div class="overlay" in:fadeIn out:fadeIn onmousedown={(e) => e.target === e.currentTarget && palette.hide()} role="presentation">
    <div
      class="palette"
      in:pop={{ duration: 140, y: -6, from: 0.98 }}
      out:pop={{ duration: 90, y: -6, from: 0.98 }}
      use:layer={{ onClose: () => palette.hide(), focus: 'none', trap: true }}
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
      tabindex="-1"
    >
      <div class="search">
        <Search size={16} />
        <input
          bind:this={inputEl}
          bind:value={palette.query}
          onkeydown={onKeydown}
          placeholder="Search pages, processes…"
          aria-label="Command palette search"
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-list"
          aria-activedescendant={items[palette.activeIndex] ? `palette-opt-${palette.activeIndex}` : undefined}
          autocomplete="off"
          spellcheck="false"
        />
        <span class="esc"><Kbd keys="Esc" /></span>
      </div>
      <div class="list" id="palette-list" role="listbox" aria-label="Results" bind:this={listEl}>
        {#if items.length === 0}
          <div class="empty">
            <span class="ico"><SearchX size={20} /></span>
            <p class="t">No results</p>
            <p class="s">Nothing matches “{palette.query.trim()}”. Try a page, workspace or process name.</p>
          </div>
        {:else}
          {#each items as item, i (item.id)}
            {@const prev = i > 0 ? items[i - 1] : null}
            {#if !prev || prev.group !== item.group}
              <div class="group" role="presentation">{GROUP_LABEL[item.group]}</div>
            {/if}
            {@const Icon = item.icon}
            <div
              class="item"
              class:active={i === palette.activeIndex}
              id={`palette-opt-${i}`}
              data-index={i}
              role="option"
              tabindex="-1"
              aria-selected={i === palette.activeIndex}
              onmousemove={() => palette.activeIndex !== i && (palette.activeIndex = i)}
              onclick={() => run(i)}
              onkeydown={(e) => e.key === 'Enter' && run(i)}
            >
              <span class="tile">{#if Icon}<Icon size={15} />{/if}</span>
              <span class="title">
                {#each highlight(item.title, palette.query) as part}{#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}{/each}
              </span>
              {#if item.hint}
                <span class="hint" class:mono={item.group !== 'action'}>
                  {#each highlight(item.hint, palette.query) as part}{#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}{/each}
                </span>
              {/if}
              {#if item.keys}<span class="keys"><Kbd keys={item.keys} /></span>{/if}
              {#if i === palette.activeIndex}<span class="enter"><CornerDownLeft size={13} /></span>{/if}
            </div>
          {/each}
        {/if}
      </div>
      <div class="footer">
        <span><ArrowUp size={12} /><ArrowDown size={12} />navigate</span>
        <span><CornerDownLeft size={12} />select</span>
        <span><Kbd keys="Esc" />close</span>
        <span class="count">{items.length} {items.length === 1 ? 'result' : 'results'}</span>
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding: 12vh var(--space-5) 0;
    background: color-mix(in srgb, #000 50%, transparent);
    backdrop-filter: blur(2px);
    z-index: var(--z-palette, 700);
  }
  .palette {
    display: flex;
    flex-direction: column;
    width: 620px;
    max-width: 100%;
    max-height: min(520px, 76vh);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
    outline: none;
  }
  .search {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: 0 var(--space-5);
    height: 52px;
    flex: none;
    border-bottom: 1px solid var(--border);
    color: var(--text-2);
  }
  .search input {
    flex: 1;
    min-width: 0;
    height: 100%;
    padding: 0;
    background: transparent;
    border: none;
    outline: none;
    box-shadow: none;
    color: var(--text-0);
    font-size: var(--fs-lg);
  }
  .search input:focus {
    box-shadow: none;
  }
  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--space-3);
    scroll-padding: 32px var(--space-3);
  }
  .group {
    padding: 10px var(--space-3) 4px;
    color: var(--text-2);
    font-size: 10.5px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.07em;
  }
  .item {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    height: 38px;
    padding: 0 var(--space-3) 0 6px;
    border-radius: var(--radius);
    color: var(--text-0);
    font-size: var(--fs-md);
    cursor: pointer;
  }
  .item.active {
    background: var(--accent-subtle);
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .tile {
    display: grid;
    place-items: center;
    flex: none;
    width: 26px;
    height: 26px;
    border-radius: 7px;
    background: var(--bg-3);
    color: var(--text-1);
  }
  .item.active .tile {
    background: color-mix(in srgb, var(--accent) 20%, transparent);
    color: var(--accent);
  }
  .title {
    flex: none;
    max-width: 55%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hint {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .hint.mono {
    font-family: var(--font-mono);
    font-size: 10.5px;
  }
  .keys {
    margin-left: auto;
    flex: none;
  }
  .enter {
    display: inline-flex;
    flex: none;
    color: var(--accent);
  }
  mark {
    background: transparent;
    color: var(--accent);
    font-weight: 600;
  }
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 40px var(--space-5);
    text-align: center;
  }
  .empty .ico {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    margin-bottom: 6px;
    border-radius: 11px;
    background: var(--bg-3);
    color: var(--text-1);
  }
  .empty .t {
    color: var(--text-0);
    font-weight: 600;
  }
  .empty .s {
    max-width: 340px;
    color: var(--text-2);
    font-size: var(--fs-sm);
    overflow-wrap: anywhere;
  }
  .footer {
    display: flex;
    align-items: center;
    gap: var(--space-5);
    flex: none;
    height: 34px;
    padding: 0 var(--space-5);
    border-top: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-0) 45%, var(--bg-1));
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .footer span {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .footer .count {
    margin-left: auto;
    font-variant-numeric: tabular-nums;
  }
</style>
