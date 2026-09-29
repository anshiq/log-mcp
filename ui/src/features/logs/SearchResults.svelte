<script lang="ts">
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import SearchX from '@lucide/svelte/icons/search-x';
  import History from '@lucide/svelte/icons/history';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import Copy from '@lucide/svelte/icons/copy';
  import Radio from '@lucide/svelte/icons/radio';
  import { parseAnsi, toSpans } from '../../lib/ansi';
  import { processHue } from '../../lib/logproc';
  import type { SearchHit } from '../../lib/logsearch';
  import { getPlatform } from '../../lib/platform';
  import { toasts } from '../../lib/toasts.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';

  interface Props {
    hits: SearchHit[];
    loading: boolean;
    error: string;
    searched: boolean;
    truncated: boolean;
    query: string;
    re: RegExp | null;
    labels: Map<string, string>;
    onRetry: () => void;
    onOpenLive: (proc: string) => void;
  }

  let { hits, loading, error, searched, truncated, query, re, labels, onRetry, onOpenLive }: Props = $props();

  const PAGE = 150;

  let collapsed = $state(new Set<string>());
  let expanded = $state<string | null>(null);
  let limits = $state<Record<string, number>>({});

  const groups = $derived.by(() => {
    const map = new Map<string, SearchHit[]>();
    for (const h of hits) {
      const list = map.get(h.proc);
      if (list) list.push(h);
      else map.set(h.proc, [h]);
    }
    return [...map].map(([proc, list]) => ({ proc, list }));
  });

  function toggleGroup(proc: string) {
    const next = new Set(collapsed);
    if (next.has(proc)) next.delete(proc);
    else next.add(proc);
    collapsed = next;
  }

  function stamp(ts: number): string {
    if (!ts) return '';
    const d = new Date(ts);
    const p = (n: number) => String(n).padStart(2, '0');
    return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
  }

  function spans(text: string) {
    return toSpans(parseAnsi(text), re);
  }

  async function copy(text: string) {
    try {
      await getPlatform().copyText(text);
      toasts.ok('Copied line');
    } catch {
      toasts.err('Copy failed');
    }
  }
</script>

