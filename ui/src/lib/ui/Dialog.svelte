<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import type { Snippet } from 'svelte';

  let {
    title,
    width = 460,
    onClose,
    children,
    footer
  }: {
    title?: string;
    width?: number;
    onClose: () => void;
    children?: Snippet;
    footer?: Snippet;
  } = $props();

  let dialogEl: HTMLDivElement | null = $state(null);
  let previouslyFocused: HTMLElement | null = null;

  function focusables(): HTMLElement[] {
    if (!dialogEl) return [];
    return [...dialogEl.querySelectorAll<HTMLElement>('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')].filter(
      (el) => !el.hasAttribute('disabled')
    );
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onClose();
      return;
    }
    if (e.key !== 'Tab') return;
    const els = focusables();
    if (els.length === 0) return;
    const first = els[0];
    const last = els[els.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last?.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first?.focus();
    }
  }

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement | null;
    void tick().then(() => {
      focusables()[0]?.focus();
    });
    window.addEventListener('keydown', onKeydown, true);
  });

  onDestroy(() => {
    window.removeEventListener('keydown', onKeydown, true);
    previouslyFocused?.focus();
  });
</script>

<div class="overlay" onclick={onClose} role="presentation">
  <div
    class="dialog"
    style="width: {width}px"
    bind:this={dialogEl}
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
    role="dialog"
    aria-modal="true"
    aria-label={title}
    tabindex="-1"
  >
    {#if title}
      <div class="header">
        <h2>{title}</h2>
        <button class="close" onclick={onClose} aria-label="Close">✕</button>
      </div>
    {/if}
    <div class="body">
      {@render children?.()}
    </div>
    {#if footer}
      <div class="footer">
        {@render footer()}
      </div>
    {/if}
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .dialog {
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    display: flex;
    flex-direction: column;
    max-height: 85vh;
  }
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-5) var(--space-6) 0;
  }
  .header h2 {
    margin: 0;
    font-size: var(--fs-lg);
  }
  .close {
    background: transparent;
    border: none;
    color: var(--text-1);
    font-size: var(--fs-md);
  }
  .body {
    padding: var(--space-6);
    overflow: auto;
  }
  .footer {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
    padding: 0 var(--space-6) var(--space-6);
  }
</style>
