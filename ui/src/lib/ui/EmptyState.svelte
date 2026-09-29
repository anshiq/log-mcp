<script lang="ts">
  import type { Component, Snippet } from 'svelte';

  let {
    title,
    body,
    icon,
    compact = false,
    action
  }: {
    title: string;
    body?: string;
    icon?: Component<{ size?: number }>;
    compact?: boolean;
    action?: Snippet;
  } = $props();
</script>

<div class="empty" class:compact>
  {#if icon}
    {@const Icon = icon}
    <div class="icon"><Icon size={compact ? 18 : 22} /></div>
  {/if}
  <p class="title">{title}</p>
  {#if body}<p class="body">{body}</p>{/if}
  {#if action}<div class="action">{@render action()}</div>{/if}
</div>

<style>
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: 56px var(--space-6);
    text-align: center;
  }
  .empty.compact {
    padding: var(--space-7) var(--space-5);
  }
  .icon {
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    border-radius: 12px;
    background: var(--bg-3);
    color: var(--text-1);
    margin-bottom: var(--space-2);
  }
  .compact .icon {
    width: 36px;
    height: 36px;
    border-radius: 10px;
  }
  .title {
    margin: 0;
    color: var(--text-0);
    font-size: var(--fs-lg);
    font-weight: 600;
  }
  .compact .title {
    font-size: var(--fs-md);
  }
  .body {
    margin: 0;
    max-width: 400px;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .action {
    margin-top: var(--space-3);
    display: flex;
    gap: var(--space-3);
  }
</style>
