<script lang="ts">
  import { onMount } from 'svelte';
  import Palette from '@lucide/svelte/icons/palette';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import Bell from '@lucide/svelte/icons/bell';
  import Server from '@lucide/svelte/icons/server';
  import SlidersHorizontal from '@lucide/svelte/icons/sliders-horizontal';
  import Info from '@lucide/svelte/icons/info';
  import Monitor from '@lucide/svelte/icons/monitor';
  import Sun from '@lucide/svelte/icons/sun';
  import Moon from '@lucide/svelte/icons/moon';
  import Power from '@lucide/svelte/icons/power';
  import Keyboard from '@lucide/svelte/icons/keyboard';
  import Save from '@lucide/svelte/icons/save';
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
  import VolumeX from '@lucide/svelte/icons/volume-x';
  import Activity from '@lucide/svelte/icons/activity';
  import { SystemService } from '../../lib/api';
  import { prefs } from '../../lib/state/prefs.svelte';
  import { connection } from '../../lib/state/connection.svelte';
  import { processes } from '../../lib/state/processes.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import { duration, processDisplayName } from '../../lib/format';
  import { getPlatform } from '../../lib/platform';
  import Button from '../../lib/ui/Button.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Banner from '../../lib/ui/Banner.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Switch from '../../lib/ui/Switch.svelte';
  import SegmentedControl from '../../lib/ui/SegmentedControl.svelte';
  import KeyValueList from '../../lib/ui/KeyValueList.svelte';
  import ConfirmDialog from '../../lib/ui/ConfirmDialog.svelte';
  import Checkbox from '../../lib/ui/Checkbox.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';

  interface FieldSchema {
    type: string;
    enum?: string[];
    description: string;
  }

  let schema = $state<Record<string, FieldSchema>>({});
  let original = $state<Record<string, unknown>>({});
  let draft = $state<Record<string, unknown>>({});
  let loading = $state(true);
  let loadError = $state('');
  let saving = $state(false);
  let saveError = $state('');
  let shutdownOpen = $state(false);
  let keepProcesses = $state(true);
  let shuttingDown = $state(false);
  let now = $state(Date.now());

  let fontSize = $state(String(prefs.data.logFontSize));
  let maxLines = $state(String(prefs.data.maxBufferLines));
  let backlog = $state(String(prefs.data.defaultBacklog));

  const GROUPS: Record<string, string> = {
    log: 'Logging',
    idle: 'Lifecycle',
    shim: 'Lifecycle',
    retention: 'Storage'
  };
  const GROUP_ORDER = ['Logging', 'Lifecycle', 'Storage'];

  function title(key: string): string {
    const words = key.replace(/([a-z])([A-Z])/g, '$1_$2').split(/[_\s]+/).filter(Boolean);
    const text = words.join(' ');
    return text.charAt(0).toUpperCase() + text.slice(1);
  }

  function groupOf(key: string): string {
    const head = key.split('_')[0] ?? key;
    return GROUPS[head] ?? 'General';
  }

  const groups = $derived.by(() => {
    const keys = Object.keys(schema);
    const names = [...new Set(keys.map(groupOf))].sort((a, b) => {
      const ia = GROUP_ORDER.indexOf(a);
      const ib = GROUP_ORDER.indexOf(b);
      return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib) || a.localeCompare(b);
    });
    return names.map((name) => ({ name, keys: keys.filter((k) => groupOf(k) === name).sort() }));
  });

  const dirtyKeys = $derived(Object.keys(schema).filter((k) => JSON.stringify(draft[k]) !== JSON.stringify(original[k])));
  const dirty = $derived(dirtyKeys.length > 0);

  const themeOptions = [
    { value: 'system', label: 'System', icon: Monitor },
    { value: 'light', label: 'Light', icon: Sun },
    { value: 'dark', label: 'Dark', icon: Moon }
  ];

  const uptimeSeconds = $derived(connection.uptimeSeconds + Math.max(0, Math.floor((now - (connection.lastCheck || now)) / 1000)));

  const muted = $derived(
    prefs.data.mutedProcesses.map((id) => {
      const p = processes.map.get(id);
      return { id, name: p ? processDisplayName(p) : id };
    })
  );

  const notificationState = $derived(typeof Notification === 'undefined' ? 'unsupported' : Notification.permission);

  function normalize(v: unknown): unknown {
    if (typeof v === 'string' && v.startsWith('"') && v.endsWith('"') && v.length >= 2) return v.slice(1, -1);
    return v;
  }

  function apply(settings: Record<string, unknown>) {
    const clean: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(settings)) clean[k] = normalize(v);
    original = clean;
    draft = { ...clean };
  }

  async function load() {
    loading = true;
    loadError = '';
    try {
      const res = await SystemService.getSettings();
      schema = res.schema ?? {};
      apply(res.settings ?? {});
      void connection.check();
    } catch (err) {
      loadError = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  async function save() {
    saving = true;
    saveError = '';
    try {
      const payload: Record<string, unknown> = {};
      for (const k of dirtyKeys) payload[k] = draft[k];
      const res = await SystemService.updateSettings(payload);
      apply(res.settings ?? draft);
      toasts.ok('Daemon settings saved');
    } catch (err) {
      saveError = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }

  function reset() {
    draft = { ...original };
    saveError = '';
  }

  function setDraft(key: string, value: unknown) {
    draft = { ...draft, [key]: value };
  }

  function commitNumber(raw: string, min: number, max: number, current: number): number {
    const n = Math.round(Number(raw));
    if (!Number.isFinite(n) || raw.trim() === '') return current;
    return Math.min(max, Math.max(min, n));
  }

  function commitFont() {
    const n = commitNumber(fontSize, 9, 24, prefs.data.logFontSize);
    prefs.set('logFontSize', n);
    fontSize = String(n);
  }

  function commitMaxLines() {
    const n = commitNumber(maxLines, 1000, 5000000, prefs.data.maxBufferLines);
    prefs.set('maxBufferLines', n);
    maxLines = String(n);
  }

  function commitBacklog() {
    const n = commitNumber(backlog, 0, 100000, prefs.data.defaultBacklog);
    prefs.set('defaultBacklog', n);
    backlog = String(n);
  }

  function setNotify(key: string, on: boolean) {
    prefs.set('notifyOn', { ...prefs.data.notifyOn, [key]: on });
  }

  async function enableNotifications() {
    const ok = await getPlatform().requestNotificationPermission();
    if (typeof Notification !== 'undefined' && Notification.permission === 'default') await Notification.requestPermission();
    if (ok) toasts.info('Notification permission requested');
  }

  async function shutdown() {
    shuttingDown = true;
    try {
      await SystemService.shutdown(keepProcesses);
      toasts.ok(keepProcesses ? 'Daemon is shutting down. Processes keep running.' : 'Daemon is shutting down and stopping all processes.');
    } catch (err) {
      toastError(err);
    } finally {
      shuttingDown = false;
    }
  }

  function enumOptions(f: FieldSchema): { value: string; label: string }[] {
    return (f.enum ?? []).map((v) => ({ value: v, label: v }));
  }

  function unitOf(key: string): string {
    if (key.endsWith('_ms')) return 'ms';
    if (key.endsWith('_seconds') || key.endsWith('_secs')) return 's';
    if (key.endsWith('_bytes')) return 'bytes';
    if (key.endsWith('_mb')) return 'MB';
    return '';
  }

  onMount(() => {
    void load();
    const t = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(t);
  });
</script>

<div class="page">
  <div class="page-header">
    <div class="titles">
      <h1>Settings</h1>
      <p class="subtitle">Personalise this dashboard, and configure the daemon that runs your processes.</p>
    </div>
  </div>

  <div class="stack">
    <section class="card" aria-labelledby="s-appearance">
      <div class="card-header"><Palette size={16} /><span id="s-appearance">Appearance</span></div>
      <div class="rows">
        <div class="row">
          <div class="label">
            <span class="name">Theme</span>
            <span class="desc">Follow your operating system or force a colour scheme.</span>
          </div>
          <SegmentedControl label="Theme" options={themeOptions} value={prefs.data.theme} onchange={(v) => prefs.set('theme', v as 'system' | 'light' | 'dark')} />
        </div>
        <div class="row">
          <div class="label">
            <span class="name">Density</span>
            <span class="desc">Compact tightens rows, tables and controls to fit more on screen.</span>
          </div>
          <SegmentedControl
            label="Density"
            options={[
              { value: 'comfortable', label: 'Comfortable' },
              { value: 'compact', label: 'Compact' }
            ]}
            value={prefs.data.density}
            onchange={(v) => prefs.set('density', v as 'comfortable' | 'compact')}
          />
        </div>
      </div>
    </section>

    <section class="card" aria-labelledby="s-logs">
      <div class="card-header"><ScrollText size={16} /><span id="s-logs">Log viewer</span></div>
      <div class="rows">
        <div class="row">
          <div class="label">
            <span class="name">Font size</span>
            <span class="desc">Text size in the log viewer, between 9 and 24.</span>
          </div>
          <div class="ctl narrow">
            <Input type="number" label="Log font size" min={9} max={24} bind:value={fontSize} onchange={commitFont}>
              {#snippet trailing()}<span class="unit">px</span>{/snippet}
            </Input>
          </div>
        </div>
        <div class="row">
          <div class="label">
            <span class="name">Timestamps</span>
            <span class="desc">How the time column renders for each log line.</span>
          </div>
          <div class="ctl">
            <Select
              label="Timestamp format"
              value={prefs.data.timestamps}
              onchange={(e) => prefs.set('timestamps', (e.currentTarget as HTMLSelectElement).value as 'relative' | 'local' | 'utc' | 'off')}
              options={[
                { value: 'local', label: 'Local time' },
                { value: 'utc', label: 'UTC' },
                { value: 'relative', label: 'Relative (5s ago)' },
                { value: 'off', label: 'Hidden' }
              ]}
            />
          </div>
        </div>
        <div class="row">
          <div class="label">
            <span class="name">Wrap long lines</span>
            <span class="desc">Wrap instead of scrolling horizontally.</span>
          </div>
          <Switch label="Wrap long lines" checked={prefs.data.wrap} onchange={(e) => prefs.set('wrap', (e.currentTarget as HTMLInputElement).checked)} />
        </div>
        <div class="row">
          <div class="label">
            <span class="name">Render ANSI colours</span>
            <span class="desc">Show terminal colour codes as colours instead of raw escape sequences.</span>
          </div>
          <Switch label="Render ANSI colours" checked={prefs.data.ansi} onchange={(e) => prefs.set('ansi', (e.currentTarget as HTMLInputElement).checked)} />
        </div>
        <div class="row">
          <div class="label">
            <span class="name">Max buffered lines</span>
            <span class="desc">Older lines are dropped from the browser once this many are held.</span>
          </div>
          <div class="ctl narrow">
            <Input type="number" label="Max buffer lines" min={1000} max={5000000} step={1000} bind:value={maxLines} onchange={commitMaxLines}>
              {#snippet trailing()}<span class="unit">lines</span>{/snippet}
            </Input>
          </div>
        </div>
        <div class="row">
          <div class="label">
            <span class="name">Default backlog</span>
            <span class="desc">Lines loaded from history when you open a process.</span>
          </div>
          <div class="ctl narrow">
            <Input type="number" label="Default backlog" min={0} max={100000} step={100} bind:value={backlog} onchange={commitBacklog}>
              {#snippet trailing()}<span class="unit">lines</span>{/snippet}
            </Input>
          </div>
        </div>
      </div>
    </section>

    <section class="card" aria-labelledby="s-notify">
      <div class="card-header"><Bell size={16} /><span id="s-notify">Notifications</span></div>
      <div class="rows">
        <div class="row">
          <div class="label">
            <span class="name">Do not disturb</span>
            <span class="desc">Silence all desktop notifications and toasts for alerts.</span>
          </div>
          <Switch label="Do not disturb" checked={prefs.data.dnd} onchange={(e) => prefs.set('dnd', (e.currentTarget as HTMLInputElement).checked)} />
        </div>
        <div class="row">
          <div class="label">
            <span class="name">Process crashes</span>
            <span class="desc">Notify when a running process exits unexpectedly.</span>
          </div>
          <Switch label="Notify on crash" checked={prefs.data.notifyOn['crash'] ?? true} onchange={(e) => setNotify('crash', (e.currentTarget as HTMLInputElement).checked)} />
        </div>
        <div class="row">
          <div class="label">
            <span class="name">Start failures</span>
            <span class="desc">Notify when a process fails to start.</span>
          </div>
          <Switch label="Notify on failure" checked={prefs.data.notifyOn['fail'] ?? true} onchange={(e) => setNotify('fail', (e.currentTarget as HTMLInputElement).checked)} />
        </div>
        <div class="row">
          <div class="label">
            <span class="name">Log alerts</span>
            <span class="desc">Notify when a log alert rule matches.</span>
          </div>
          <Switch label="Notify on log alerts" checked={prefs.data.notifyOn['alert'] ?? true} onchange={(e) => setNotify('alert', (e.currentTarget as HTMLInputElement).checked)} />
        </div>
        {#if notificationState === 'default'}
          <div class="row">
            <div class="label">
              <span class="name">Browser permission</span>
              <span class="desc">Allow this browser to show system notifications.</span>
            </div>
            <Button onclick={() => void enableNotifications()}><Bell size={14} />Enable notifications</Button>
          </div>
        {:else if notificationState === 'denied'}
          <div class="row">
            <div class="label">
              <span class="name">Browser permission</span>
              <span class="desc">Notifications are blocked for this site. Change it in your browser’s site settings.</span>
            </div>
            <Badge tone="warn">Blocked</Badge>
          </div>
        {/if}
        <div class="row top">
          <div class="label">
            <span class="name">Muted processes</span>
            <span class="desc">Processes you silenced alerts for.</span>
          </div>
          <div class="muted-box">
            {#if muted.length === 0}
              <span class="none"><VolumeX size={14} />Nothing muted</span>
            {:else}
              <ul class="mutes">
                {#each muted as m (m.id)}
                  <li>
                    <span class="mname">{m.name}</span>
                    <span class="mono mid">{m.id}</span>
                    <Button size="sm" variant="ghost" onclick={() => prefs.toggleMute(m.id)}>Unmute</Button>
                  </li>
                {/each}
              </ul>
              <Button size="sm" onclick={() => prefs.set('mutedProcesses', [])}>Unmute all</Button>
            {/if}
          </div>
        </div>
      </div>
    </section>

    <section class="card" aria-labelledby="s-daemon-settings">
      <div class="card-header"><SlidersHorizontal size={16} /><span id="s-daemon-settings">Daemon settings</span>{#if dirty}<Badge tone="warn" size="sm" dot>Unsaved changes</Badge>{/if}</div>
      {#if loading}
        <div class="rows">
          {#each [0, 1, 2] as i (i)}
            <div class="row">
              <div class="label"><Skeleton width="120px" height="13px" /><Skeleton width="220px" height="11px" /></div>
              <Skeleton width="200px" height="32px" radius="7px" />
            </div>
          {/each}
        </div>
      {:else if loadError}
        <div class="pad">
          <Banner tone="err" title="Couldn’t load daemon settings" message={loadError}>
            {#snippet actions()}<Button size="sm" onclick={() => void load()}>Retry</Button>{/snippet}
          </Banner>
        </div>
      {:else if groups.length === 0}
        <div class="pad muted">The daemon didn’t report any editable settings.</div>
      {:else}
        <form
          onsubmit={(e) => {
            e.preventDefault();
            if (dirty) void save();
          }}
        >
          {#each groups as g (g.name)}
            <div class="group">{g.name}</div>
            <div class="rows">
              {#each g.keys as key (key)}
                {@const f = schema[key] as FieldSchema}
                {@const unit = unitOf(key)}
                <div class="row" class:changed={dirtyKeys.includes(key)}>
                  <div class="label">
                    <label class="name" for={`set-${key}`}>{title(key)}</label>
                    <span class="desc">{f.description}</span>
                  </div>
                  <div class="ctl" class:seg={f.enum && f.enum.length <= 4}>
                    {#if f.enum && f.enum.length > 0}
                      {#if f.enum.length <= 4}
                        <SegmentedControl label={title(key)} options={enumOptions(f)} value={String(draft[key] ?? '')} onchange={(v) => setDraft(key, v)} />
                      {:else}
                        <Select id={`set-${key}`} value={String(draft[key] ?? '')} options={enumOptions(f)} onchange={(e) => setDraft(key, (e.currentTarget as HTMLSelectElement).value)} />
                      {/if}
                    {:else if f.type === 'boolean'}
                      <Switch label={title(key)} id={`set-${key}`} checked={draft[key] === true} onchange={(e) => setDraft(key, (e.currentTarget as HTMLInputElement).checked)} />
                    {:else if f.type === 'number' || f.type === 'integer'}
                      <Input
                        id={`set-${key}`}
                        type="number"
                        value={String(draft[key] ?? '')}
                        oninput={(e) => setDraft(key, (e.currentTarget as HTMLInputElement).value === '' ? '' : Number((e.currentTarget as HTMLInputElement).value))}
                      >
                        {#snippet trailing()}{#if unit}<span class="unit">{unit}</span>{/if}{/snippet}
                      </Input>
                    {:else}
                      <Input id={`set-${key}`} value={String(draft[key] ?? '')} mono oninput={(e) => setDraft(key, (e.currentTarget as HTMLInputElement).value)} />
                    {/if}
                  </div>
                </div>
              {/each}
            </div>
          {/each}
          {#if saveError}<div class="pad"><Banner tone="err" message={saveError} /></div>{/if}
          <div class="savebar" class:on={dirty}>
            <span class="state">
              {#if dirty}{dirtyKeys.length} unsaved change{dirtyKeys.length === 1 ? '' : 's'}{:else}All changes saved{/if}
            </span>
            <Button onclick={reset} disabled={!dirty || saving}><RotateCcw size={14} />Reset</Button>
            <Button variant="primary" type="submit" loading={saving} disabled={!dirty}>{#if !saving}<Save size={14} />{/if}Save changes</Button>
          </div>
        </form>
      {/if}
    </section>

    <section class="card" aria-labelledby="s-daemon">
      <div class="card-header">
        <Server size={16} /><span id="s-daemon">Daemon</span>
        <span class="spacer"></span>
        {#if connection.reachable}<Badge tone="ok" size="sm" dot>Healthy</Badge>{:else}<Badge tone="err" size="sm" dot>Unreachable</Badge>{/if}
      </div>
      <div class="pad kv">
        <KeyValueList
          items={[
            { k: 'Version', v: connection.version, mono: true, copy: true },
            { k: 'API version', v: connection.apiVersion, mono: true },
            { k: 'Uptime', v: uptimeSeconds > 0 ? duration(uptimeSeconds * 1000) : '', mono: false },
            { k: 'Socket path', v: connection.socketPath, copy: true },
            { k: 'Data directory', v: connection.dataDir, copy: true },
            { k: 'TCP address', v: connection.tcpAddr, copy: true }
          ]}
        />
      </div>
      <div class="danger-zone">
        <div class="label">
          <span class="name">Shut down daemon</span>
          <span class="desc">Stops the daemon. Running processes can be left running and reattached the next time it starts.</span>
        </div>
        <Button variant="danger" onclick={() => ((keepProcesses = true), (shutdownOpen = true))}><Power size={14} />Shutdown daemon</Button>
      </div>
    </section>

    <section class="about" aria-labelledby="s-about">
      <span class="logo"><Activity size={14} strokeWidth={2.4} /></span>
      <div class="ab">
        <span id="s-about" class="name"><Info size={13} />agent-runtime</span>
        <span class="muted">Dashboard {typeof __APP_VERSION__ === 'string' ? `v${__APP_VERSION__}` : ''} · Daemon {connection.version || 'unknown'}</span>
      </div>
      <span class="spacer"></span>
      <Button onclick={() => window.dispatchEvent(new CustomEvent('ar:shortcuts'))}><Keyboard size={14} />Keyboard shortcuts</Button>
    </section>
  </div>
</div>

<ConfirmDialog
  bind:open={shutdownOpen}
  title="Shut down the daemon?"
  message="The dashboard will lose its connection until you run “agent-runtime daemon start” again."
  danger
  confirmLabel="Shut down"
  loading={shuttingDown}
  onConfirm={() => void shutdown()}
>
  <Checkbox bind:checked={keepProcesses}>Keep managed processes running (they reattach on next start)</Checkbox>
</ConfirmDialog>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    width: 100%;
    max-width: 880px;
  }
  .card-header :global(svg) {
    color: var(--text-2);
  }
  .spacer {
    flex: 1;
  }
  .rows {
    display: flex;
    flex-direction: column;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-6);
    padding: var(--space-4) var(--space-5);
    border-bottom: 1px solid var(--border);
    transition: background var(--dur-fast) var(--ease);
  }
  .rows > .row:last-child {
    border-bottom: none;
  }
  .row.top {
    align-items: flex-start;
  }
  .row.changed {
    background: color-mix(in srgb, var(--warn) 6%, transparent);
    box-shadow: inset 2px 0 0 var(--warn);
  }
  .label {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .name {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-0);
    font-size: var(--fs-md);
    font-weight: 500;
  }
  .desc {
    color: var(--text-2);
    font-size: var(--fs-sm);
    line-height: 1.45;
  }
  .ctl {
    width: 240px;
    flex: none;
  }
  .ctl.narrow {
    width: 160px;
  }
  .ctl.seg {
    width: auto;
  }
  .unit {
    padding-right: 6px;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .group {
    padding: var(--space-4) var(--space-5) var(--space-2);
    color: var(--text-2);
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    border-bottom: 1px solid var(--border);
  }
  .group:not(:first-child) {
    border-top: 1px solid var(--border);
  }
  .pad {
    padding: var(--space-4) var(--space-5);
  }
  .pad.kv {
    padding-top: var(--space-2);
    padding-bottom: var(--space-2);
  }
  .savebar {
    position: sticky;
    bottom: 0;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-5);
    border-top: 1px solid var(--border);
    border-radius: 0 0 var(--radius-lg) var(--radius-lg);
    background: color-mix(in srgb, var(--bg-0) 45%, var(--bg-1));
  }
  .savebar .state {
    margin-right: auto;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .savebar.on .state {
    color: var(--warn);
  }
  .muted-box {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: var(--space-3);
    width: 340px;
    max-width: 100%;
  }
  .none {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .mutes {
    width: 100%;
    margin: 0;
    padding: 0;
    list-style: none;
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }
  .mutes li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 4px 6px 4px var(--space-4);
    font-size: var(--fs-sm);
  }
  .mutes li + li {
    border-top: 1px solid var(--border);
  }
  .mname {
    font-weight: 500;
  }
  .mid {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .danger-zone {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-6);
    padding: var(--space-4) var(--space-5);
    border-top: 1px solid var(--border);
    background: color-mix(in srgb, var(--err) 4%, transparent);
    border-radius: 0 0 var(--radius-lg) var(--radius-lg);
  }
  .about {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-2) var(--space-6);
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .about .logo {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border-radius: 8px;
    background: var(--accent);
    color: var(--accent-fg);
  }
  .about .ab {
    display: flex;
    flex-direction: column;
  }
  .about .name {
    font-size: var(--fs-sm);
  }
  .about .name :global(svg) {
    display: none;
  }

  @media (max-width: 860px) {
    .row {
      flex-direction: column;
      align-items: stretch;
      gap: var(--space-3);
    }
    .ctl,
    .ctl.narrow,
    .muted-box {
      width: 100%;
    }
    .ctl.seg {
      width: auto;
    }
    .muted-box {
      align-items: flex-start;
    }
    .danger-zone {
      flex-direction: column;
      align-items: flex-start;
    }
  }
</style>
