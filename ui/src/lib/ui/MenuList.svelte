<script lang="ts">
  import type { MenuItem } from './menu';

  let {
    items = [],
    label = 'Menu',
    onSelect,
    onClose
  }: {
    items?: MenuItem[];
    label?: string;
    onSelect?: (id: string) => void;
    onClose?: () => void;
  } = $props();

  let root: HTMLDivElement | null = $state(null);

  const actionable = $derived(items.filter((i) => !i.separator && !i.heading));

  function buttons(): HTMLButtonElement[] {
    return root ? [...root.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]:not(:disabled)')] : [];
  }

  function move(delta: number, absolute?: number) {
    const list = buttons();
    if (list.length === 0) return;
    const current = list.indexOf(document.activeElement as HTMLButtonElement);
    let next = absolute ?? current + delta;
    if (absolute === undefined) next = (next + list.length) % list.length;
    list[Math.max(0, Math.min(list.length - 1, next))]?.focus();
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      move(1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      move(-1);
    } else if (e.key === 'Home') {
      e.preventDefault();
      move(0, 0);
    } else if (e.key === 'End') {
      e.preventDefault();
      move(0, buttons().length - 1);
    } else if (e.key === 'Tab') {
      e.preventDefault();
      onClose?.();
    } else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      const list = buttons();
      const start = list.indexOf(document.activeElement as HTMLButtonElement);
      const ordered = [...list.slice(start + 1), ...list.slice(0, start + 1)];
      ordered.find((b) => b.textContent?.trim().toLowerCase().startsWith(e.key.toLowerCase()))?.focus();
    }
  }

  function choose(it: MenuItem) {
    if (it.disabled) return;
    it.onSelect?.();
    onSelect?.(it.id);
    onClose?.();
  }

  $effect(() => {
    if (actionable.length === 0) return;
    queueMicrotask(() => buttons()[0]?.focus());
  });
</script>

<div class="menu" role="menu" aria-label={label} tabindex="-1" bind:this={root} onkeydown={onKey}>
  {#each items as it (it.id)}
    {#if it.separator}
      <div class="sep" role="separator"></div>
    {:else if it.heading}
      <div class="heading" role="presentation">{it.heading}</div>
    {:else}
      {@const Icon = it.icon}
      <button type="button" role="menuitem" class="item" class:danger={it.danger} disabled={it.disabled} tabindex="-1" onclick={() => choose(it)}>
        <span class="ic">{#if Icon}<Icon size={14} />{/if}</span>
        <span class="lbl">{it.label}</span>
        {#if it.hint}<span class="hint">{it.hint}</span>{/if}
      </button>
    {/if}
  {/each}
</div>

<style>
  .menu {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 180px;
    max-width: 320px;
    padding: 4px;
    overflow: auto;
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    box-shadow: var(--shadow-pop);
    outline: none;
  }
  .item {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: 8px;
    width: 100%;
    height: 26px;
    padding: 0 8px;
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-0);
    font-size: var(--fs-sm);
    font-weight: 400;
    text-align: left;
    cursor: pointer;
  }
  .item:hover:not(:disabled),
  .item:focus-visible {
    outline: none;
    background: var(--bg-3);
  }
  .item:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .item.danger {
    color: var(--err);
  }
  .item.danger:hover:not(:disabled),
  .item.danger:focus-visible {
    background: color-mix(in srgb, var(--err) 12%, transparent);
  }
  .ic {
    display: inline-flex;
    width: 14px;
    flex: none;
    color: var(--text-2);
  }
  .danger .ic {
    color: var(--err);
  }
  .lbl {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hint {
    color: var(--text-2);
    font-size: var(--fs-xs);
    font-family: var(--font-mono);
  }
  .sep {
    height: 1px;
    margin: 4px -4px;
    background: var(--border);
  }
  .heading {
    padding: 6px 8px 3px;
    color: var(--text-2);
    font-size: var(--fs-micro);
    font-weight: 600;
    
    letter-spacing: 0;
  }
</style>
