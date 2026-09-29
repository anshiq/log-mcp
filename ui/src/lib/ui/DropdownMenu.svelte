<script lang="ts">
  import type { Snippet } from 'svelte';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import { floating, type Placement } from './floating';
  import { layer, outsideClick } from './overlay';
  import { pop } from './motion';
  import MenuList from './MenuList.svelte';
  import type { MenuItem } from './menu';

  let {
    items = [],
    label = 'Menu',
    placement = 'bottom-start',
    disabled = false,
    onSelect,
    trigger,
    children
  }: {
    items?: MenuItem[];
    label?: string;
    placement?: Placement;
    disabled?: boolean;
    onSelect?: (id: string) => void;
    trigger?: Snippet<[{ open: boolean }]>;
    children?: Snippet;
  } = $props();

  let open = $state(false);
  let anchor: HTMLElement | null = $state(null);

  function close(restore = true) {
    open = false;
    if (restore) anchor?.querySelector<HTMLElement>('button')?.focus();
  }

  function onTriggerKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      open = true;
    }
  }
</script>

<span class="dd" bind:this={anchor}>
  {#if trigger}
    <span
      class="custom"
      role="presentation"
      onclick={() => !disabled && (open = !open)}
      onkeydown={onTriggerKey}
    >
      {@render trigger({ open })}
    </span>
  {:else}
    <button type="button" class="trig" class:open aria-haspopup="menu" aria-expanded={open} {disabled} onclick={() => (open = !open)} onkeydown={onTriggerKey}>
      {label}
      <ChevronDown size={14} />
    </button>
  {/if}
</span>

{#if open}
  <div
    class="pane"
    use:floating={{ anchor, placement }}
    use:layer={{ onClose: () => close(), focus: 'none', restoreFocus: false }}
    use:outsideClick={{ onOutside: () => close(false), ignore: () => [anchor] }}
    in:pop={{ duration: 120, y: 4 }}
    out:pop={{ duration: 90, y: 4 }}
  >
    {#if children}<div class="extra">{@render children()}</div>{/if}
    <MenuList {items} {label} {onSelect} onClose={() => close()} />
  </div>
{/if}

<style>
  .dd {
    display: inline-flex;
  }
  .custom {
    display: inline-flex;
  }
  .trig {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: var(--control-h);
    padding: 0 8px 0 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-2);
    color: var(--text-0);
    font-size: var(--fs-sm);
    font-weight: 500;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease);
  }
  .trig:hover:not(:disabled),
  .trig.open {
    background: var(--bg-3);
    border-color: var(--border-strong);
  }
  .trig:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .pane {
    display: flex;
    flex-direction: column;
  }
  .extra {
    padding: 4px;
  }
</style>
