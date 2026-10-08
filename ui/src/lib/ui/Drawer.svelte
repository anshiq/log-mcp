<script lang="ts">
  import type { Snippet } from 'svelte';
  import X from '@lucide/svelte/icons/x';
  import { layer } from './overlay';
  import { slide, fadeIn } from './motion';

  let {
    open = $bindable(false),
    width = 520,
    title = '',
    description = '',
    side = 'right',
    onClose,
    children,
    footer
  }: {
    open?: boolean;
    width?: number;
    title?: string;
    description?: string;
    side?: 'right' | 'left';
    onClose?: () => void;
    children?: Snippet;
    footer?: Snippet;
  } = $props();

  const uid = `drw-${Math.random().toString(36).slice(2, 9)}`;

  function close() {
    open = false;
    onClose?.();
  }
</script>

{#if open}
  <div class="backdrop" in:fadeIn out:fadeIn onmousedown={close} role="presentation"></div>
  <div
    class="drawer {side}"
    style={`width:${width}px`}
    in:slide={{ x: side === 'right' ? 32 : -32 }}
    out:slide={{ x: side === 'right' ? 32 : -32 }}
    use:layer={{ onClose: close, trap: true }}
    role="dialog"
    aria-modal="true"
    aria-labelledby={title ? `${uid}-t` : undefined}
    aria-label={title ? undefined : 'Panel'}
    tabindex="-1"
  >
    <div class="header">
      <div class="titles">
        {#if title}<h2 id={`${uid}-t`}>{title}</h2>{/if}
        {#if description}<p>{description}</p>{/if}
      </div>
      <button class="close" type="button" aria-label="Close panel" onclick={close}><X size={16} /></button>
    </div>
    <div class="body" data-layer-body>
      {#if children}{@render children()}{/if}
    </div>
    {#if footer}<div class="footer">{@render footer()}</div>{/if}
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: var(--backdrop);
    z-index: var(--z-drawer, 450);
  }
  .drawer {
    position: fixed;
    top: 0;
    bottom: 0;
    max-width: 100vw;
    display: flex;
    flex-direction: column;
    background: var(--bg-1);
    box-shadow: var(--shadow-pop);
    z-index: var(--z-drawer, 450);
    outline: none;
  }
  .drawer.right {
    right: 0;
    border-left: 1px solid var(--border-strong);
  }
  .drawer.left {
    left: 0;
    border-right: 1px solid var(--border-strong);
  }
  .header {
    display: flex;
    align-items: flex-start;
    gap: var(--space-4);
    min-height: 40px;
    padding: 0 8px 0 12px;
    border-bottom: 1px solid var(--border);
  }
  .titles {
    flex: 1;
    min-width: 0;
  }
  .titles h2 {
    font-size: var(--fs-lg);
  }
  .titles p {
    margin-top: 2px;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    padding: 0;
    border: none;
    border-radius: var(--radius);
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
  }
  .close:hover {
    background: var(--bg-hover);
    color: var(--text-0);
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 12px;
  }
  .footer {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
    padding: 8px 12px;
    border-top: 1px solid var(--border);
  }
</style>
