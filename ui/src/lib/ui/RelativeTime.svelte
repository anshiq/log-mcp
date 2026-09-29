<script lang="ts">
  import { relativeTime } from '../format';
  import { clock } from '../state/clock.svelte';

  let { ts = 0, empty = '—' }: { ts?: number | null; empty?: string } = $props();

  $effect(() => clock.retain());

  const exact = $derived(ts ? new Date(ts).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'medium' }) : '');
  const label = $derived.by(() => {
    void clock.now;
    return ts ? relativeTime(ts) : empty;
  });
</script>

{#if ts}
  <time datetime={new Date(ts).toISOString()} title={exact}>{label}</time>
{:else}
  <span class="none">{empty}</span>
{/if}

<style>
  time {
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .none {
    color: var(--text-2);
  }
</style>
