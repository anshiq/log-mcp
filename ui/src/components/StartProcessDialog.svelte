<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Plus from '@lucide/svelte/icons/plus';
  import X from '@lucide/svelte/icons/x';
  import FolderOpen from '@lucide/svelte/icons/folder-open';
  import Info from '@lucide/svelte/icons/info';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import { ConfigService, ProcessService } from '../lib/api';
  import { splitCommand } from '../lib/shellquote';
  import { scopeState } from '../lib/state/scope.svelte';
  import Dialog from '../lib/ui/Dialog.svelte';
  import Spinner from '../lib/ui/Spinner.svelte';
  import Switch from '../lib/ui/Switch.svelte';

  interface AppDef {
    name: string;
    command: string[];
    workdir: string;
  }
  interface EnvRow {
    id: number;
    key: string;
    value: string;
  }

  let {
    workspaceId,
    onClose,
    onStarted
  }: { workspaceId: string; onClose: () => void; onStarted: (processId: string) => void } = $props();

  function asStrings(v: unknown): string[] {
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
  }

  const KEY_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;
  let rowId = 0;

  const available = $derived(scopeState.workspaces.filter((w) => !w.missing));
  const fixed = $derived(workspaceId !== '');

  let ws = $state('');
  let mode = $state<'app' | 'command'>('command');
  let apps = $state<AppDef[]>([]);
  let appsLoading = $state(false);
  let selectedApp = $state('');
  let commandLine = $state('');
  let workdir = $state('');
  let envRows = $state<EnvRow[]>([{ id: ++rowId, key: '', value: '' }]);
  let lifetime = $state<'persistent' | 'session'>('persistent');
  let pty = $state(false);
  let starting = $state(false);
  let submitted = $state(false);
  let error = $state('');
  let commandInput: HTMLInputElement | null = $state(null);
  let appSeq = 0;

  const app = $derived(apps.find((a) => a.name === selectedApp));
  const parts = $derived(splitCommand(commandLine));
  const wsError = $derived(!ws ? 'Choose a workspace' : '');
  const commandError = $derived(mode === 'command' && parts.length === 0 ? 'Enter a command to run' : '');
  const appError = $derived(mode === 'app' && !selectedApp ? 'Choose an app' : '');
  const envIssues = $derived(
    envRows.map((r, i) => {
      const filled = r.key.trim() !== '' || r.value !== '';
      if (!filled) return '';
      const k = r.key.trim();
      if (!k) return 'Name is required';
      if (!KEY_RE.test(k)) return 'Use letters, digits and underscores';
      if (envRows.findIndex((x) => x.key.trim() === k) !== i) return 'Duplicate name';
      return '';
    })
  );
  const valid = $derived(!wsError && !commandError && !appError && envIssues.every((x) => !x));
  const noWorkspaces = $derived(!fixed && available.length === 0);

  $effect(() => {
    if (ws) return;
    if (fixed) ws = workspaceId;
    else if (available.length === 1) ws = available[0]?.id ?? '';
  });

  onMount(() => {
    void tick().then(() => {
      if (mode === 'command') commandInput?.focus();
    });
  });

  $effect(() => {
    const id = ws;
    const my = ++appSeq;
    apps = [];
    selectedApp = '';
    if (!id) return;
    appsLoading = true;
    void ConfigService.get(id)
      .then((cfg) => {
        if (my !== appSeq) return;
        const raw = (cfg.apps as Record<string, Record<string, unknown>>) ?? {};
        apps = Object.entries(raw).map(([name, a]) => ({
          name,
          command: asStrings(a['command'] ?? a['Command']),
          workdir: typeof (a['workdir'] ?? a['WorkDir']) === 'string' ? String(a['workdir'] ?? a['WorkDir']) : ''
        }));
        if (apps.length > 0) {
          selectedApp = apps[0]?.name ?? '';
          mode = 'app';
        } else {
          mode = 'command';
        }
      })
      .catch(() => {
        if (my === appSeq) mode = 'command';
      })
      .finally(() => {
        if (my === appSeq) appsLoading = false;
      });
  });

  function addRow() {
    envRows = [...envRows, { id: ++rowId, key: '', value: '' }];
  }

  function removeRow(id: number) {
    const next = envRows.filter((r) => r.id !== id);
    envRows = next.length > 0 ? next : [{ id: ++rowId, key: '', value: '' }];
  }

  function onKeyPaste(e: ClipboardEvent, id: number) {
    const text = e.clipboardData?.getData('text') ?? '';
    if (!text.includes('=') || !/[\n=]/.test(text)) return;
    const lines = text
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter((l) => l && !l.startsWith('#') && l.includes('='));
    if (lines.length === 0) return;
    e.preventDefault();
    const parsed = lines.map((l) => {
      const i = l.indexOf('=');
      return { id: ++rowId, key: l.slice(0, i).trim().replace(/^export\s+/, ''), value: l.slice(i + 1).trim().replace(/^(['"])(.*)\1$/, '$2') };
    });
    const idx = envRows.findIndex((r) => r.id === id);
    const rest = envRows.filter((r, i) => i !== idx || r.key.trim() !== '' || r.value !== '');
    const at = idx >= 0 ? Math.min(idx, rest.length) : rest.length;
    envRows = [...rest.slice(0, at), ...parsed, ...rest.slice(at)];
  }

  async function doStart() {
    submitted = true;
    error = '';
    if (!valid) return;
    starting = true;
    try {
      const env = envRows.filter((r) => r.key.trim()).map((r) => `${r.key.trim()}=${r.value}`);
      const req: Record<string, unknown> = { workspaceId: ws, lifetime, pty, env };
      if (mode === 'app') {
        req.app = selectedApp;
      } else {
        req.command = parts;
        if (workdir.trim()) req.workdir = workdir.trim();
      }
      const res = await ProcessService.start(req);
      onStarted(String(res.processId));
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      starting = false;
    }
  }
</script>

<Dialog title="Start process" width={540} {onClose}>
  {#snippet children()}
    <form
      class="fields"
      onsubmit={(e) => {
        e.preventDefault();
        void doStart();
      }}
    >
      {#if noWorkspaces}
        <div class="banner info stacked">
          <Info size={16} />
          <div>
            <strong>No workspace is open yet</strong>
            <p>Open a project folder from the workspace switcher in the top bar (enter its path and press Open), then start a process here.</p>
          </div>
        </div>
      {:else if fixed}
        <div class="scope">
          <FolderOpen size={14} />
          <span class="scope-name">{scopeState.workspaceLabel(workspaceId)}</span>
          <span class="scope-path mono truncate" title={scopeState.workspacePath(workspaceId)}>{scopeState.workspacePath(workspaceId)}</span>
        </div>
      {:else}
        <div class="form-field">
          <label for="sp-ws">Workspace</label>
          <select id="sp-ws" bind:value={ws} aria-invalid={submitted && !!wsError}>
            {#if available.length !== 1}<option value="" disabled>Select a workspace…</option>{/if}
            {#each available as w (w.id)}
              <option value={w.id}>{scopeState.workspaceLabel(w.id)} — {w.path}</option>
            {/each}
          </select>
          {#if submitted && wsError}<span class="err-text">{wsError}</span>{/if}
        </div>
      {/if}

      <div class="seg modes" role="group" aria-label="Start from">
        <button type="button" class:active={mode === 'app'} aria-pressed={mode === 'app'} disabled={apps.length === 0} onclick={() => (mode = 'app')}>
          From config{#if apps.length > 0} <span class="n">{apps.length}</span>{/if}
        </button>
        <button type="button" class:active={mode === 'command'} aria-pressed={mode === 'command'} onclick={() => (mode = 'command')}>Command</button>
      </div>

      {#if mode === 'app'}
        <div class="form-field">
          <label for="sp-app">App</label>
          <select id="sp-app" bind:value={selectedApp}>
            {#each apps as a (a.name)}
              <option value={a.name}>{a.name}</option>
            {/each}
          </select>
          {#if app}
            <pre class="preview mono">{app.command.join(' ') || '—'}</pre>
            {#if app.workdir}<span class="hint">Runs in <span class="mono">{app.workdir}</span></span>{/if}
          {/if}
        </div>
      {:else}
        <div class="form-field">
          <label for="sp-cmd">Command</label>
          <input
            id="sp-cmd"
            class="mono"
            bind:this={commandInput}
            bind:value={commandLine}
            placeholder="npm run dev"
            autocomplete="off"
            spellcheck="false"
            aria-invalid={submitted && !!commandError}
          />
          {#if submitted && commandError}
            <span class="err-text">{commandError}</span>
          {:else if appsLoading}
            <span class="hint">Loading apps…</span>
          {:else}
            <span class="hint">Quotes are supported, for example <span class="mono">sh -c "echo hi; sleep 30"</span></span>
          {/if}
        </div>
        <div class="form-field">
          <label for="sp-wd">Working directory <span class="opt">optional</span></label>
          <input id="sp-wd" class="mono" bind:value={workdir} placeholder="Defaults to the workspace root" autocomplete="off" spellcheck="false" />
        </div>
      {/if}

      <div class="form-field">
        <div class="lbl-row">
          <span class="field-label">Environment <span class="opt">optional</span></span>
          <button type="button" class="btn ghost sm" onclick={addRow}><Plus size={12} /> Add variable</button>
        </div>
        <div class="env">
          {#each envRows as row, i (row.id)}
            <div class="env-row">
              <input
                class="mono k"
                bind:value={row.key}
                placeholder="NAME"
                aria-label="Variable name {i + 1}"
                aria-invalid={!!envIssues[i]}
                autocomplete="off"
                spellcheck="false"
                onpaste={(e) => onKeyPaste(e, row.id)}
              />
              <span class="eq">=</span>
              <input class="mono v" bind:value={row.value} placeholder="value" aria-label="Variable value {i + 1}" autocomplete="off" spellcheck="false" />
              <button
                type="button"
                class="btn ghost icon sm"
                aria-label="Remove variable {i + 1}"
                title="Remove"
                onclick={() => removeRow(row.id)}
                disabled={envRows.length === 1 && !row.key && !row.value}
              >
                <X size={14} />
              </button>
              {#if envIssues[i]}<span class="err-text row-err">{envIssues[i]}</span>{/if}
            </div>
          {/each}
        </div>
        <span class="hint">Paste a block of <span class="mono">KEY=value</span> lines into a name field to add several at once. Supports <span class="mono">{'${port:name}'}</span> templates.</span>
      </div>

      <div class="opts">
        <div class="opt-block">
          <span class="field-label">Lifetime</span>
          <div class="seg" role="group" aria-label="Lifetime">
            <button type="button" class:active={lifetime === 'persistent'} aria-pressed={lifetime === 'persistent'} onclick={() => (lifetime = 'persistent')}>Persistent</button>
            <button type="button" class:active={lifetime === 'session'} aria-pressed={lifetime === 'session'} onclick={() => (lifetime = 'session')}>Session</button>
          </div>
        </div>
        <div class="opt-block pty">
          <span class="field-label">Pseudo-terminal</span>
          <div class="pty-row">
            <Switch bind:checked={pty} label="Allocate a PTY" />
            <span class="hint">Allocate a PTY</span>
          </div>
        </div>
      </div>

      {#if error}
        <div class="banner err"><TriangleAlert size={14} /> <span>{error}</span></div>
      {/if}
      <button type="submit" class="hidden" tabindex="-1" aria-hidden="true"></button>
    </form>
  {/snippet}
  {#snippet footer()}
    <button type="button" class="btn" onclick={onClose}>Cancel</button>
    <button type="button" class="btn primary" disabled={starting || noWorkspaces} onclick={doStart}>
      {#if starting}<Spinner size={14} />{/if}Start
    </button>
  {/snippet}
</Dialog>

<style>
  .fields {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .scope {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 7px 11px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-2);
    color: var(--text-2);
    min-width: 0;
  }
  .scope-name {
    color: var(--text-0);
    font-weight: 600;
    font-size: var(--fs-sm);
    flex: none;
  }
  .scope-path {
    font-size: var(--fs-xs);
    min-width: 0;
  }
  .modes {
    align-self: stretch;
  }
  .modes > button {
    flex: 1;
  }
  .modes .n {
    margin-left: 4px;
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
  }
  .form-field select,
  .form-field input {
    width: 100%;
  }
  .form-field .mono {
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
  }
  .form-field :global([aria-invalid='true']) {
    border-color: var(--err);
  }
  .form-field :global([aria-invalid='true']:focus) {
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--err) 20%, transparent);
  }
  .form-field label,
  .field-label {
    text-transform: none;
    letter-spacing: 0;
    font-size: var(--fs-sm);
    color: var(--text-0);
    font-weight: 500;
  }
  .opt {
    color: var(--text-2);
    font-weight: 400;
    margin-left: 4px;
    font-size: var(--fs-xs);
  }
  .preview {
    margin: 0;
    padding: 8px 11px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-0);
    color: var(--text-1);
    font-size: var(--fs-xs);
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 96px;
    overflow: auto;
  }
  .lbl-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 26px;
  }
  .env {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .env-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto minmax(0, 1.4fr) 26px;
    align-items: center;
    gap: 6px;
  }
  .eq {
    color: var(--text-2);
    font-family: var(--font-mono);
  }
  .row-err {
    grid-column: 1 / -1;
  }
  .err-text {
    font-size: var(--fs-xs);
    color: var(--err);
  }
  .opts {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-5);
    padding-top: var(--space-4);
    border-top: 1px solid var(--border);
  }
  .opt-block {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .pty-row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: 30px;
  }
  .hint {
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .stacked {
    align-items: flex-start;
  }
  .stacked p {
    margin: 2px 0 0;
    color: var(--text-1);
    font-size: var(--fs-sm);
    line-height: 1.5;
  }
  .stacked :global(svg) {
    color: var(--info);
    margin-top: 2px;
    flex: none;
  }
  .hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    border: none;
    padding: 0;
  }
</style>
