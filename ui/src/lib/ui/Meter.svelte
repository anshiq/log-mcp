<script lang="ts">
  let {
    value = 0,
    max = 100,
    tone = 'auto',
    label = 'Usage'
  }: { value?: number; max?: number; tone?: 'auto' | 'ok' | 'warn' | 'err' | 'info' | 'accent'; label?: string } = $props();

  const pct = $derived(max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0);
  const resolved = $derived(tone === 'auto' ? (pct >= 90 ? 'err' : pct >= 70 ? 'warn' : 'ok') : tone);
</script>

<div class="meter {resolved}" role="meter" aria-label={label} aria-valuenow={value} aria-valuemin={0} aria-valuemax={max}>
  <div class="fill" style:width="{pct}%"></div>
</div>

<style>
  .meter {
    height: 4px;
    background: var(--bg-3);
    border-radius: 999px;
    overflow: hidden;
    --c: var(--ok);
  }
  .meter.warn {
    --c: var(--warn);
  }
  .meter.err {
    --c: var(--err);
  }
  .meter.info {
    --c: var(--info);
  }
  .meter.accent {
    --c: var(--accent);
  }
  .fill {
    height: 100%;
    background: var(--c);
    border-radius: inherit;
    transition: width 0.3s ease;
  }
</style>
