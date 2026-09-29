<script lang="ts">
  import type { Snippet } from 'svelte';
  import { floating, type Placement } from './floating';
  import Kbd from './Kbd.svelte';
  import { fadeIn } from './motion';

  let {
    text,
    placement = 'top',
    delay = 350,
    shortcut = '',
    children
  }: { text: string; placement?: Placement; delay?: number; shortcut?: string; children?: Snippet } = $props();

  const uid = `tip-${Math.random().toString(36).slice(2, 9)}`;
  let visible = $state(false);
  let anchor: HTMLElement | null = $state(null);
  let timer: ReturnType<typeof setTimeout> | null = null;

  function show() {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => (visible = true), delay);
  }

  function hide() {
    if (timer) clearTimeout(timer);
    timer = null;
    visible = false;
  }
</script>

<span
  class="wrap"
  bind:this={anchor}
  role="presentation"
  aria-describedby={visible ? uid : undefined}
  onmouseenter={show}
  onmouseleave={hide}
  onfocusin={show}
  onfocusout={hide}
  onkeydown={(e) => e.key === 'Escape' && hide()}
  onmousedown={hide}
>
  {@render children?.()}
</span>

{#if visible}
  <span class="bubble" id={uid} role="tooltip" use:floating={{ anchor, placement, offset: 8 }} in:fadeIn={{ duration: 100 }} out:fadeIn={{ duration: 80 }}>
    {text}
    {#if shortcut}<Kbd keys={shortcut} />{/if}
  </span>
{/if}

<style>
  .wrap {
    display: inline-flex;
  }
  .bubble {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    max-width: 280px;
    padding: 5px 9px;
    background: var(--bg-3);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-pop);
    color: var(--text-0);
    font-size: var(--fs-xs);
    line-height: 1.4;
    pointer-events: none;
    z-index: var(--z-popover, 900);
  }
</style>
