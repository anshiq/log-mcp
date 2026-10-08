<script lang="ts">
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';
  import Regex from '@lucide/svelte/icons/regex';
  import CaseSensitive from '@lucide/svelte/icons/case-sensitive';
  import WrapText from '@lucide/svelte/icons/wrap-text';
  import SlidersHorizontal from '@lucide/svelte/icons/sliders-horizontal';
  import Pause from '@lucide/svelte/icons/pause';
  import Play from '@lucide/svelte/icons/play';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Download from '@lucide/svelte/icons/download';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import Minus from '@lucide/svelte/icons/minus';
  import Plus from '@lucide/svelte/icons/plus';
  import FileDown from '@lucide/svelte/icons/file-down';
  import ClipboardCopy from '@lucide/svelte/icons/clipboard-copy';
  import Radio from '@lucide/svelte/icons/radio';
  import History from '@lucide/svelte/icons/history';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import Switch from '../../lib/ui/Switch.svelte';
  import { prefs } from '../../lib/state/prefs.svelte';
  import type { LogsUi, LevelFilter } from '../../lib/state/logsui.svelte';
  import type { LogSession } from '../../lib/state/logs.svelte';
  import type { Process } from '../../lib/api/types';
  import ProcessPicker from './ProcessPicker.svelte';
  import LogPopover from './LogPopover.svelte';

  interface Props {
    ui: LogsUi;
    session: LogSession;
    list: Process[];
    labels: Map<string, string>;
    runningCount: number;
    matcherError: string;
    buffered: number;
    searching: boolean;
    exportTargets: Process[];
    onSearch: () => void;
    onClear: () => void;
    onExportView: () => void;
    onCopyView: () => void;
    onExportProcess: (id: string) => void;
    onToggleProcess: (id: string) => void;
    onSelectRunning: () => void;
    onClearSelection: () => void;
  }

  let {
    ui,
    session,
    list,
    labels,
    runningCount,
    matcherError,
    buffered,
    searching,
    exportTargets,
    onSearch,
    onClear,
    onExportView,
    onCopyView,
    onExportProcess,
    onToggleProcess,
    onSelectRunning,
    onClearSelection
  }: Props = $props();

  let input = $state<HTMLInputElement | null>(null);
  let viewOpen = $state(false);
  let exportOpen = $state(false);

  const levels: { id: LevelFilter; label: string }[] = [
    { id: 'all', label: 'All' },
    { id: 'debug', label: 'Debug' },
    { id: 'info', label: 'Info' },
    { id: 'warn', label: 'Warn' },
    { id: 'error', label: 'Error' }
  ];

  const stamps: { id: 'relative' | 'local' | 'utc' | 'off'; label: string }[] = [
    { id: 'relative', label: 'Relative' },
    { id: 'local', label: 'Local' },
    { id: 'utc', label: 'UTC' },
    { id: 'off', label: 'Off' }
  ];

  export function focusFilter() {
    input?.focus();
    input?.select();
  }

  function onInputKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      if (ui.query) ui.query = '';
      else input?.blur();
    } else if (e.key === 'Enter' && ui.mode === 'history') {
      e.preventDefault();
      onSearch();
    }
  }

  function bump(delta: number) {
    prefs.set('logFontSize', Math.min(20, Math.max(10, prefs.data.logFontSize + delta)));
  }

  function setView<K extends keyof typeof ui.view>(k: K, v: (typeof ui.view)[K]) {
    ui.view[k] = v;
    ui.saveView();
  }
</script>

