<script lang="ts">
  let {
    values,
    max = 0,
    height = 56,
    color = 'var(--accent)',
    capacity = 60
  }: { values: number[]; max?: number; height?: number; color?: string; capacity?: number } = $props();

  const W = 100;
  const H = 40;

  const ceiling = $derived(Math.max(max, ...values, 1e-9) * (max > 0 ? 1 : 1.25));
  const points = $derived(
    values.map((v, i) => {
      const x = capacity > 1 ? W - (values.length - 1 - i) * (W / (capacity - 1)) : W;
      const y = H - 2 - (Math.min(v, ceiling) / ceiling) * (H - 4);
      return [x, y] as const;
    })
  );
  const line = $derived(points.map(([x, y]) => `${x.toFixed(2)},${y.toFixed(2)}`).join(' '));
  const area = $derived(
    points.length > 1
      ? `${points[0]![0].toFixed(2)},${H} ${line} ${points[points.length - 1]![0].toFixed(2)},${H}`
      : ''
  );
  const last = $derived(points[points.length - 1]);
</script>

<svg viewBox="0 0 {W} {H}" preserveAspectRatio="none" style="height:{height}px; --c:{color}" role="img" aria-label="History chart">
  <line x1="0" y1={H - 0.5} x2={W} y2={H - 0.5} class="base" />
  <line x1="0" y1={H / 2} x2={W} y2={H / 2} class="grid" />
  {#if points.length > 1}
    <polygon points={area} class="area" />
    <polyline points={line} class="line" />
  {/if}
</svg>
{#if last && points.length > 0}
  <span class="sr-only">{values[values.length - 1]}</span>
{/if}

<style>
  svg {
    display: block;
    width: 100%;
  }
  .base {
    stroke: var(--border);
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }
  .grid {
    stroke: var(--border);
    stroke-width: 1;
    stroke-dasharray: 3 4;
    opacity: 0.6;
    vector-effect: non-scaling-stroke;
  }
  .area {
    fill: var(--c);
    opacity: 0.16;
  }
  .line {
    fill: none;
    stroke: var(--c);
    stroke-width: 1.6;
    stroke-linejoin: round;
    stroke-linecap: round;
    vector-effect: non-scaling-stroke;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
  }
</style>
