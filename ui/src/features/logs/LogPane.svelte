<script lang="ts">
  import { onMount, tick, untrack, type Snippet } from 'svelte';
  import ArrowDown from '@lucide/svelte/icons/arrow-down';
  import { parseAnsi, stripAnsi, toSpans, type AnsiSpan, type Matcher } from '../../lib/ansi';
  import { processHue } from '../../lib/logproc';
  import type { LogSession } from '../../lib/state/logs.svelte';

  interface Row {
    seq: number;
    no: number;
    ts: number;
    time: string;
    stream: string;
    level: string;
    badge: string;
    proc: string;
    hue: number;
    spans: AnsiSpan[];
  }

  interface Props {
    session: LogSession;
    follow?: boolean;
    wrap?: boolean;
    fontSize?: number;
    compact?: boolean;
    tsFormat?: 'relative' | 'local' | 'utc' | 'off';
    ansi?: boolean;
    showTags?: boolean;
    showLevels?: boolean;
    showLineNumbers?: boolean;
    zebra?: boolean;
    matcher?: Matcher | null;
    labels?: Map<string, string>;
    onTag?: (proc: string) => void;
    empty?: Snippet;
    label?: string;
  }

  let {
    session,
    follow = $bindable(true),
    wrap = false,
    fontSize = 12,
    compact = false,
    tsFormat = 'local',
    ansi = true,
    showTags = true,
    showLevels = true,
    showLineNumbers = true,
    zebra = true,
    matcher = null,
    labels = new Map<string, string>(),
    onTag,
    empty,
    label = 'Process logs'
  }: Props = $props();

  const PAD = 12;
  const GAP = 10;
  const OVERSCAN = 14;
  const MAX_PLAIN = 4000;

  let el = $state<HTMLDivElement | null>(null);
  let probe = $state<HTMLSpanElement | null>(null);
  let viewH = $state(0);
  let viewW = $state(0);
  let charW = $state(7.2);
  let totalH = $state(0);
  let winTop = $state(0);
  let rows = $state.raw<Row[]>([]);
  let anchorOrd = $state(0);
  let nowTick = $state(Date.now());
  let hovering = false;
  let lastTop = 0;
  let intentUntil = 0;
  let lastKey = '';
  let cache = new Map<number, Row>();
  let cacheAnsi = true;
  let cacheMatcher: Matcher | null = null;
  let cacheFmt = '';
  let pf = new Float64Array(2048);
  let pfBase = 0;
  let pfEnd = 0;
  let pfKey = '';
  let layoutQueued = false;

  const rowH = $derived(Math.round(fontSize * 1.35));
  const noCh = $derived.by(() => {
    void session.version;
    return Math.max(3, String(Math.max(1, session.buffer.endSeq - session.numBase)).length);
  });
  const tsCh = $derived(tsFormat === 'off' ? 0 : tsFormat === 'relative' ? 7 : 12);
  const tagCh = 12;
  const lvCh = 5;
  const cells = $derived((showLineNumbers ? 1 : 0) + (tsFormat === 'off' ? 0 : 1) + (showTags ? 1 : 0) + (showLevels ? 1 : 0));
  const fixedPx = $derived(((showLineNumbers ? noCh : 0) + tsCh + (showTags ? tagCh : 0) + (showLevels ? lvCh : 0)) * charW + cells * GAP + PAD * 2);
  const newCount = $derived(follow ? 0 : Math.max(0, session.ordBase + session.viewCount - anchorOrd));
  const isEmpty = $derived(session.viewCount === 0);

  function pad(n: number, w = 2): string {
    return String(n).padStart(w, '0');
  }

  function clock(ts: number, utc: boolean): string {
    const d = new Date(ts);
    return utc
      ? `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}.${pad(d.getUTCMilliseconds(), 3)}`
      : `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
  }

  function rel(ts: number, now: number): string {
    const s = Math.max(0, Math.round((now - ts) / 1000));
    if (s < 1) return 'now';
    if (s < 60) return `${s}s ago`;
    if (s < 3600) return `${Math.floor(s / 60)}m ago`;
    if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
    return `${Math.floor(s / 86400)}d ago`;
  }

  function badgeOf(level: string): string {
    if (level === '') return '';
    switch (level.toLowerCase()) {
      case 'trace':
      case 'debug':
      case 'dbg':
        return 'debug';
      case 'warn':
      case 'warning':
        return 'warn';
      case 'error':
      case 'err':
        return 'error';
      case 'fatal':
      case 'panic':
      case 'critical':
      case 'crit':
        return 'fatal';
      default:
        return 'info';
    }
  }

  function buildRow(seq: number): Row {
    const l = session.buffer.atSeq(seq);
    const segs = ansi ? parseAnsi(l.text) : (() => {
      const t = stripAnsi(l.text).replace(/\r/g, '');
      return t === '' ? [] : [{ t, style: '', cls: '' }];
    })();
    return {
      seq,
      no: seq - session.numBase + 1,
      ts: l.ts,
      time: tsFormat === 'local' ? clock(l.ts, false) : tsFormat === 'utc' ? clock(l.ts, true) : '',
      stream: l.stream,
      level: l.level,
      badge: badgeOf(l.level),
      proc: l.proc,
      hue: l.proc ? processHue(l.proc) : 210,
      spans: toSpans(segs, matcher?.re ?? null)
    };
  }

  function measure() {
    if (!probe) return;
    const w = probe.getBoundingClientRect().width / 10;
    if (w > 2 && Math.abs(w - charW) > 0.01) charW = w;
  }

  function cols(): number {
    const msgW = viewW - fixedPx;
    return Math.max(8, Math.floor(msgW / charW));
  }

  function prefix(count: number, ordBase: number, key: string) {
    if (key !== pfKey || pfEnd < ordBase) {
      pfKey = key;
      pfBase = ordBase;
      pfEnd = ordBase;
      pf[0] = 0;
    }
    const endOrd = ordBase + count;
    if (pfEnd > endOrd) pfEnd = endOrd;
    if (ordBase - pfBase > 100000) {
      const d = ordBase - pfBase;
      pf.copyWithin(0, d, pfEnd - pfBase + 1);
      pfBase = ordBase;
    }
    const need = endOrd - pfBase + 2;
    if (need > pf.length) {
      const next = new Float64Array(Math.max(need, pf.length * 2));
      next.set(pf.subarray(0, pfEnd - pfBase + 1));
      pf = next;
    }
    const c = cols();
    for (let o = pfEnd; o < endOrd; o++) {
      const len = Math.min(session.plainAt(o - ordBase), MAX_PLAIN * 8);
      const lines = Math.max(1, Math.ceil(len / c));
      pf[o - pfBase + 1] = (pf[o - pfBase] as number) + lines * rowH;
    }
    pfEnd = endOrd;
  }

  function layout() {
    const e = el;
    if (!e) return;
    const count = session.viewCount;
    const ordBase = session.ordBase;
    const epoch = session.epoch;
    if (ansi !== cacheAnsi || matcher !== cacheMatcher || tsFormat !== cacheFmt) {
      cache = new Map();
      cacheAnsi = ansi;
      cacheMatcher = matcher;
      cacheFmt = tsFormat;
    }
    let top = (k: number) => k * rowH;
    let total = count * rowH;
    if (wrap) {
      prefix(count, ordBase, `${epoch}|${rowH}|${cols()}`);
      const b0 = pf[ordBase - pfBase] as number;
      top = (k) => (pf[ordBase + k - pfBase] as number) - b0;
      total = top(count);
    }
    const target = follow ? Math.max(0, total - viewH) : e.scrollTop;
    const find = (y: number): number => {
      if (count === 0) return 0;
      if (!wrap) return Math.min(count - 1, Math.max(0, Math.floor(y / rowH)));
      let lo = 0;
      let hi = count - 1;
      while (lo < hi) {
        const mid = (lo + hi + 1) >> 1;
        if (top(mid) <= y) lo = mid;
        else hi = mid - 1;
      }
      return lo;
    };
    const first = find(target);
    const last = find(target + viewH);
    const start = Math.max(0, first - OVERSCAN);
    const end = Math.min(count, last + 1 + OVERSCAN);
    const key = `${session.version}|${start}|${end}|${epoch}|${ordBase}|${ansi}|${tsFormat}|${wrap}|${rowH}|${viewW}`;
    const nextTop = top(start);
    if (key !== lastKey) {
      lastKey = key;
      const next = new Map<number, Row>();
      const out: Row[] = [];
      for (let k = start; k < end; k++) {
        const seq = session.rowSeq(k);
        const r = cache.get(seq) ?? buildRow(seq);
        next.set(seq, r);
        out.push(r);
      }
      cache = next;
      rows = out;
    }
    if (nextTop !== winTop) winTop = nextTop;
    if (total !== totalH) totalH = total;
    if (follow) void tick().then(stick);
  }

  function stick() {
    const e = el;
    if (!e || !follow) return;
    const t = Math.max(0, e.scrollHeight - e.clientHeight);
    if (Math.abs(e.scrollTop - t) > 0.5) {
      e.scrollTop = t;
      lastTop = t;
    }
  }

  function queueLayout() {
    if (layoutQueued) return;
    layoutQueued = true;
    requestAnimationFrame(() => {
      layoutQueued = false;
      layout();
    });
  }

  function setFollow(v: boolean) {
    if (v === follow) return;
    if (!v) anchorOrd = session.ordBase + session.viewCount;
    follow = v;
  }

  function markIntent() {
    intentUntil = performance.now() + 500;
  }

  function onScroll() {
    const e = el;
    if (!e) return;
    const up = e.scrollTop < lastTop - 0.5;
    lastTop = e.scrollTop;
    const dist = e.scrollHeight - e.scrollTop - e.clientHeight;
    if (dist < 6) setFollow(true);
    else if (up && follow && performance.now() < intentUntil) setFollow(false);
    queueLayout();
  }

  export function jumpLive() {
    setFollow(true);
    lastKey = '';
    layout();
    void tick().then(stick);
  }

  export function focus() {
    el?.focus({ preventScroll: true });
  }

  export function scrollToTop() {
    if (!el) return;
    markIntent();
    setFollow(false);
    el.scrollTop = 0;
  }

  function onKey(e: KeyboardEvent) {
    const t = e.target as HTMLElement | null;
    const typing = !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable);
    if (typing || e.metaKey || e.ctrlKey || e.altKey) return;
    const inside = hovering || (!!el && el.contains(document.activeElement));
    if (!inside) return;
    if (e.key === 'End') {
      e.preventDefault();
      jumpLive();
    } else if (e.key === 'Home') {
      e.preventDefault();
      scrollToTop();
    } else if (e.key === 'PageUp' || e.key === 'ArrowUp') {
      markIntent();
    }
  }

  onMount(() => {
    const e = el as HTMLDivElement;
    const ro = new ResizeObserver(() => {
      viewH = e.clientHeight;
      viewW = e.clientWidth;
    });
    ro.observe(e);
    viewH = e.clientHeight;
    viewW = e.clientWidth;
    const intent = () => markIntent();
    const enter = () => (hovering = true);
    const leave = () => (hovering = false);
    e.addEventListener('wheel', intent, { passive: true });
    e.addEventListener('touchstart', intent, { passive: true });
    e.addEventListener('pointerdown', intent, { passive: true });
    e.addEventListener('pointerenter', enter);
    e.addEventListener('pointerleave', leave);
    const onFonts = () => measure();
    void document.fonts?.ready.then(measure);
    document.fonts?.addEventListener?.('loadingdone', onFonts);
    measure();
    return () => {
      ro.disconnect();
      e.removeEventListener('wheel', intent);
      e.removeEventListener('touchstart', intent);
      e.removeEventListener('pointerdown', intent);
      e.removeEventListener('pointerenter', enter);
      e.removeEventListener('pointerleave', leave);
      document.fonts?.removeEventListener?.('loadingdone', onFonts);
    };
  });

  $effect(() => {
    void fontSize;
    void compact;
    void tick().then(measure);
  });

  $effect(() => {
    if (tsFormat !== 'relative') return;
    const t = setInterval(() => (nowTick = Date.now()), 1000);
    return () => clearInterval(t);
  });

  $effect(() => {
    void session.version;
    void wrap;
    void fontSize;
    void compact;
    void tsFormat;
    void ansi;
    void matcher;
    void showTags;
    void showLevels;
    void showLineNumbers;
    void viewW;
    void viewH;
    void charW;
    void follow;
    void noCh;
    untrack(layout);
  });

  const contentW = $derived(Math.ceil(fixedPx + Math.min(session.maxPlain, MAX_PLAIN) * charW + PAD));
  const style = $derived(`--fs:${fontSize}px;--rh:${rowH}px;--no:${noCh}ch;--ts:${tsCh}ch;--tag:${tagCh}ch;--lv:${lvCh}ch;--gap:${GAP}px;--pad:${PAD}px`);
</script>

<svelte:window onkeydown={onKey} />

<div class="pane" {style} class:wrap>
  <div
    class="scroller"
    bind:this={el}
    onscroll={onScroll}
    role="log"
    aria-label={label}
    aria-live="off"
    tabindex="-1"
  >
    <div class="sizer" style="height:{totalH}px;{wrap ? '' : `min-width:${contentW}px`}">
      <div class="win" style="transform:translateY({winTop}px)">
        {#each rows as r (r.seq)}
          <div class="row" class:err={r.stream === 'stderr'} class:sys={r.stream === 'system'} class:odd={zebra && (r.seq & 1) === 1}>
            {#if showLineNumbers}<span class="no">{r.no}</span>{/if}
            {#if tsFormat !== 'off'}<span class="ts" title={new Date(r.ts).toLocaleString()}>{tsFormat === 'relative' ? rel(r.ts, nowTick) : r.time}</span>{/if}
            {#if showTags}
              <span class="tagcell">
                {#if r.proc}
                  <button type="button" class="tag" style="--h:{r.hue}" title={labels.get(r.proc) ?? r.proc} aria-label="Show only {labels.get(r.proc) ?? r.proc}" onclick={() => onTag?.(r.proc)}><span>{labels.get(r.proc) ?? r.proc.slice(-6)}</span></button>
                {/if}
              </span>
            {/if}
            {#if showLevels}<span class="lv">{#if r.badge}<span class="lvb {r.badge}">{r.badge}</span>{/if}</span>{/if}
            <span class="msg">{#each r.spans as s}{#if s.hit}<mark class={s.cls} style={s.style}>{s.t}</mark>{:else if s.cls || s.style}<span class={s.cls} style={s.style}>{s.t}</span>{:else}{s.t}{/if}{/each}</span>
          </div>
        {/each}
      </div>
    </div>
  </div>
  <span class="probe" bind:this={probe} aria-hidden="true">0000000000</span>
  {#if isEmpty && empty}
    <div class="overlay">{@render empty()}</div>
  {/if}
  {#if !follow && !isEmpty}
    <button type="button" class="pill" onclick={jumpLive}>
      <ArrowDown size={14} />
      <span>Jump to live{newCount > 0 ? ` · ${newCount.toLocaleString()} new` : ''}</span>
      <span class="kbd">End</span>
    </button>
  {/if}
</div>

<style>
  .pane {
    --tl: 68%;
    position: relative;
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    background: var(--bg-1);
    font-family: var(--font-mono);
    font-size: var(--fs);
    font-variant-ligatures: none;
    font-feature-settings: 'calt' 0, 'liga' 0;
  }

  :global(:root[data-theme='light']) .pane {
    --tl: 36%;
    --ansi-0: #24292f;
    --ansi-1: #cf222e;
    --ansi-2: #1a7f37;
    --ansi-3: #9a6700;
    --ansi-4: #0969da;
    --ansi-5: #8250df;
    --ansi-6: #1b7c83;
    --ansi-7: #57606a;
    --ansi-8: #6e7781;
    --ansi-9: #a40e26;
    --ansi-10: #116329;
    --ansi-11: #7d4e00;
    --ansi-12: #0550ae;
    --ansi-13: #6639ba;
    --ansi-14: #136061;
    --ansi-15: #24292f;
  }

  .scroller {
    flex: 1;
    min-width: 0;
    overflow: auto;
    outline: none;
    overscroll-behavior: contain;
    scrollbar-gutter: stable;
  }

  .sizer {
    position: relative;
    min-width: 100%;
  }

  .win {
    position: absolute;
    left: 0;
    right: 0;
    top: 0;
    will-change: transform;
  }

  .row {
    display: flex;
    align-items: flex-start;
    gap: var(--gap);
    padding: 0 var(--pad);
    height: var(--rh);
    line-height: var(--rh);
    color: var(--text-0);
    white-space: pre;
  }

  .wrap .row {
    height: auto;
  }

  .row.odd {
    background: color-mix(in srgb, var(--text-0) 2.4%, transparent);
  }

  .row.err {
    background: color-mix(in srgb, var(--log-stderr) 7%, transparent);
    box-shadow: inset 2px 0 0 var(--log-stderr);
  }

  .row.err .msg {
    color: color-mix(in srgb, var(--text-0) 72%, var(--log-stderr));
  }

  .row.sys {
    color: var(--text-2);
    font-style: italic;
    background: color-mix(in srgb, var(--log-system) 6%, transparent);
    box-shadow: inset 2px 0 0 var(--log-system);
  }

  .row:hover {
    background: var(--bg-hover);
  }

  .no {
    flex: none;
    width: var(--no);
    text-align: right;
    color: var(--text-2);
    opacity: 0.75;
    user-select: none;
    font-variant-numeric: tabular-nums;
  }

  .ts {
    flex: none;
    width: var(--ts);
    color: var(--text-2);
    white-space: pre;
    overflow: hidden;
    text-align: right;
  }

  .tagcell {
    flex: none;
    width: var(--tag);
    display: flex;
    align-items: center;
    height: var(--rh);
    min-width: 0;
  }

  .tag {
    display: flex;
    align-items: center;
    max-width: 100%;
    padding: 0 6px;
    height: calc(var(--rh) - 4px);
    line-height: 1;
    box-sizing: border-box;
    border-radius: 4px;
    border: 1px solid hsl(var(--h) 70% var(--tl) / 0.32);
    background: hsl(var(--h) 70% var(--tl) / 0.12);
    color: hsl(var(--h) 72% var(--tl));
    font: inherit;
    font-size: 0.86em;
    font-weight: 600;
    overflow: hidden;
    white-space: nowrap;
    text-align: left;
    cursor: pointer;
  }

  .tag span {
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .tag:hover:not(:disabled) {
    background: hsl(var(--h) 70% var(--tl) / 0.22);
    border-color: hsl(var(--h) 70% var(--tl) / 0.6);
  }

  .lv {
    flex: none;
    width: var(--lv);
    display: flex;
    align-items: center;
    height: var(--rh);
    user-select: none;
  }

  .lvb {
    display: block;
    width: 100%;
    text-align: center;
    height: calc(var(--rh) - 7px);
    line-height: calc(var(--rh) - 7px);
    border-radius: 3px;
    font-size: 0.7em;
    font-weight: 700;
    letter-spacing: 0.04em;
    
    color: var(--text-1);
    background: color-mix(in srgb, var(--text-2) 16%, transparent);
  }

  .lvb.debug {
    color: var(--text-2);
    background: color-mix(in srgb, var(--log-debug) 12%, transparent);
  }

  .lvb.info {
    color: var(--info);
    background: color-mix(in srgb, var(--info) 14%, transparent);
  }

  .lvb.warn {
    color: var(--log-warn);
    background: color-mix(in srgb, var(--log-warn) 18%, transparent);
  }

  .lvb.error,
  .lvb.fatal {
    color: var(--err);
    background: color-mix(in srgb, var(--err) 18%, transparent);
  }

  .lvb.fatal {
    color: var(--accent-fg);
    background: var(--err);
  }

  .msg {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    tab-size: 4;
  }

  .wrap .msg {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    word-break: break-all;
    overflow: visible;
  }

  .msg :global(.ab) {
    font-weight: 700;
  }

  .msg :global(.ad) {
    opacity: 0.65;
  }

  .msg :global(.ai) {
    font-style: italic;
  }

  .msg :global(.au) {
    text-decoration: underline;
  }

  .msg :global(.as) {
    text-decoration: line-through;
  }

  mark {
    background: color-mix(in srgb, var(--warn) 42%, transparent);
    color: inherit;
    border-radius: 2px;
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--warn) 55%, transparent);
  }

  .probe {
    position: absolute;
    visibility: hidden;
    pointer-events: none;
    white-space: pre;
    left: 0;
    top: 0;
  }

  .overlay {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: var(--font-ui);
    pointer-events: none;
    overflow: auto;
  }

  .overlay :global(*) {
    pointer-events: auto;
  }

  .pill {
    position: absolute;
    left: 50%;
    bottom: var(--space-5);
    transform: translateX(-50%);
    height: 24px;
    padding: 0 10px 0 8px;
    border-radius: 12px;
    border: none;
    background: var(--accent);
    color: var(--accent-fg);
    font-family: var(--font-ui);
    font-size: var(--fs-xs);
    font-weight: 500;
    box-shadow: var(--shadow-pop);
    z-index: 3;
  }

  .pill:hover:not(:disabled) {
    background: var(--accent-hover);
  }

  .pill .kbd {
    background: color-mix(in srgb, var(--accent-fg) 20%, transparent);
    border-color: color-mix(in srgb, var(--accent-fg) 30%, transparent);
    color: var(--accent-fg);
    font-size: var(--fs-micro);
  }
</style>
