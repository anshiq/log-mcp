<script lang="ts">
  import { onMount, tick, type Snippet } from 'svelte';

  interface TriggerProps {
    toggle: () => void;
    open: boolean;
    attrs: { 'aria-haspopup': 'true'; 'aria-expanded': boolean; 'aria-controls': string };
  }

  interface Props {
    open?: boolean;
    align?: 'start' | 'end';
    width?: number;
    label?: string;
    trigger: Snippet<[TriggerProps]>;
    children: Snippet<[() => void]>;
    onOpen?: () => void;
  }

  let { open = $bindable(false), align = 'start', width = 320, label = 'Menu', trigger, children, onOpen }: Props = $props();

  let root = $state<HTMLDivElement | null>(null);
  let panel = $state<HTMLDivElement | null>(null);
  let shiftX = $state(0);
  const uid = `lp-${Math.random().toString(36).slice(2, 9)}`;

  function toggle() {
    open = !open;
    if (open) onOpen?.();
  }

  function close() {
    open = false;
    const btn = root?.querySelector<HTMLElement>('button, [tabindex]');
    btn?.focus();
  }

  function onDocPointer(e: PointerEvent) {
    if (!open || !root) return;
    if (!root.contains(e.target as Node)) open = false;
  }

  function onKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') {
      e.stopPropagation();
      close();
    }
  }

  onMount(() => {
    document.addEventListener('pointerdown', onDocPointer, true);
    return () => document.removeEventListener('pointerdown', onDocPointer, true);
  });

  $effect(() => {
    if (!open) {
      shiftX = 0;
      return;
    }
    void tick().then(() => {
      if (!panel) return;
      const r = panel.getBoundingClientRect();
      const margin = 12;
      if (r.right > window.innerWidth - margin) shiftX = window.innerWidth - margin - r.right;
      else if (r.left < margin) shiftX = margin - r.left;
    });
  });
</script>

<svelte:window onkeydown={onKey} />

<div class="lp" bind:this={root}>
  {@render trigger({ toggle, open, attrs: { 'aria-haspopup': 'true', 'aria-expanded': open, 'aria-controls': uid } })}
  {#if open}
    <div id={uid} class="panel {align}" bind:this={panel} style="width:min({width}px, calc(100vw - 24px));transform:translateX({shiftX}px)" role="group" aria-label={label}>
      {@render children(close)}
    </div>
  {/if}
</div>

<style>
  .lp {
    position: relative;
    display: inline-flex;
    min-width: 0;
  }

  .panel {
    position: absolute;
    top: calc(100% + 6px);
    z-index: 60;
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
    animation: lp-in var(--dur) var(--ease);
  }

  .panel.start {
    left: 0;
  }

  .panel.end {
    right: 0;
  }

  @keyframes lp-in {
    from {
      opacity: 0;
      margin-top: -4px;
    }
    to {
      opacity: 1;
      margin-top: 0;
    }
  }
</style>
