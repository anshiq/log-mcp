<script lang="ts">
  import { onMount } from 'svelte';
  import { ConfigService, ProcessService } from '../lib/api';
  import { splitCommand } from '../lib/shellquote';

  let {
    workspaceId,
    onClose,
    onStarted
  }: { workspaceId: string; onClose: () => void; onStarted: (processId: string) => void } = $props();

  let mode = $state<'app' | 'command'>('app');
  let apps = $state<{ name: string; command: string[]; workdir: string }[]>([]);
  let selectedApp = $state('');
  let commandLine = $state('');
  let workdir = $state('');
  let envText = $state('');
  let lifetime = $state<'persistent' | 'session'>('persistent');
  let pty = $state(false);
  let starting = $state(false);
  let error = $state('');

  async function loadApps() {
    try {
      const cfg = await ConfigService.get(workspaceId);
      const raw = (cfg.apps as Record<string, { command?: string[]; workdir?: string }>) ?? {};
      apps = Object.entries(raw).map(([name, a]) => ({
        name,
        command: a.command ?? [],
        workdir: a.workdir ?? ''
      }));
      if (apps.length === 0) mode = 'command';
      else selectedApp = apps[0].name;
    } catch {
      mode = 'command';
    }
  }

  onMount(loadApps);

  function parseEnv(): string[] {
    return envText
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l && l.includes('='));
  }

  async function submit(e: Event) {
    e.preventDefault();
    starting = true;
    error = '';
    try {
      const req: Record<string, unknown> = { workspaceId, lifetime, pty, env: parseEnv() };
      if (mode === 'app') {
        req.app = selectedApp;
      } else {
        const parts = splitCommand(commandLine);
        if (parts.length === 0) {
          error = 'Enter a command';
          starting = false;
          return;
        }
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

<div
  class="overlay"
  onclick={onClose}
  onkeydown={(e) => e.key === 'Escape' && onClose()}
  role="button"
  tabindex="-1"
>
  <form class="dialog" role="none" onclick={(e) => e.stopPropagation()} onsubmit={submit}>
    <h2>Start process</h2>
    <div class="mode">
      <button type="button" class:active={mode === 'app'} onclick={() => (mode = 'app')} disabled={apps.length === 0}>
        From config
      </button>
      <button type="button" class:active={mode === 'command'} onclick={() => (mode = 'command')}>Command</button>
    </div>
    {#if mode === 'app'}
      <label>
        App
        <select bind:value={selectedApp}>
          {#each apps as a}
            <option value={a.name}>{a.name} — {a.command.join(' ')}</option>
          {/each}
        </select>
      </label>
    {:else}
      <label>
        Command
        <input placeholder="npm run dev" bind:value={commandLine} autocomplete="off" />
      </label>
      <label>
        Working directory (optional)
        <input placeholder="defaults to the workspace root" bind:value={workdir} autocomplete="off" />
      </label>
    {/if}
    <label>
      Environment (one KEY=value per line)
      <textarea rows="3" bind:value={envText}></textarea>
    </label>
    <div class="row">
      <label class="inline">
        Lifetime
        <select bind:value={lifetime}>
          <option value="persistent">persistent</option>
          <option value="session">session</option>
        </select>
      </label>
      <label class="checkbox">
        <input type="checkbox" bind:checked={pty} />
        Allocate a PTY
      </label>
    </div>
    {#if error}
      <p class="error">{error}</p>
    {/if}
    <div class="actions">
      <button type="button" onclick={onClose}>Cancel</button>
      <button type="submit" class="primary" disabled={starting}>{starting ? 'Starting…' : 'Start'}</button>
    </div>
  </form>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .dialog {
    width: 460px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: var(--space-6);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    box-shadow: var(--shadow-pop);
  }
  h2 {
    margin: 0;
    font-size: var(--fs-lg);
  }
  .mode {
    display: flex;
    gap: var(--space-2);
  }
  .mode button {
    flex: 1;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2);
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  .mode button.active {
    background: var(--accent-subtle);
    color: var(--text-0);
    border-color: var(--accent);
  }
  label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  label.inline {
    flex: 1;
  }
  label.checkbox {
    flex-direction: row;
    align-items: center;
    gap: var(--space-2);
  }
  input,
  select,
  textarea {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    color: var(--text-0);
    font-family: inherit;
  }
  textarea {
    font-family: var(--font-mono);
    resize: vertical;
  }
  .row {
    display: flex;
    gap: var(--space-4);
    align-items: flex-end;
  }
  .error {
    color: var(--err);
    font-size: var(--fs-sm);
    margin: 0;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
  }
  .actions button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-5);
    color: var(--text-0);
  }
  .actions .primary {
    background: var(--accent);
    color: #fff;
    border: none;
  }
</style>