<div class="results">
  {#if loading}
    <div class="state">
      <Spinner size={20} label="Searching" />
      <p>Searching stored logs for <b>{query}</b></p>
    </div>
  {:else if error}
    <div class="state">
      <div class="icon-wrap err"><CircleAlert size={20} /></div>
      <h3>Search failed</h3>
      <p>{error}</p>
      <button type="button" class="btn" onclick={onRetry}>Try again</button>
    </div>
  {:else if !searched}
    <div class="state">
      <div class="icon-wrap"><History size={20} /></div>
      <h3>Search log history</h3>
      <p>Look through retained output of every selected process, including ones that have exited. Type a query above and press Enter.</p>
    </div>
  {:else if hits.length === 0}
    <div class="state">
      <div class="icon-wrap"><SearchX size={20} /></div>
      <h3>No matches</h3>
      <p>Nothing in the retained logs matches <b>{query}</b>. Try a shorter query, a different level, or include both streams.</p>
    </div>
  {:else}
    <div class="summary">
      <span><b>{hits.length.toLocaleString()}</b> {hits.length === 1 ? 'match' : 'matches'} in {groups.length} {groups.length === 1 ? 'process' : 'processes'}</span>
      {#if truncated}<span class="badge warn">Scan truncated, narrow the query for a complete result</span>{/if}
    </div>
    <div class="scroll">
      {#each groups as g (g.proc)}
        {@const open = !collapsed.has(g.proc)}
        {@const limit = limits[g.proc] ?? PAGE}
        <section class="group">
          <div class="ghead">
            <button type="button" class="gtoggle" aria-expanded={open} onclick={() => toggleGroup(g.proc)}>
              <ChevronRight size={14} class={open ? 'rot' : ''} />
              <span class="tag" style="--h:{processHue(g.proc)}">{labels.get(g.proc) ?? g.proc.slice(-6)}</span>
              <span class="gcount">{g.list.length.toLocaleString()}</span>
            </button>
            <button type="button" class="btn sm ghost" onclick={() => onOpenLive(g.proc)}><Radio size={13} />Tail live</button>
          </div>
          {#if open}
            {#each g.list.slice(0, limit) as h (h.key)}
              {@const isOpen = expanded === h.key}
              <div class="hit" class:open={isOpen} class:err={h.stream === 'stderr'}>
                <button type="button" class="hrow" aria-expanded={isOpen} onclick={() => (expanded = isOpen ? null : h.key)}>
                  <span class="ts">{stamp(h.ts)}</span>
                  <span class="msg">{#each spans(h.text) as s}{#if s.hit}<mark class={s.cls} style={s.style}>{s.t}</mark>{:else if s.cls || s.style}<span class={s.cls} style={s.style}>{s.t}</span>{:else}{s.t}{/if}{/each}</span>
                </button>
                {#if isOpen}
                  <div class="ctx">
                    {#each h.before as line}<div class="cl">{line}</div>{/each}
                    <div class="cl focus">{h.text}</div>
                    {#each h.after as line}<div class="cl">{line}</div>{/each}
                    {#if h.before.length === 0 && h.after.length === 0}
                      <p class="muted nctx">No surrounding lines were retained for this match.</p>
                    {/if}
                    <div class="cact">
                      <button type="button" class="btn sm" onclick={() => void copy(h.text)}><Copy size={13} />Copy line</button>
                      <button type="button" class="btn sm" onclick={() => onOpenLive(h.proc)}><Radio size={13} />Tail {labels.get(h.proc) ?? 'process'}</button>
                    </div>
                  </div>
                {/if}
              </div>
            {/each}
            {#if g.list.length > limit}
              <button type="button" class="more" onclick={() => (limits[g.proc] = limit + PAGE)}>Show {Math.min(PAGE, g.list.length - limit)} more of {(g.list.length - limit).toLocaleString()} remaining</button>
            {/if}
          {/if}
        </section>
      {/each}
    </div>
  {/if}
</div>

<style>
  .results {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-1);
  }

  .state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-8);
    text-align: center;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }

  .state h3 {
    color: var(--text-0);
    font-size: var(--fs-lg);
  }

  .state p {
    max-width: 420px;
  }

  .state b {
    color: var(--text-0);
    font-family: var(--font-mono);
    font-weight: 500;
  }

  .icon-wrap {
    width: 44px;
    height: 44px;
    border-radius: 12px;
    display: grid;
    place-items: center;
    background: var(--bg-3);
    color: var(--text-1);
  }

  .icon-wrap.err {
    color: var(--err);
    background: color-mix(in srgb, var(--err) 12%, transparent);
  }

  .summary {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    flex-wrap: wrap;
    padding: var(--space-3) var(--space-5);
    border-bottom: 1px solid var(--border);
    color: var(--text-1);
    font-size: var(--fs-sm);
  }

  .summary b {
    color: var(--text-0);
    font-variant-numeric: tabular-nums;
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow: auto;
  }

  .group {
    border-bottom: 1px solid var(--border);
  }

  .ghead {
    position: sticky;
    top: 0;
    z-index: 2;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-4);
    background: var(--bg-2);
    border-bottom: 1px solid var(--border);
  }

  .gtoggle {
    border: none;
    background: transparent;
    padding: 4px 6px;
    gap: 8px;
  }

  .gtoggle:hover:not(:disabled) {
    background: var(--bg-hover);
  }

  .gtoggle :global(.rot) {
    transform: rotate(90deg);
  }

  .gtoggle :global(svg) {
    transition: transform var(--dur-fast) var(--ease);
    color: var(--text-2);
  }

  .gcount {
    color: var(--text-2);
    font-size: var(--fs-xs);
    font-variant-numeric: tabular-nums;
  }

  .tag {
    padding: 1px 8px;
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    font-weight: 600;
    color: hsl(var(--h) 72% 62%);
    background: hsl(var(--h) 70% 60% / 0.13);
    border: 1px solid hsl(var(--h) 70% 60% / 0.32);
  }

  :global(:root[data-theme='light']) .tag {
    color: hsl(var(--h) 72% 34%);
    background: hsl(var(--h) 70% 40% / 0.1);
    border-color: hsl(var(--h) 70% 40% / 0.3);
  }

  .hit {
    border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  }

  .hit.err {
    box-shadow: inset 2px 0 0 var(--log-stderr);
  }

  .hit.open {
    background: var(--bg-2);
  }

  .hrow {
    width: 100%;
    display: flex;
    align-items: flex-start;
    justify-content: flex-start;
    gap: var(--space-4);
    padding: 5px var(--space-5);
    border: none;
    border-radius: 0;
    background: transparent;
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
    font-weight: 400;
    text-align: left;
    white-space: normal;
    line-height: 1.55;
  }

  .hrow:hover:not(:disabled) {
    background: var(--bg-hover);
  }

  .ts {
    flex: none;
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
  }

  .msg {
    flex: 1;
    min-width: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    color: var(--text-0);
  }

  .hit.err .msg {
    color: color-mix(in srgb, var(--text-0) 72%, var(--log-stderr));
  }

  mark {
    background: color-mix(in srgb, var(--warn) 42%, transparent);
    color: inherit;
    border-radius: 2px;
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--warn) 55%, transparent);
  }

  .ctx {
    padding: var(--space-2) var(--space-5) var(--space-4);
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .cl {
    padding: 2px var(--space-4);
    color: var(--text-2);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    border-left: 2px solid var(--border);
  }

  .cl.focus {
    color: var(--text-0);
    border-left-color: var(--accent);
    background: var(--accent-subtle);
  }

  .nctx {
    font-family: var(--font-ui);
    padding: var(--space-2) var(--space-4);
    font-size: var(--fs-xs);
  }

  .cact {
    display: flex;
    gap: var(--space-3);
    padding-top: var(--space-3);
    font-family: var(--font-ui);
  }

  .more {
    width: 100%;
    border: none;
    border-radius: 0;
    background: transparent;
    color: var(--accent);
    padding: var(--space-3);
  }
</style>
