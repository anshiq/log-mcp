<script lang="ts">
  import type { Snippet } from 'svelte';
  import { floating, type Placement } from './floating';
  import { layer, outsideClick } from './overlay';
  import { pop } from './motion';

  let {
    open = $bindable(false),
    placement = 'bottom-start',
    width,
    label = 'Popover',
    onClose,
    children,
    trigger
  }: {
    open?: boolean;
    placement?: Placement;
    width?: number;
    label?: string;
    onClose?: () => void;
    children?: Snippet;
    trigger?: Snippet;
  } = $props();

  let anchor: HTMLElement | null = $state(null);

  function close() {
    open = false;
    onClose?.();
  }
</script>

<span class="pop" bind:this={anchor}>
  <span class="trig">{#if trigger}{@render trigger()}{/if}</span>
</span>

{#if open}
  <div
    class="body"
    style={width ? `width:${width}px` : undefined}
    use:floating={{ anchor, placement }}
    use:layer={{ onClose: close, focus: 'none', restoreFocus: true }}
    use:outsideClick={{ onOutside: close, ignore: () => [anchor] }}
    in:pop={{ duration: 130, y: 4 }}
    out:pop={{ duration: 90, y: 4 }}
    role="dialog"
    aria-label={label}
    tabindex="-1"
  >
    {#if children}{@render children()}{/if}
  </div>
{/if}

<style>
  .pop {
    display: inline-flex;
  }
  .trig {
    display: inline-flex;
  }
  .body {
    min-width: 200px;
    max-width: min(420px, 94vw);
    padding: var(--space-4);
    overflow: auto;
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    box-shadow: var(--shadow-pop);
    outline: none;
  }
</style>
