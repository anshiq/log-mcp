<script lang="ts">
  import type { Component, Snippet } from 'svelte';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import { router } from '../../lib/router.svelte';

  let {
    title,
    icon: Icon,
    count,
    href = '',
    linkLabel = 'View all',
    tone = 'neutral',
    actions
  }: {
    title: string;
    icon: Component<{ size?: number }>;
    count?: number | string;
    href?: string;
    linkLabel?: string;
    tone?: 'neutral' | 'err' | 'ok' | 'warn';
    actions?: Snippet;
  } = $props();
</script>

<div class="card-header ch {tone}">
  <span class="ico"><Icon size={16} /></span>
  <span class="title">{title}</span>
  {#if count !== undefined && count !== ''}<span class="badge">{count}</span>{/if}
  <span class="spacer"></span>
  {@render actions?.()}
  {#if href}
    <a class="more" href={`#${href}`} use:router.link={href}>{linkLabel}<ArrowRight size={13} /></a>
  {/if}
</div>

<style>
  .ch {
    min-height: 52px;
  }
  .ico {
    display: inline-flex;
    color: var(--text-2);
  }
  .ch.err .ico {
    color: var(--err);
  }
  .ch.ok .ico {
    color: var(--ok);
  }
  .ch.warn .ico {
    color: var(--warn);
  }
  .spacer {
    flex: 1;
  }
  .title {
    font-weight: 600;
  }
  .more {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: var(--fs-xs);
    font-weight: 500;
    color: var(--text-1);
    text-decoration: none;
    padding: 3px 6px;
    border-radius: var(--radius-sm);
  }
  .more:hover {
    color: var(--text-0);
    background: var(--bg-hover);
  }
</style>
