<script lang="ts">
  import { collapseContext, diffLines, diffStats, parseUnified, type DiffRow } from '../config/diff';

  let {
    diff = '',
    old = undefined,
    next = undefined,
    context = 3,
    lineNumbers = true,
    stats = true,
    emptyText = 'No changes'
  }: {
    diff?: string;
    old?: string;
    next?: string;
    context?: number;
    lineNumbers?: boolean;
    stats?: boolean;
    emptyText?: string;
  } = $props();

  const rows = $derived.by<DiffRow[]>(() => {
    const all = old !== undefined && next !== undefined ? diffLines(old, next) : parseUnified(diff);
    return context < 0 ? all : collapseContext(all, context);
  });
  const summary = $derived(diffStats(rows));
  const changed = $derived(summary.added + summary.removed > 0);
  const hasRows = $derived(rows.some((r) => r.kind !== 'meta' && r.kind !== 'hunk') && (old !== undefined || diff.trim() !== ''));

  function marker(kind: string): string {
    if (kind === 'add') return '+';
    if (kind === 'del') return '−';
    return '';
  }
</script>

<div class="diff" class:plain={!lineNumbers}>
  {#if stats && changed}
    <div class="stats">
      <span class="plus">+{summary.added}</span>
      <span class="minus">−{summary.removed}</span>
      <span class="bar" aria-hidden="true">
        {#each Array.from({ length: 5 }) as _, i}
          {@const total = summary.added + summary.removed}
          {@const addBlocks = Math.round((summary.added / total) * 5)}
          <i class={i < addBlocks ? 'a' : 'd'}></i>
        {/each}
      </span>
    </div>
  {/if}
  {#if !hasRows || !changed}
    <div class="empty">{emptyText}</div>
  {:else}
    <div class="scroll" role="table" aria-label="Diff">
      {#each rows as row, i (i)}
        {#if row.kind === 'hunk' || row.kind === 'meta'}
          <div class="row {row.kind}" role="row">
            <span class="text" role="cell">{row.text}</span>
          </div>
        {:else}
          <div class="row {row.kind}" role="row">
            {#if lineNumbers}
              <span class="no" role="cell">{row.oldNo ?? ''}</span>
              <span class="no" role="cell">{row.newNo ?? ''}</span>
            {/if}
            <span class="mark" role="cell">{marker(row.kind)}</span>
            <span class="text" role="cell">{row.text === '' ? ' ' : row.text}</span>
          </div>
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .diff {
    display: flex;
    flex-direction: column;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
    min-height: 0;
  }
  .stats {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 6px var(--space-4);
    background: var(--bg-2);
    border-bottom: 1px solid var(--border);
    font-size: var(--fs-xs);
    font-weight: 600;
  }
  .plus {
    color: var(--ok);
  }
  .minus {
    color: var(--err);
  }
  .bar {
    display: inline-flex;
    gap: 2px;
    margin-left: var(--space-2);
  }
  .bar i {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    background: var(--border-strong);
  }
  .bar i.a {
    background: var(--ok);
  }
  .bar i.d {
    background: var(--err);
  }
  .empty {
    padding: var(--space-5);
    color: var(--text-2);
    font-family: var(--font-ui);
    text-align: center;
  }
  .scroll {
    overflow: auto;
    min-height: 0;
    padding: 2px 0;
  }
  .row {
    display: flex;
    align-items: stretch;
    min-width: max-content;
    line-height: 18px;
    white-space: pre;
  }
  .no {
    flex: none;
    width: 42px;
    padding: 0 var(--space-3);
    text-align: right;
    color: var(--text-2);
    user-select: none;
    font-variant-numeric: tabular-nums;
    opacity: 0.8;
  }
  .mark {
    flex: none;
    width: 20px;
    text-align: center;
    user-select: none;
    font-weight: 700;
  }
  .plain .mark {
    width: 16px;
  }
  .text {
    flex: 1;
    padding-right: var(--space-4);
    color: var(--text-1);
  }
  .row.add {
    background: color-mix(in srgb, var(--ok) 13%, transparent);
  }
  .row.add .mark,
  .row.add .text {
    color: var(--ok);
  }
  .row.add .no {
    background: color-mix(in srgb, var(--ok) 10%, transparent);
  }
  .row.del {
    background: color-mix(in srgb, var(--err) 13%, transparent);
  }
  .row.del .mark,
  .row.del .text {
    color: var(--err);
  }
  .row.del .no {
    background: color-mix(in srgb, var(--err) 10%, transparent);
  }
  .row.ctx .text {
    color: var(--text-1);
  }
  .row.hunk {
    background: var(--accent-subtle);
    color: var(--accent);
    padding: 1px var(--space-4);
    font-size: var(--fs-xs);
  }
  .row.hunk .text {
    color: var(--accent);
    padding: 0;
  }
  .row.meta {
    color: var(--text-2);
    padding: 0 var(--space-4);
  }
  .row.meta .text {
    color: var(--text-2);
  }
</style>
