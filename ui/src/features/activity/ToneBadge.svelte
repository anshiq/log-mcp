<script lang="ts">
  import type { Component } from 'svelte';
  import type { Tone } from '../../lib/activity';

  let {
    icon: Icon,
    tone = 'neutral',
    size = 28
  }: { icon: Component<{ size?: number }>; tone?: Tone; size?: number } = $props();
</script>

<span class="tb {tone}" style:width="{size}px" style:height="{size}px" aria-hidden="true">
  <Icon size={Math.round(size * 0.5)} />
</span>

<style>
  .tb {
    display: inline-grid;
    place-items: center;
    flex: none;
    border-radius: 8px;
    color: var(--c);
    background: color-mix(in srgb, var(--c) 14%, transparent);
    border: 1px solid color-mix(in srgb, var(--c) 26%, transparent);
    --c: var(--neutral);
  }
  .tb.ok {
    --c: var(--ok);
  }
  .tb.err {
    --c: var(--err);
  }
  .tb.warn {
    --c: var(--warn);
  }
  .tb.info {
    --c: var(--info);
  }
  .tb.neutral {
    --c: var(--text-1);
  }
</style>
