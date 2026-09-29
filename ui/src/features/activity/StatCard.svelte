<script lang="ts">
  import type { Component } from 'svelte';
  import { router } from '../../lib/router.svelte';
  import type { Tone } from '../../lib/activity';

  let {
    label,
    value,
    sub = '',
    icon: Icon,
    tone = 'neutral',
    href = ''
  }: {
    label: string;
    value: string | number;
    sub?: string;
    icon: Component<{ size?: number }>;
    tone?: Tone;
    href?: string;
  } = $props();
</script>

{#if href}
  <a class="stat card-link {tone}" href={`#${href}`} use:router.link={href}>
    <span class="stat-label"><span class="ic"><Icon size={14} /></span>{label}</span>
    <span class="stat-value">{value}</span>
    <span class="stat-sub">{sub || ' '}</span>
  </a>
{:else}
  <div class="stat {tone}">
    <span class="stat-label"><span class="ic"><Icon size={14} /></span>{label}</span>
    <span class="stat-value">{value}</span>
    <span class="stat-sub">{sub || ' '}</span>
  </div>
{/if}

<style>
  .stat {
    --c: var(--text-2);
    color: var(--text-0);
    text-decoration: none;
    position: relative;
    min-width: 0;
    transition:
      border-color 0.12s ease,
      background 0.12s ease,
      transform 0.12s ease;
  }
  .stat.ok {
    --c: var(--ok);
  }
  .stat.err {
    --c: var(--err);
  }
  .stat.warn {
    --c: var(--warn);
  }
  .stat.info {
    --c: var(--info);
  }
  .ic {
    display: inline-grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 6px;
    color: var(--c);
    background: color-mix(in srgb, var(--c) 13%, transparent);
  }
  .card-link:hover {
    border-color: var(--border-strong);
    background: var(--bg-2);
  }
  .card-link:active {
    transform: translateY(1px);
  }
  .card-link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .stat.err .stat-value {
    color: var(--err);
  }
  .stat-sub {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
