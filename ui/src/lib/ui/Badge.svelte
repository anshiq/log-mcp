<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    tone = 'neutral',
    dot = false,
    size = 'md',
    children
  }: {
    tone?: 'neutral' | 'ok' | 'warn' | 'err' | 'info' | 'accent';
    dot?: boolean;
    size?: 'sm' | 'md';
    children?: Snippet;
  } = $props();
</script>

<span class="badge {tone} {size}">
  {#if dot}<span class="d"></span>{/if}
  {@render children?.()}
</span>

<style>
  .badge {
    --c: var(--text-1);
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 1px 8px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-3);
    color: var(--c);
    font-size: var(--fs-xs);
    font-weight: 500;
    line-height: 1.6;
    white-space: nowrap;
  }
  .badge.sm {
    padding: 0 6px;
    font-size: var(--fs-micro);
    line-height: 1.55;
  }
  .badge.ok {
    --c: var(--ok);
  }
  .badge.warn {
    --c: var(--warn);
  }
  .badge.err {
    --c: var(--err);
  }
  .badge.info {
    --c: var(--info);
  }
  .badge.accent {
    --c: var(--accent);
  }
  .badge:not(.neutral) {
    background: color-mix(in srgb, var(--c) 12%, transparent);
    border-color: color-mix(in srgb, var(--c) 30%, transparent);
  }
  .d {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--c);
    flex: none;
  }
</style>
