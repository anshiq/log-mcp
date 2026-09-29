<script lang="ts">
  import type { Snippet } from 'svelte';
  import { floating } from './floating';
  import { layer, outsideClick } from './overlay';
  import { pop } from './motion';
  import MenuList from './MenuList.svelte';
  import type { MenuItem } from './menu';

  let {
    items = [],
    label = 'Context menu',
    onSelect,
    children
  }: {
    items?: MenuItem[];
    label?: string;
    onSelect?: (id: string) => void;
    children?: Snippet;
  } = $props();

  let point = $state<{ x: number; y: number } | null>(null);
  let host: HTMLDivElement | null = $state(null);

  function onContext(e: MouseEvent) {
    e.preventDefault();
    point = { x: e.clientX, y: e.clientY };
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'ContextMenu' || (e.shiftKey && e.key === 'F10')) {
      e.preventDefault();
      const r = (e.target as HTMLElement).getBoundingClientRect();
      point = { x: r.left + 12, y: r.bottom - 4 };
    }
  }

  function close() {
    point = null;
  }
</script>

<div bind:this={host} class="host" oncontextmenu={onContext} onkeydown={onKey} role="presentation">
  {#if children}{@render children()}{/if}
</div>

{#if point}
  <div
    class="pane"
    use:floating={{ anchor: point, placement: 'bottom-start', offset: 2 }}
    use:layer={{ onClose: close, focus: 'none' }}
    use:outsideClick={{ onOutside: close }}
    oncontextmenu={(e) => e.preventDefault()}
    in:pop={{ duration: 110, y: 2 }}
    out:pop={{ duration: 80, y: 2 }}
    role="presentation"
  >
    <MenuList {items} {label} {onSelect} onClose={close} />
  </div>
{/if}

<svelte:window onblur={close} onresize={close} />

<style>
  .host {
    display: contents;
  }
  .pane {
    display: flex;
  }
</style>
