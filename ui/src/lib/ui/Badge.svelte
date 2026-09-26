<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    tone = 'neutral',
    children
  }: { tone?: 'neutral' | 'ok' | 'warn' | 'err' | 'info'; children?: Snippet } = $props();
</script>

<span class="badge {tone}">{@render children?.()}</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    background: var(--bg-3);
    border-radius: var(--radius-sm);
    padding: 0 var(--space-2);
    font-size: var(--fs-xs);
    color: var(--text-1);
    line-height: 1.6;
  }
  .badge.ok {
    color: var(--ok);
  }
  .badge.warn {
    color: var(--warn);
  }
  .badge.err {
    color: var(--err);
  }
  .badge.info {
    color: var(--info);
  }
</style>
