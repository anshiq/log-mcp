<script lang="ts">
  import type { Snippet } from 'svelte';

  let { text, children }: { text: string; children?: Snippet } = $props();

  let visible = $state(false);
  let timer: ReturnType<typeof setTimeout> | null = null;

  function show() {
    timer = setTimeout(() => (visible = true), 400);
  }
  function hide() {
    if (timer) clearTimeout(timer);
    visible = false;
  }
</script>

<span class="wrap" role="button" tabindex="0" onmouseenter={show} onmouseleave={hide} onfocusin={show} onfocusout={hide}>
  {@render children?.()}
  {#if visible}
    <span class="bubble" role="tooltip">{text}</span>
  {/if}
</span>

<style>
  .wrap {
    position: relative;
    display: inline-flex;
  }
  .bubble {
    position: absolute;
    bottom: calc(100% + var(--space-2));
    left: 50%;
    transform: translateX(-50%);
    background: var(--bg-3);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-3);
    font-size: var(--fs-xs);
    color: var(--text-0);
    white-space: nowrap;
    z-index: 200;
    box-shadow: var(--shadow-1);
    pointer-events: none;
  }
</style>