<div class="toolbar-wrap">
  <div class="row">
    <ProcessPicker
      {list}
      selected={ui.selected}
      {labels}
      {runningCount}
      onToggle={onToggleProcess}
      onClear={onClearSelection}
      {onSelectRunning}
    />

    <div class="filter" class:bad={!!matcherError}>
      <Search size={14} />
      <input
        bind:this={input}
        bind:value={ui.query}
        onkeydown={onInputKey}
        placeholder={ui.mode === 'live' ? 'Filter lines' : 'Search stored logs'}
        aria-label="Filter logs"
        spellcheck="false"
        autocomplete="off"
      />
      {#if ui.query}
        <button type="button" class="tog" aria-label="Clear filter" title="Clear filter" onclick={() => { ui.query = ''; input?.focus(); }}><X size={14} /></button>
      {:else}
        <span class="kbd hint">Ctrl F</span>
      {/if}
      <span class="sepv"></span>
      <button type="button" class="tog" class:on={ui.caseSensitive} aria-pressed={ui.caseSensitive} aria-label="Match case" title="Match case" onclick={() => (ui.caseSensitive = !ui.caseSensitive)}><CaseSensitive size={16} /></button>
      <button type="button" class="tog" class:on={ui.regex} aria-pressed={ui.regex} aria-label="Use regular expression" title="Regular expression" onclick={() => (ui.regex = !ui.regex)}><Regex size={16} /></button>
    </div>

    <div class="seg" role="group" aria-label="Mode">
      <button type="button" class:active={ui.mode === 'live'} aria-pressed={ui.mode === 'live'} onclick={() => (ui.mode = 'live')}><Radio size={14} />Live</button>
      <button type="button" class:active={ui.mode === 'history'} aria-pressed={ui.mode === 'history'} onclick={() => (ui.mode = 'history')}><History size={14} />History</button>
    </div>

    {#if ui.mode === 'history'}
      <button type="button" class="btn primary" disabled={searching || !ui.query.trim()} onclick={onSearch}>
        {#if searching}<Spinner size={14} />{:else}<Search size={14} />{/if}
        Search
      </button>
    {/if}
  </div>

  {#if matcherError}
    <p class="errline" role="alert">Invalid regular expression: {matcherError}</p>
  {/if}

  <div class="row">
    <div class="seg" role="group" aria-label="Minimum level">
      {#each levels as l (l.id)}
        <button type="button" class:active={ui.level === l.id} aria-pressed={ui.level === l.id} onclick={() => (ui.level = l.id)}>{l.label}</button>
      {/each}
    </div>

    <div class="chips" role="group" aria-label="Streams">
      <button type="button" class="chip" class:active={ui.stdout} aria-pressed={ui.stdout} onclick={() => (ui.stdout = !ui.stdout)}>stdout</button>
      <button type="button" class="chip err" class:active={ui.stderr} aria-pressed={ui.stderr} onclick={() => (ui.stderr = !ui.stderr)}>stderr</button>
    </div>

    <span class="grow"></span>

    <button type="button" class="btn icon" class:on={prefs.data.wrap} aria-pressed={prefs.data.wrap} aria-label="Wrap lines" title="Wrap long lines" onclick={() => prefs.set('wrap', !prefs.data.wrap)}><WrapText size={16} /></button>

    <LogPopover bind:open={viewOpen} align="end" width={300} label="View options">
      {#snippet trigger({ toggle, attrs })}
        <button type="button" class="btn" onclick={toggle} {...attrs}><SlidersHorizontal size={14} />View</button>
      {/snippet}
      {#snippet children()}
        <div class="menu">
          <div class="mrow col">
            <span class="mlabel">Timestamps</span>
            <div class="seg full" role="group" aria-label="Timestamp format">
              {#each stamps as s (s.id)}
                <button type="button" class:active={prefs.data.timestamps === s.id} aria-pressed={prefs.data.timestamps === s.id} onclick={() => prefs.set('timestamps', s.id)}>{s.label}</button>
              {/each}
            </div>
          </div>
          <div class="mrow">
            <span class="mlabel">Font size</span>
            <div class="stepper">
              <button type="button" class="btn icon sm" aria-label="Smaller text" onclick={() => bump(-1)}><Minus size={14} /></button>
              <span class="num">{prefs.data.logFontSize}px</span>
              <button type="button" class="btn icon sm" aria-label="Larger text" onclick={() => bump(1)}><Plus size={14} /></button>
            </div>
          </div>
          <div class="switches">
            <Switch checked={prefs.data.wrap} onchange={(e) => prefs.set('wrap', (e.currentTarget as HTMLInputElement).checked)}>Wrap long lines</Switch>
            <Switch checked={prefs.data.ansi} onchange={(e) => prefs.set('ansi', (e.currentTarget as HTMLInputElement).checked)}>ANSI colors</Switch>
            <Switch checked={ui.view.lineNumbers} onchange={(e) => setView('lineNumbers', (e.currentTarget as HTMLInputElement).checked)}>Line numbers</Switch>
            <Switch checked={ui.view.tags} onchange={(e) => setView('tags', (e.currentTarget as HTMLInputElement).checked)}>Process tags</Switch>
            <Switch checked={ui.view.levels} onchange={(e) => setView('levels', (e.currentTarget as HTMLInputElement).checked)}>Level badges</Switch>
            <Switch checked={ui.view.zebra} onchange={(e) => setView('zebra', (e.currentTarget as HTMLInputElement).checked)}>Row striping</Switch>
          </div>
        </div>
      {/snippet}
    </LogPopover>

    {#if ui.mode === 'live'}
      <span class="sepv tall"></span>
      <button
        type="button"
        class="btn"
        class:warn={session.paused}
        aria-pressed={session.paused}
        onclick={() => (session.paused ? session.resume() : session.pause())}
        title={session.paused ? 'Resume live updates' : 'Freeze the view while logs keep buffering'}
      >
        {#if session.paused}<Play size={14} />Resume{#if buffered > 0}<span class="badge warn">{buffered.toLocaleString()}</span>{/if}{:else}<Pause size={14} />Pause{/if}
      </button>
      <button type="button" class="btn icon" aria-label="Clear view" title="Clear view (stored logs are kept)" onclick={onClear}><Trash2 size={16} /></button>
    {/if}

    <LogPopover bind:open={exportOpen} align="end" width={320} label="Export">
      {#snippet trigger({ toggle, attrs })}
        <button type="button" class="btn" onclick={toggle} {...attrs}><Download size={14} />Export<ChevronDown size={14} /></button>
      {/snippet}
      {#snippet children(close)}
        <div class="menu list">
          <button type="button" class="item" onclick={() => { onExportView(); close(); }}>
            <FileDown size={16} />
            <span><b>Download current view</b><small>Filtered lines as a text file</small></span>
          </button>
          <button type="button" class="item" onclick={() => { onCopyView(); close(); }}>
            <ClipboardCopy size={16} />
            <span><b>Copy current view</b><small>Filtered lines to the clipboard</small></span>
          </button>
          {#if exportTargets.length > 0}
            <div class="msep">Full stored history</div>
            {#each exportTargets as p (p.id)}
              <button type="button" class="item" onclick={() => { onExportProcess(p.id); close(); }}>
                <Download size={16} />
                <span><b>{labels.get(p.id) ?? p.id}</b><small>Everything retained for this process</small></span>
              </button>
            {/each}
          {/if}
        </div>
      {/snippet}
    </LogPopover>
  </div>
</div>

<style>
  .toolbar-wrap {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    position: relative;
    z-index: 20;
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
    min-width: 0;
  }

  .grow {
    flex: 1;
  }

  .filter {
    flex: 1 1 260px;
    min-width: 200px;
    display: flex;
    align-items: center;
    gap: 4px;
    height: 32px;
    padding: 0 4px 0 10px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--text-2);
    transition: border-color var(--dur-fast) var(--ease), box-shadow var(--dur-fast) var(--ease);
  }

  .filter:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-subtle);
  }

  .filter.bad {
    border-color: var(--err);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--err) 18%, transparent);
  }

  .filter input {
    flex: 1;
    min-width: 0;
    height: 100%;
    padding: 0 4px;
    border: none;
    background: transparent;
    box-shadow: none;
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
  }

  .filter input:focus {
    border: none;
    box-shadow: none;
  }

  .hint {
    margin-right: 4px;
    user-select: none;
  }

  .tog {
    width: 24px;
    height: 24px;
    padding: 0;
    border: 1px solid transparent;
    background: transparent;
    color: var(--text-2);
    border-radius: var(--radius-sm);
  }

  .tog:hover:not(:disabled) {
    background: var(--bg-hover);
    border-color: transparent;
    color: var(--text-0);
  }

  .tog.on {
    color: var(--accent);
    background: var(--accent-subtle);
    border-color: color-mix(in srgb, var(--accent) 40%, transparent);
  }

  .sepv {
    width: 1px;
    height: 16px;
    background: var(--border-strong);
    margin: 0 2px;
  }

  .sepv.tall {
    height: 22px;
    margin: 0 2px;
  }

  .seg > button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-sm);
  }

  .seg.full {
    display: flex;
    width: 100%;
  }

  .seg.full > button {
    flex: 1;
    justify-content: center;
    padding: 4px 6px;
  }

  .chip.err.active {
    border-color: color-mix(in srgb, var(--log-stderr) 55%, transparent);
    background: color-mix(in srgb, var(--log-stderr) 14%, transparent);
  }

  .btn.on {
    background: var(--accent-subtle);
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
    color: var(--accent);
  }

  .btn.warn {
    background: color-mix(in srgb, var(--warn) 14%, var(--bg-2));
    border-color: color-mix(in srgb, var(--warn) 50%, var(--border));
    color: var(--text-0);
  }

  .btn {
    height: 32px;
  }

  .btn.icon {
    width: 32px;
    padding: 0;
  }

  .btn.icon.sm {
    width: 26px;
    height: 26px;
  }

  .errline {
    color: var(--err);
    font-size: var(--fs-xs);
    font-family: var(--font-mono);
    margin-top: -4px;
  }

  .menu {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-4);
  }

  .menu.list {
    gap: 1px;
    padding: var(--space-2);
  }

  .mrow {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }

  .mrow.col {
    flex-direction: column;
    align-items: stretch;
  }

  .mlabel {
    font-size: var(--fs-xs);
    font-weight: 600;
    
    letter-spacing: 0;
    color: var(--text-2);
  }

  .stepper {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }

  .num {
    min-width: 42px;
    text-align: center;
    font-variant-numeric: tabular-nums;
    font-size: var(--fs-sm);
  }

  .switches {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3) var(--space-4);
    padding-top: var(--space-3);
    border-top: 1px solid var(--border);
  }

  .item {
    width: 100%;
    justify-content: flex-start;
    align-items: flex-start;
    gap: 12px;
    padding: 9px 10px;
    border: none;
    background: transparent;
    text-align: left;
    font-weight: 400;
    white-space: normal;
  }

  .item:hover:not(:disabled) {
    background: var(--bg-hover);
  }

  .item :global(svg) {
    margin-top: 2px;
    color: var(--text-1);
  }

  .item span {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .item b {
    font-weight: 600;
    color: var(--text-0);
    font-size: var(--fs-sm);
  }

  .item small {
    color: var(--text-2);
    font-size: var(--fs-xs);
  }

  .msep {
    padding: var(--space-3) 10px var(--space-2);
    font-size: var(--fs-xs);
    font-weight: 600;
    
    letter-spacing: 0;
    color: var(--text-2);
    border-top: 1px solid var(--border);
    margin-top: var(--space-2);
  }
</style>
