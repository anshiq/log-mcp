<script lang="ts">
  let { status }: { status?: string } = $props();

  function toneOf(s?: string): 'ok' | 'off' | 'bad' | 'busy' {
    if (s === 'running' || s === 'ready') return 'ok';
    if (s === 'exited' || s === 'stopped') return 'off';
    if (s === 'starting' || s === 'stopping') return 'busy';
    return 'bad';
  }

  const tone = $derived(toneOf(status));
</script>

<span class="pill {tone}">
  <span class="dot"></span>
  {status ?? 'unknown'}
</span>

<style>
  .pill {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-sm);
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }
  .pill.ok .dot {
    background: var(--ok);
  }
  .pill.off .dot {
    background: var(--neutral);
  }
  .pill.bad .dot {
    background: var(--err);
  }
  .pill.busy .dot {
    background: var(--warn);
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
