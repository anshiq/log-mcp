<script lang="ts">
  import { untrack } from 'svelte';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import LogPane from '../features/logs/LogPane.svelte';
  import { LogSession } from '../lib/state/logs.svelte';
  import { prefs } from '../lib/state/prefs.svelte';
  import type { Matcher } from '../lib/ansi';

  export interface LogLine {
    line: string;
    stream?: string;
    timestamp?: string;
    processId?: string;
    level?: string;
  }

  interface Props {
    lines?: LogLine[];
    matcher?: Matcher | null;
    showTags?: boolean;
    emptyText?: string;
  }

  let { lines = [] as LogLine[], matcher = null, showTags = false, emptyText = 'No log lines yet.' }: Props = $props();

  const session = new LogSession({ max: prefs.data.maxBufferLines, schedule: (fn) => fn() });
  let follow = $state(true);
  let consumed = 0;
  let last: LogLine | null = null;
  let counter = 0;

  function toBuffered(l: LogLine) {
    const ts = l.timestamp ? Date.parse(l.timestamp) : NaN;
    return {
      id: counter++,
      ts: Number.isNaN(ts) ? Date.now() : ts,
      stream: l.stream || 'stdout',
      level: l.level ?? '',
      text: l.line ?? '',
      proc: l.processId ?? ''
    };
  }

  function sync(next: LogLine[]) {
    let from = 0;
    if (last !== null && consumed > 0) {
      if (next.length >= consumed && next[consumed - 1] === last) from = consumed;
      else {
        const at = next.lastIndexOf(last);
        if (at >= 0) from = at + 1;
      }
    }
    if (from === 0 && consumed > 0) {
      session.clear();
      counter = 0;
    }
    for (let i = from; i < next.length; i++) session.push(toBuffered(next[i] as LogLine));
    session.flushNow();
    consumed = next.length;
    last = next.length > 0 ? (next[next.length - 1] as LogLine) : null;
  }

  $effect(() => {
    const next = lines;
    untrack(() => sync(next));
  });

  $effect(() => {
    const m = matcher;
    untrack(() => session.setCriteria({ procs: null, streams: null, minRank: 0, matcher: m }));
  });
</script>

<div class="viewer">
  <LogPane
    {session}
    bind:follow
    wrap={prefs.data.wrap}
    fontSize={prefs.data.logFontSize}
    compact={prefs.data.density === 'compact'}
    tsFormat={prefs.data.timestamps}
    ansi={prefs.data.ansi}
    {showTags}
    showLevels={false}
    showLineNumbers
    {matcher}
    label="Process logs"
  >
    {#snippet empty()}
      <div class="none">
        <ScrollText size={20} />
        <span>{emptyText}</span>
      </div>
    {/snippet}
  </LogPane>
</div>

<style>
  .viewer {
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .none {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
</style>
