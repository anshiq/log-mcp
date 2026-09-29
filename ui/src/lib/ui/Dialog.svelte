<script lang="ts">
  import type { Component, Snippet } from 'svelte';
  import X from '@lucide/svelte/icons/x';
  import { layer } from './overlay';
  import { pop, fadeIn } from './motion';

  let {
    title,
    description,
    icon,
    tone = 'default',
    width = 460,
    flush = false,
    dismissible = true,
    onClose,
    children,
    footer
  }: {
    title?: string;
    description?: string;
    icon?: Component<{ size?: number }>;
    tone?: 'default' | 'danger' | 'warn';
    width?: number;
    flush?: boolean;
    dismissible?: boolean;
    onClose: () => void;
    children?: Snippet;
    footer?: Snippet;
  } = $props();

  const uid = `dlg-${Math.random().toString(36).slice(2, 9)}`;
</script>

<div
  class="overlay"
  in:fadeIn
  out:fadeIn
  onmousedown={(e) => {
    if (dismissible && e.target === e.currentTarget) onClose();
  }}
  role="presentation"
>
  <div
    class="dialog"
    style={`width:${width}px`}
    in:pop
    out:pop
    use:layer={{ onClose: () => dismissible && onClose(), trap: true, escape: dismissible }}
    role="dialog"
    aria-modal="true"
    aria-labelledby={title ? `${uid}-t` : undefined}
    aria-describedby={description ? `${uid}-d` : undefined}
    tabindex="-1"
  >
    {#if title}
      <div class="header">
        {#if icon}
          {@const Icon = icon}
          <span class="badge {tone}"><Icon size={16} /></span>
        {/if}
        <div class="titles">
          <h2 id={`${uid}-t`}>{title}</h2>
          {#if description}<p id={`${uid}-d`}>{description}</p>{/if}
        </div>
        {#if dismissible}
          <button class="close" type="button" onclick={onClose} aria-label="Close dialog"><X size={16} /></button>
        {/if}
      </div>
    {/if}
    <div class="body" class:flush data-layer-body>
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
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-6);
    background: color-mix(in srgb, #000 55%, transparent);
    backdrop-filter: blur(2px);
    z-index: var(--z-dialog, 500);
  }
  .dialog {
    display: flex;
    flex-direction: column;
    max-width: 100%;
    max-height: min(85vh, 100%);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    outline: none;
  }
  .header {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-5) var(--space-5) var(--space-3) var(--space-6);
  }
  .titles {
    flex: 1;
    min-width: 0;
  }
  .titles h2 {
    font-size: var(--fs-lg);
    line-height: 1.35;
  }
  .titles p {
    margin-top: 2px;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .badge {
    display: grid;
    place-items: center;
    flex: none;
    width: 32px;
    height: 32px;
    border-radius: 9px;
    background: var(--accent-subtle);
    color: var(--accent);
  }
  .badge.danger {
    background: color-mix(in srgb, var(--err) 14%, transparent);
    color: var(--err);
  }
  .badge.warn {
    background: color-mix(in srgb, var(--warn) 14%, transparent);
    color: var(--warn);
  }
  .close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 28px;
    height: 28px;
    padding: 0;
    margin: 0;
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
    padding: var(--space-3) var(--space-6) var(--space-6);
    overflow: auto;
    min-height: 0;
    color: var(--text-1);
    font-size: var(--fs-md);
  }
  .body.flush {
    padding: 0;
  }
  .footer {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-3);
    padding: var(--space-4) var(--space-6);
    border-top: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-0) 45%, var(--bg-1));
    border-radius: 0 0 var(--radius-lg) var(--radius-lg);
  }
</style>
