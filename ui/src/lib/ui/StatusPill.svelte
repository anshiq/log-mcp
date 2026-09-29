<script lang="ts">
  let { status, plain = false }: { status?: string; plain?: boolean } = $props();

  function toneOf(s?: string): 'ok' | 'off' | 'bad' | 'busy' {
    if (s === 'running' || s === 'ready') return 'ok';
    if (s === 'exited' || s === 'stopped') return 'off';
    if (s === 'starting' || s === 'stopping') return 'busy';
    return 'bad';
  }

  const tone = $derived(toneOf(status));
</script>

<span class="pill {tone}" class:plain>
  <span class="dot"></span>
  {status ?? 'unknown'}
</span>

<style>
  .pill {
    --c: var(--err);
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 1px 9px 1px 8px;
    border-radius: 999px;
    border: 1px solid color-mix(in srgb, var(--c) 30%, transparent);
    background: color-mix(in srgb, var(--c) 12%, transparent);
    color: var(--c);
    font-size: var(--fs-xs);
    font-weight: 500;
    line-height: 1.6;
    white-space: nowrap;
  }
  .pill.plain {
    padding: 0;
    border: none;
    background: none;
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  .pill.ok {
    --c: var(--ok);
  }
  .pill.off {
    --c: var(--neutral);
  }
  .pill.busy {
    --c: var(--info);
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--c);
    flex: none;
  }
  .pill.busy .dot {
    animation: pulse 1.2s infinite;
  }
  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }
</style>
