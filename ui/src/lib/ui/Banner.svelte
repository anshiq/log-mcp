<script lang="ts">
  import type { Snippet } from 'svelte';
  import Info from '@lucide/svelte/icons/info';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import X from '@lucide/svelte/icons/x';

  let {
    tone = 'info',
    message = '',
    title = '',
    onDismiss,
    children,
    actions
  }: {
    tone?: 'info' | 'warn' | 'err' | 'ok';
    message?: string;
    title?: string;
    onDismiss?: () => void;
    children?: Snippet;
    actions?: Snippet;
  } = $props();

  const Icon = $derived(tone === 'warn' ? TriangleAlert : tone === 'err' ? CircleAlert : tone === 'ok' ? CircleCheck : Info);
</script>

<div class="banner {tone}" role={tone === 'err' || tone === 'warn' ? 'alert' : 'status'} data-tone={tone}>
  <span class="ico"><Icon size={16} /></span>
  <div class="content">
    {#if title}<strong>{title}</strong>{/if}
    {#if message}<span class="msg">{message}</span>{/if}
    {#if children}<span class="msg">{@render children()}</span>{/if}
  </div>
  {#if actions}<div class="actions">{@render actions()}</div>{/if}
  {#if onDismiss}
    <button type="button" class="x" aria-label="Dismiss" onclick={onDismiss}><X size={14} /></button>
  {/if}
</div>

<style>
  .banner {
    --c: var(--info);
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: 10px var(--space-4);
    border-radius: var(--radius);
    border: 1px solid color-mix(in srgb, var(--c) 35%, transparent);
    background: color-mix(in srgb, var(--c) 9%, var(--bg-1));
    font-size: var(--fs-sm);
    color: var(--text-0);
  }
  .banner.warn {
    --c: var(--warn);
  }
  .banner.err {
    --c: var(--err);
  }
  .banner.ok {
    --c: var(--ok);
  }
  .ico {
    display: inline-flex;
    padding-top: 1px;
    color: var(--c);
    flex: none;
  }
  .content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow-wrap: anywhere;
  }
  .msg {
    color: var(--text-1);
  }
  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: none;
  }
  .x {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 22px;
    height: 22px;
    padding: 0;
    border: none;
    background: transparent;
    border-radius: 4px;
    color: var(--text-2);
    cursor: pointer;
  }
  .x:hover {
    background: var(--bg-hover);
    color: var(--text-0);
  }
</style>
