<!-- Virtualized log viewer over a windowed buffer. Holds 1M+ lines at
  60fps: fixed row height, absolute positioning, render window only.
  Auto-scroll pauses on user scroll with a "Jump to live" pill. -->
<script lang="ts">
  export interface LogLine {
    line: string;
    stream?: string;
    timestamp?: string;
    processId?: string;
  }

  let { lines = [] as LogLine[], follow = true }: { lines?: LogLine[]; follow?: boolean } = $props();

  const ROW_H = 20;
  let viewport: HTMLDivElement | null = $state(null);
  let scrollTop = $state(0);
  let viewH = $state(600);
  let userScrolled = $state(false);
  let newCount = $state(0);
  let prevLen = 0;

  $effect(() => {
    if (lines.length > prevLen) {
      if (!userScrolled && viewport) viewport.scrollTop = viewport.scrollHeight;
      else newCount += lines.length - prevLen;
    }
    prevLen = lines.length;
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
    viewport.scrollTop = viewport.scrollHeight;
    userScrolled = false;
    newCount = 0;
  }

  function streamClass(s?: string) {
    return s === 'stderr' ? 'stderr' : s === 'system' ? 'system' : 'stdout';
  }
</script>

<div class="logwrap">
  {#if userScrolled && newCount > 0}
    <button class="live-pill" onclick={jumpLive}>Jump to live ({newCount} new)</button>
  {/if}
  <div
    class="viewport"
    bind:this={viewport}
    onscroll={onScroll}
    bind:clientHeight={viewH}
    role="log"
    aria-label="Process logs"
  >
    <div class="spacer" style="height: {lines.length * ROW_H}px">
      {#each visible as l, i (start + i)}
        <div class="row {streamClass(l.stream)}" style="top: {(start + i) * ROW_H}px">
          <span class="gutter">{start + i + 1}</span>
          <span class="text">{l.line}</span>
        </div>
      {/each}
    </div>
  </div>
</div>

<style>
  .logwrap { position: relative; height: 100%; }
  .viewport { overflow-y: auto; height: 100%; background: #0d1117; color: #c9d1d9; font-family: monospace; font-size: 12px; }
  .spacer { position: relative; }
  .row { position: absolute; height: 20px; line-height: 20px; white-space: pre; display: flex; width: 100%; }
  .gutter { width: 64px; flex: none; text-align: right; padding-right: 12px; color: #6e7681; user-select: none; }
  .stderr .text { color: #f47067; }
  .system .text { color: #6cb6ff; }
  .live-pill { position: absolute; top: 8px; right: 16px; z-index: 2; }
</style>
