<script lang="ts">
  import { tick } from 'svelte';

  export interface LogLine {
    line: string;
    stream?: string;
    timestamp?: string;
    processId?: string;
  }

  let { lines = [] as LogLine[] }: { lines?: LogLine[] } = $props();

  const ROW_H = 20;
  let viewport: HTMLDivElement | null = $state(null);
  let scrollTop = $state(0);
  let viewH = $state(600);
  let userScrolled = $state(false);
  let newCount = $state(0);
  let prevLen = 0;

  $effect(() => {
    const grew = lines.length - prevLen;
    prevLen = lines.length;
    if (grew <= 0) return;
    if (userScrolled) {
      newCount += grew;
      return;
    }
    void tick().then(() => {
      if (viewport) viewport.scrollTop = viewport.scrollHeight;
    });
  });

  const start = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - 10));
  const end = $derived(Math.min(lines.length, Math.ceil((scrollTop + viewH) / ROW_H) + 10));
  const visible = $derived(lines.slice(start, end));

  function onScroll() {
    if (!viewport) return;
    scrollTop = viewport.scrollTop;
    const atBottom = viewport.scrollHeight - scrollTop - viewH < 40;
    userScrolled = !atBottom;
    if (atBottom) newCount = 0;
  }

  function jumpLive() {
    if (!viewport) return;
    userScrolled = false;
    newCount = 0;
    void tick().then(() => {
      if (viewport) viewport.scrollTop = viewport.scrollHeight;
    });
  }

  function streamClass(s?: string) {
    return s === 'stderr' ? 'stderr' : s === 'system' ? 'system' : 'stdout';
  }
</script>

<div class="logwrap">
  {#if userScrolled && newCount > 0}
    <button class="live-pill" onclick={jumpLive}>↓ Jump to live ({newCount} new)</button>
  {/if}
  {#if lines.length === 0}
    <div class="empty">No log lines yet.</div>
  {:else}
    <div class="viewport" bind:this={viewport} onscroll={onScroll} bind:clientHeight={viewH} role="log" aria-label="Process logs">
      <div class="spacer" style="height: {lines.length * ROW_H}px">
        {#each visible as l, i (start + i)}
          <div class="row {streamClass(l.stream)}" style="top: {(start + i) * ROW_H}px">
            <span class="gutter">{start + i + 1}</span>
            {#if l.timestamp}
              <span class="ts">{new Date(l.timestamp).toLocaleTimeString()}</span>
            {/if}
            <span class="text">{l.line}</span>
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .logwrap {
    position: relative;
    height: 100%;
  }
  .empty {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-2);
    background: var(--bg-1);
  }
  .viewport {
    overflow-y: auto;
    height: 100%;
    background: var(--bg-1);
    color: var(--text-0);
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
  }
  .spacer {
    position: relative;
  }
  .row {
    position: absolute;
    height: 20px;
    line-height: 20px;
    white-space: pre;
    display: flex;
    width: 100%;
    padding: 0 var(--space-4);
  }
  .row:hover {
    background: var(--bg-2);
  }
  .gutter {
    width: 48px;
    flex: none;
    text-align: right;
    padding-right: var(--space-4);
    color: var(--text-2);
    user-select: none;
  }
  .ts {
    flex: none;
    color: var(--text-2);
    padding-right: var(--space-4);
  }
  .text {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .stderr .text {
    color: var(--log-stderr);
  }
  .system .text {
    color: var(--log-system);
  }
  .live-pill {
    position: absolute;
    top: var(--space-4);
    right: var(--space-5);
    z-index: 2;
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: 999px;
    padding: var(--space-2) var(--space-4);
    box-shadow: var(--shadow-pop);
    font-size: var(--fs-sm);
  }
</style>
