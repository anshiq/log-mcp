<script lang="ts">
  import { duration } from '../format';

  let { ms = 0, empty = '—' }: { ms?: number | null; empty?: string } = $props();
</script>

<span class="dur">{ms === null || ms === undefined ? empty : duration(ms)}</span>

<style>
  .dur {
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
</style>
