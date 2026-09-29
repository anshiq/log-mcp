<script lang="ts" module>
  import type { Component } from 'svelte';

  export interface MenuItem {
    type?: 'item';
    id: string;
    label: string;
    icon?: Component<{ size?: number | string }>;
    hint?: string;
    danger?: boolean;
    disabled?: boolean;
    onselect: () => void;
  }
  export interface MenuSeparator {
    type: 'separator';
  }
  export interface MenuHeading {
    type: 'heading';
    label: string;
  }
  export type MenuEntry = MenuItem | MenuSeparator | MenuHeading;
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';
  import { tick } from 'svelte';

  let {
    items,
    label,
    triggerClass = 'btn ghost icon sm',
    align = 'end',
    disabled = false,
    open = $bindable(false),
    children
  }: {
    items: MenuEntry[];
    label: string;
    triggerClass?: string;
    align?: 'start' | 'end';
    disabled?: boolean;
    open?: boolean;
    children?: Snippet;
  } = $props();

  let trigger: HTMLButtonElement | null = $state(null);
  let menu: HTMLDivElement | null = $state(null);
  let pos = $state({ left: 0, top: 0, ready: false });

  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return {
      destroy() {
        node.remove();
      }
    };
  }

  function enabledButtons(): HTMLButtonElement[] {
    return menu ? [...menu.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]:not(:disabled)')] : [];
  }

  function place() {
    if (!trigger || !menu) return;
    const t = trigger.getBoundingClientRect();
    const m = menu.getBoundingClientRect();
    const margin = 8;
    let left = align === 'end' ? t.right - m.width : t.left;
    left = Math.max(margin, Math.min(left, window.innerWidth - m.width - margin));
    let top = t.bottom + 6;
    if (top + m.height > window.innerHeight - margin) top = Math.max(margin, t.top - m.height - 6);
    pos = { left, top, ready: true };
  }

  async function show() {
    pos = { ...pos, ready: false };
    open = true;
    await tick();
    place();
    enabledButtons()[0]?.focus();
  }

  function close(refocus = true) {
    if (!open) return;
    open = false;
    if (refocus) trigger?.focus();
  }

  function toggle(e: MouseEvent) {
    e.stopPropagation();
    if (open) close(false);
    else void show();
  }

  function onTriggerKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' && !open) {
      e.preventDefault();
      e.stopPropagation();
      void show();
    }
  }

  function onMenuKey(e: KeyboardEvent) {
    const btns = enabledButtons();
    const idx = btns.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      close();
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      btns[(idx + 1) % btns.length]?.focus();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      btns[(idx - 1 + btns.length) % btns.length]?.focus();
    } else if (e.key === 'Home') {
      e.preventDefault();
      btns[0]?.focus();
    } else if (e.key === 'End') {
      e.preventDefault();
      btns[btns.length - 1]?.focus();
    } else if (e.key === 'Tab') {
      close(false);
    }
    e.stopPropagation();
  }

  function choose(item: MenuItem) {
    close();
    item.onselect();
  }

  function onWindowPointer(e: PointerEvent) {
    if (!open) return;
    const n = e.target as Node;
    if (menu?.contains(n) || trigger?.contains(n)) return;
    open = false;
  }

  function onWindowScroll(e: Event) {
    if (!open) return;
    if (menu && e.target instanceof Node && menu.contains(e.target)) return;
    open = false;
  }
</script>

<svelte:window onpointerdown={onWindowPointer} onresize={() => close(false)} />
<svelte:document onscrollcapture={onWindowScroll} />

<button
  bind:this={trigger}
  class={triggerClass}
  class:open
  type="button"
  aria-haspopup="menu"
  aria-expanded={open}
  aria-label={label}
  title={label}
  {disabled}
  onclick={toggle}
  onkeydown={onTriggerKey}
>
  {@render children?.()}
</button>

{#if open}
  <div
    use:portal
    bind:this={menu}
    class="menu"
    class:ready={pos.ready}
    role="menu"
    tabindex="-1"
    aria-label={label}
    style="left:{pos.left}px;top:{pos.top}px"
    onkeydown={onMenuKey}
  >
    {#each items as entry, i (i)}
      {#if entry.type === 'separator'}
        <div class="sep" role="separator"></div>
      {:else if entry.type === 'heading'}
        <div class="heading" role="presentation">{entry.label}</div>
      {:else}
        {@const Icon = entry.icon}
        <button
          type="button"
          role="menuitem"
          class="item"
          class:danger={entry.danger}
          disabled={entry.disabled}
          onclick={() => choose(entry)}
        >
          <span class="ico">{#if Icon}<Icon size={14} />{/if}</span>
          <span class="text">{entry.label}</span>
          {#if entry.hint}<span class="hint mono">{entry.hint}</span>{/if}
        </button>
      {/if}
    {/each}
  </div>
{/if}

<style>
  .menu {
    position: fixed;
    z-index: 300;
    min-width: 196px;
    max-width: 280px;
    padding: 5px;
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    opacity: 0;
    transform: translateY(-3px) scale(0.985);
    transform-origin: top right;
    outline: none;
  }
  .menu.ready {
    opacity: 1;
    transform: none;
    transition: opacity var(--dur-fast) var(--ease), transform var(--dur-fast) var(--ease);
  }
  .item {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 6px 9px;
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-0);
    font-size: var(--fs-sm);
    text-align: left;
    cursor: pointer;
  }
  .item:hover:not(:disabled),
  .item:focus-visible {
    background: var(--bg-hover);
    outline: none;
  }
  .item:focus-visible {
    background: var(--accent-subtle);
  }
  .item:disabled {
    opacity: 0.45;
  }
  .item.danger {
    color: var(--err);
  }
  .item.danger:hover:not(:disabled),
  .item.danger:focus-visible {
    background: color-mix(in srgb, var(--err) 12%, transparent);
  }
  .ico {
    width: 14px;
    height: 14px;
    display: inline-grid;
    place-items: center;
    color: var(--text-2);
    flex: none;
  }
  .item.danger .ico {
    color: inherit;
  }
  .text {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
  }
  .hint {
    color: var(--text-2);
    font-size: 10.5px;
  }
  .sep {
    height: 1px;
    margin: 5px 4px;
    background: var(--border);
  }
  .heading {
    padding: 6px 9px 4px;
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-2);
  }
</style>
