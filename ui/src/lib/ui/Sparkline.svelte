<script lang="ts">
  let {
    values = [],
    width = 96,
    height = 28,
    tone = 'accent',
    max = 0,
    fill = true,
    label = 'Trend'
  }: {
    values?: number[];
    width?: number;
    height?: number;
    tone?: 'accent' | 'ok' | 'warn' | 'err' | 'info';
    max?: number;
    fill?: boolean;
    label?: string;
  } = $props();

  const geometry = $derived.by(() => {
    if (values.length < 2) return { line: '', area: '' };
    const top = Math.max(max, ...values, 1e-9);
    const pad = 2;
    const step = (width - pad * 2) / (values.length - 1);
    const pts = values.map((v, i) => [pad + i * step, height - pad - (Math.max(0, v) / top) * (height - pad * 2)] as const);
    const line = pts.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ');
    const first = pts[0];
    const last = pts[pts.length - 1];
    const area = first && last ? `${first[0].toFixed(1)},${height} ${line} ${last[0].toFixed(1)},${height}` : '';
    return { line, area };
  });
</script>

<svg class="spark {tone}" {width} {height} viewBox="0 0 {width} {height}" role="img" aria-label={label}>
  {#if geometry.line}
    {#if fill}<polygon points={geometry.area} class="area" />{/if}
    <polyline points={geometry.line} class="line" />
  {:else}
    <line x1="2" x2={width - 2} y1={height - 3} y2={height - 3} class="idle" />
  {/if}
</svg>

<style>
  .spark {
    display: block;
    flex: none;
    overflow: visible;
    --c: var(--accent);
  }
  .spark.ok {
    --c: var(--ok);
  }
  .spark.warn {
    --c: var(--warn);
  }
  .spark.err {
    --c: var(--err);
  }
  .spark.info {
    --c: var(--info);
  }
  .line {
    fill: none;
    stroke: var(--c);
    stroke-width: 1.5;
    stroke-linejoin: round;
    stroke-linecap: round;
  }
  .area {
    fill: color-mix(in srgb, var(--c) 14%, transparent);
    stroke: none;
  }
  .idle {
    stroke: var(--border-strong);
    stroke-width: 1.5;
    stroke-dasharray: 2 3;
  }
</style>
