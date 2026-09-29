<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';
  import Pause from '@lucide/svelte/icons/pause';
  import Play from '@lucide/svelte/icons/play';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import ListFilter from '@lucide/svelte/icons/list-filter';
  import Check from '@lucide/svelte/icons/check';
  import Activity from '@lucide/svelte/icons/activity';
  import SquareTerminal from '@lucide/svelte/icons/square-terminal';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import ArrowDownToLine from '@lucide/svelte/icons/arrow-down-to-line';
  import type { DaemonEvent } from '../../lib/api';
  import { events } from '../../lib/state/events.svelte';
  import { processes } from '../../lib/state/processes.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { clock } from '../../lib/state/clock.svelte';
  import { router } from '../../lib/router.svelte';
  import {
    FAMILY_LABEL,
    KNOWN_TYPES,
    clock as clockTime,
    dayKey,
    dayLabel,
    eventKind,
    eventSentence,
    eventTone,
    eventWorkspace,
    exactTime,
    humanizeType,
    procLabel,
    scopedEvents
  } from '../../lib/activity';
  import type { Family } from '../../lib/activity';
  import ToneBadge from '../../features/activity/ToneBadge.svelte';
  import JsonView from '../../features/activity/JsonView.svelte';
  import RelativeTime from '../../lib/ui/RelativeTime.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';

  const STEP = 300;

  let query = $state('');
  let selectedTypes = $state(new Set<string>());
  let processFilter = $state('');
  let paused = $state(false);
  let frozen = $state<DaemonEvent[]>([]);
  let typeOpen = $state(false);
  let typeRoot: HTMLDivElement | null = $state(null);
  let expanded = $state(new Set<number>());
  let limit = $state(STEP);
  let freshIds = $state(new Set<number>());
  let known = new Set<number>();
  let primed = false;
  let autoLoads = 0;

  onMount(() => clock.retain());

  const source = $derived(paused ? frozen : events.items);
  const scoped = $derived(
    scopedEvents(source)
      .filter((e) => e.type !== 'process.stdout' && e.type !== 'process.stderr')
      .sort((a, b) => b.ts - a.ts || b.id - a.id)
  );

  const typeCounts = $derived.by(() => {
    const m = new Map<string, number>();
    for (const e of scoped) m.set(e.type, (m.get(e.type) ?? 0) + 1);
    return m;
  });

  const typeGroups = $derived.by(() => {
    const all = new Set<string>([...KNOWN_TYPES, ...typeCounts.keys()]);
    const groups = new Map<Family, string[]>();
    for (const t of all) {
      const f = eventKind(t).family;
      groups.set(f, [...(groups.get(f) ?? []), t]);
    }
    const order: Family[] = ['process', 'logs', 'config', 'other'];
    return order
      .filter((f) => groups.has(f))
      .map((f) => ({ family: f, label: FAMILY_LABEL[f], types: (groups.get(f) ?? []).sort() }));
  });

  const processOptions = $derived.by(() => {
    const ids = new Map<string, number>();
    for (const e of scoped) if (e.processId) ids.set(e.processId, (ids.get(e.processId) ?? 0) + 1);
    return [...ids.entries()]
      .map(([id, count]) => ({ id, count, label: procLabel(id) }))
      .sort((a, b) => a.label.localeCompare(b.label));
  });

  const needle = $derived(query.trim().toLowerCase());

  const filtered = $derived(
    scoped.filter((e) => {
      if (selectedTypes.size > 0 && !selectedTypes.has(e.type)) return false;
      if (processFilter && e.processId !== processFilter) return false;
      if (!needle) return true;
      const hay = `${eventSentence(e)} ${e.type} ${e.processId} ${e.instanceId} ${JSON.stringify(e.payload)}`.toLowerCase();
      return hay.includes(needle);
    })
  );

  const visible = $derived(filtered.slice(0, limit));
  const hidden = $derived(filtered.length - visible.length);
  const filtersActive = $derived(needle !== '' || selectedTypes.size > 0 || processFilter !== '');

  const groups = $derived.by(() => {
    const out: { key: string; label: string; items: DaemonEvent[] }[] = [];
    for (const e of visible) {
      const key = dayKey(e.ts);
      const last = out[out.length - 1];
      if (last && last.key === key) last.items.push(e);
      else out.push({ key, label: dayLabel(e.ts, clock.now), items: [e] });
    }
    return out;
  });

  const pendingCount = $derived(paused ? scopedEvents(events.items).length - scoped.length : 0);
  const showWorkspace = $derived(scopeState.all && scopeState.workspaces.length > 1);

  $effect(() => {
    void scopeState.workspaceId;
    limit = STEP;
    expanded = new Set();
  });

  $effect(() => {
    const ids = filtered.map((e) => e.id);
    if (!primed) {
      if (events.loaded) {
        ids.forEach((id) => known.add(id));
        primed = true;
      }
      return;
    }
    const added = ids.filter((id) => !known.has(id));
    if (added.length === 0) return;
    added.forEach((id) => known.add(id));
    untrack(() => {
      freshIds = new Set([...freshIds, ...added]);
    });
    setTimeout(() => {
      const next = new Set(freshIds);
      added.forEach((id) => next.delete(id));
      freshIds = next;
    }, 2600);
  });

  $effect(() => {
    if (!events.loaded || events.loadingOlder || !events.hasMore) return;
    if (filtered.length >= 30 || autoLoads >= 6) return;
    autoLoads++;
    void events.loadOlder();
  });

  function togglePause() {
    if (!paused) frozen = [...events.items];
    paused = !paused;
  }

  function toggleType(t: string) {
    const next = new Set(selectedTypes);
    if (next.has(t)) next.delete(t);
    else next.add(t);
    selectedTypes = next;
  }

  function toggleFamily(types: string[]) {
    const next = new Set(selectedTypes);
    const all = types.every((t) => next.has(t));
    for (const t of types) {
      if (all) next.delete(t);
      else next.add(t);
    }
    selectedTypes = next;
  }

  function toggleRow(id: number) {
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expanded = next;
  }

  function clearFilters() {
    query = '';
    selectedTypes = new Set();
    processFilter = '';
  }

  function onWindowClick(e: MouseEvent) {
    if (typeOpen && typeRoot && !typeRoot.contains(e.target as Node)) typeOpen = false;
  }

  function onWindowKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && typeOpen) typeOpen = false;
  }

  function typeSummary(): string {
    if (selectedTypes.size === 0) return 'All types';
    if (selectedTypes.size === 1) return humanizeType([...selectedTypes][0] ?? '');
    return `${selectedTypes.size} types`;
  }

  function loadMore() {
    if (hidden > 0) limit += STEP;
    else void events.loadOlder();
  }

  function payloadEmpty(e: DaemonEvent): boolean {
    return Object.keys(e.payload).length === 0;
  }

  function processExists(id: string): boolean {
    return processes.map.has(id);
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={onWindowKey} />

<div class="page fill">
  <header class="page-header">
    <div class="titles">
      <h1>Events</h1>
      <p class="subtitle">Everything that happened to your processes in {scopeState.all ? 'all workspaces' : scopeState.label}.</p>
    </div>
    <div class="actions">
      <span class="live" class:off={paused}>
        <span class="dot" class:ok={!paused}></span>
        {paused ? 'Paused' : 'Live'}
      </span>
      <button class="btn" onclick={togglePause} aria-pressed={paused} aria-label={paused ? 'Resume live updates' : 'Pause live updates'}>
        {#if paused}<Play size={14} />Resume{:else}<Pause size={14} />Pause{/if}
      </button>
    </div>
  </header>

  <div class="toolbar">
    <div class="input-wrap search">
      <Search size={14} />
      <input type="search" placeholder="Search events, processes, payloads" aria-label="Search events" bind:value={query} />
    </div>

    <div class="typepick" bind:this={typeRoot}>
      <button class="btn" class:on={selectedTypes.size > 0} onclick={() => (typeOpen = !typeOpen)} aria-haspopup="true" aria-expanded={typeOpen} aria-label="Event type filter">
        <ListFilter size={14} />{typeSummary()}<ChevronDown size={13} />
      </button>
      {#if typeOpen}
        <div class="pop" role="group" aria-label="Event types">
          {#each typeGroups as g (g.family)}
            <div class="grp">
              <button class="grp-head" onclick={() => toggleFamily(g.types)}>
                <span>{g.label}</span>
                <span class="link">{g.types.every((t) => selectedTypes.has(t)) ? 'Clear' : 'Select all'}</span>
              </button>
              {#each g.types as t (t)}
                {@const k = eventKind(t)}
                <button class="opt" role="checkbox" aria-checked={selectedTypes.has(t)} onclick={() => toggleType(t)}>
                  <span class="cb" class:checked={selectedTypes.has(t)}>{#if selectedTypes.has(t)}<Check size={11} />{/if}</span>
                  <span class="tdot {k.tone}"></span>
                  <span class="tname">{k.label}</span>
                  <span class="tid mono">{t}</span>
                  <span class="cnt">{typeCounts.get(t) ?? 0}</span>
                </button>
              {/each}
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <select class="procsel" bind:value={processFilter} aria-label="Process filter">
      <option value="">All processes</option>
      {#each processOptions as o (o.id)}
        <option value={o.id}>{o.label} · {o.id.slice(-6)} ({o.count})</option>
      {/each}
    </select>

    <span class="grow"></span>
    <span class="count" aria-live="polite">
      {filtered.length}{filtersActive ? ` of ${scoped.length}` : ''} {filtered.length === 1 ? 'event' : 'events'}
      {#if paused && pendingCount > 0}<span class="badge info">+{pendingCount} new</span>{/if}
    </span>
    {#if filtersActive}
      <button class="btn ghost sm" onclick={clearFilters}><X size={13} />Clear filters</button>
    {/if}
  </div>

  <section class="card feed" aria-label="Event timeline">
    {#if !events.loaded && events.items.length === 0}
      <div class="skeletons" aria-busy="true">
        {#each Array(7) as _, i (i)}
          <div class="sk-row">
            <Skeleton width="28px" height="28px" />
            <div class="sk-col"><Skeleton width={`${40 + ((i * 13) % 35)}%`} height="12px" /><Skeleton width="22%" height="10px" /></div>
          </div>
        {/each}
      </div>
    {:else if filtered.length === 0}
      <div class="empty-state">
        <span class="icon-wrap"><Activity size={22} /></span>
        {#if filtersActive}
          <h3>No matching events</h3>
          <p>Nothing matches the current filters. Try a different search or clear them.</p>
          <button class="btn" onclick={clearFilters}><X size={14} />Clear filters</button>
        {:else}
          <h3>No events yet</h3>
          <p>Starts, stops, crashes, health changes and log alerts show up here the moment they happen.</p>
          <a class="btn primary" href="#/processes" use:router.link={'/processes'}><SquareTerminal size={14} />Go to Processes</a>
        {/if}
      </div>
    {:else}
      <div class="scroller">
        {#each groups as g (g.key)}
          <div class="day">{g.label}<span class="day-n">{g.items.length}</span></div>
          <ul class="timeline">
            {#each g.items as e (e.id)}
              {@const k = eventKind(e.type)}
              {@const open = expanded.has(e.id)}
              {@const ws = eventWorkspace(e)}
              <li class:fresh={freshIds.has(e.id)} class:open>
                <button class="row" onclick={() => toggleRow(e.id)} aria-expanded={open}>
                  <span class="chev">{#if open}<ChevronDown size={14} />{:else}<ChevronRight size={14} />{/if}</span>
                  <span class="time mono" title={exactTime(e.ts)}>{clockTime(e.ts)}</span>
                  <ToneBadge icon={k.icon} tone={eventTone(e)} size={26} />
                  <span class="text">
                    <span class="sentence">{eventSentence(e)}</span>
                    <span class="meta">
                      <span class="etype mono">{e.type}</span>
                      {#if showWorkspace && ws}<span class="ws">{scopeState.workspaceLabel(ws)}</span>{/if}
                    </span>
                  </span>
                  <span class="ago"><RelativeTime ts={e.ts} /></span>
                </button>
                {#if open}
                  <div class="detail">
                    <div class="facts">
                      <div><span class="fk">Time</span><span class="fv">{exactTime(e.ts)}</span></div>
                      {#if e.processId}<div><span class="fk">Process</span><span class="fv mono">{e.processId}</span></div>{/if}
                      {#if e.instanceId}<div><span class="fk">Instance</span><span class="fv mono">{e.instanceId}</span></div>{/if}
                      {#if ws}<div><span class="fk">Workspace</span><span class="fv">{scopeState.workspaceLabel(ws)}</span></div>{/if}
                      {#if e.sessionId}<div><span class="fk">Session</span><span class="fv mono">{e.sessionId}</span></div>{/if}
                      <div><span class="fk">Event id</span><span class="fv mono">{e.id}</span></div>
                    </div>
                    {#if e.processId}
                      <div class="links">
                        {#if processExists(e.processId)}
                          <a class="btn sm" href={`#/processes/${e.processId}`} use:router.link={`/processes/${e.processId}`}>Open process<ArrowRight size={13} /></a>
                        {/if}
                        <a class="btn sm" href={`#/logs?p=${e.processId}`} use:router.link={`/logs?p=${encodeURIComponent(e.processId)}`}><ScrollText size={13} />View logs</a>
                      </div>
                    {/if}
                    {#if payloadEmpty(e)}
                      <p class="nopayload">This event carries no payload.</p>
                    {:else}
                      <JsonView value={e.payload} />
                    {/if}
                  </div>
                {/if}
              </li>
            {/each}
          </ul>
        {/each}

        {#if hidden > 0 || events.hasMore}
          <div class="loadmore">
            <button class="btn" onclick={loadMore} disabled={events.loadingOlder}>
              {#if events.loadingOlder}<Spinner size={14} />Loading{:else}<ArrowDownToLine size={14} />{hidden > 0 ? `Show ${Math.min(STEP, hidden)} more` : 'Load older events'}{/if}
            </button>
            {#if hidden > 0}<span class="muted">{hidden} more loaded</span>{/if}
          </div>
        {/if}
      </div>
    {/if}
  </section>
</div>

<style>
  .page {
    gap: var(--space-4);
  }
  .live {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: var(--fs-sm);
    color: var(--ok);
    font-weight: 500;
  }
  .live.off {
    color: var(--text-2);
  }
  .toolbar :global(.btn),
  .toolbar select,
  .search input {
    height: 32px;
    box-sizing: border-box;
  }
  .search {
    width: 320px;
    max-width: 100%;
  }
  .search input {
    width: 100%;
  }
  .typepick {
    position: relative;
  }
  .btn.on {
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
    background: var(--accent-subtle);
  }
  .pop {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    z-index: 30;
    width: 360px;
    max-height: 420px;
    overflow: auto;
    padding: var(--space-2);
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
  }
  .grp + .grp {
    margin-top: var(--space-2);
    padding-top: var(--space-2);
    border-top: 1px solid var(--border);
  }
  .grp-head {
    display: flex;
    justify-content: space-between;
    width: 100%;
    border: none;
    background: transparent;
    padding: 4px 8px;
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-2);
  }
  .grp-head:hover {
    background: transparent;
  }
  .grp-head .link {
    text-transform: none;
    letter-spacing: 0;
    font-weight: 500;
    color: var(--accent);
  }
  .opt {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    border: none;
    background: transparent;
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    text-align: left;
    color: var(--text-0);
  }
  .opt:hover {
    background: var(--bg-hover);
  }
  .cb {
    display: grid;
    place-items: center;
    width: 15px;
    height: 15px;
    border: 1px solid var(--border-strong);
    border-radius: 4px;
    flex: none;
    color: var(--accent-fg);
  }
  .cb.checked {
    background: var(--accent);
    border-color: var(--accent);
  }
  .tdot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--neutral);
    flex: none;
  }
  .tdot.ok {
    background: var(--ok);
  }
  .tdot.err {
    background: var(--err);
  }
  .tdot.warn {
    background: var(--warn);
  }
  .tdot.info {
    background: var(--info);
  }
  .tname {
    font-size: var(--fs-sm);
  }
  .tid {
    flex: 1;
    font-size: 10.5px;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .cnt {
    font-size: var(--fs-xs);
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
  }
  .procsel {
    max-width: 240px;
    padding-top: 0;
    padding-bottom: 0;
  }
  .count {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: var(--fs-sm);
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
  }
  .feed {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .scroller {
    overflow: auto;
    flex: 1;
    min-height: 0;
  }
  .day {
    position: sticky;
    top: 0;
    z-index: 2;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px var(--space-5);
    background: var(--bg-2);
    border-bottom: 1px solid var(--border);
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-1);
  }
  .day-n {
    font-weight: 500;
    color: var(--text-2);
    letter-spacing: 0;
  }
  .timeline {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .timeline > li {
    border-bottom: 1px solid var(--border);
  }
  .timeline > li.open {
    background: var(--bg-hover);
  }
  .timeline > li.fresh {
    animation: ev-in 0.5s ease-out, ev-glow 2.6s ease-out;
  }
  @keyframes ev-in {
    from {
      opacity: 0;
      transform: translateY(-8px);
    }
  }
  @keyframes ev-glow {
    0% {
      background: color-mix(in srgb, var(--accent) 16%, transparent);
    }
    100% {
      background: transparent;
    }
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    width: 100%;
    border: none;
    background: transparent;
    border-radius: 0;
    padding: 10px var(--space-5);
    text-align: left;
    color: var(--text-0);
  }
  .row:hover {
    background: var(--bg-hover);
  }
  .chev {
    display: inline-flex;
    color: var(--text-2);
    width: 14px;
    flex: none;
  }
  .time {
    width: 88px;
    flex: none;
    font-size: var(--fs-xs);
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
  }
  .text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .sentence {
    font-size: var(--fs-sm);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .etype {
    font-size: 10.5px;
    color: var(--text-2);
  }
  .ws {
    font-size: 10.5px;
    color: var(--text-1);
    padding: 0 6px;
    border: 1px solid var(--border);
    border-radius: 999px;
    white-space: nowrap;
  }
  .ago {
    font-size: var(--fs-xs);
    color: var(--text-2);
    flex: none;
  }
  .detail {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: 4px var(--space-5) var(--space-5) calc(var(--space-5) + 14px + var(--space-4));
  }
  .facts {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: var(--space-2) var(--space-5);
  }
  .facts > div {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .fk {
    font-size: 10.5px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-2);
    font-weight: 600;
  }
  .fv {
    font-size: var(--fs-xs);
    color: var(--text-0);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .links {
    display: flex;
    gap: var(--space-2);
  }
  .links .btn {
    text-decoration: none;
  }
  .nopayload {
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .loadmore {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-4);
    padding: var(--space-5);
  }
  .skeletons {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    padding: var(--space-5);
  }
  .sk-row {
    display: flex;
    gap: var(--space-4);
    align-items: center;
  }
  .sk-col {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  @media (max-width: 900px) {
    .time {
      display: none;
    }
    .search {
      width: 100%;
    }
  }
</style>
