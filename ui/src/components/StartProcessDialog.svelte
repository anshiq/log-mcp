<script lang="ts">
  import { onMount } from 'svelte';
  import { ConfigService, ProcessService } from '../lib/api';
  import { splitCommand } from '../lib/shellquote';
  import Dialog from '../lib/ui/Dialog.svelte';
  import Button from '../lib/ui/Button.svelte';
  import Input from '../lib/ui/Input.svelte';
  import Select from '../lib/ui/Select.svelte';
  import Checkbox from '../lib/ui/Checkbox.svelte';

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
      else selectedApp = apps[0]?.name ?? '';
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

  async function doStart() {
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

<Dialog title="Start process" width={460} {onClose}>
  {#snippet children()}
    <form
      class="fields"
      onsubmit={(e) => {
        e.preventDefault();
        void doStart();
      }}
    >
      <div class="mode">
        <button type="button" class:active={mode === 'app'} onclick={() => (mode = 'app')} disabled={apps.length === 0}>
          From config
        </button>
        <button type="button" class:active={mode === 'command'} onclick={() => (mode = 'command')}>Command</button>
      </div>
      {#if mode === 'app'}
        <label class="field-label">
          App
          <Select bind:value={selectedApp} options={apps.map((a) => ({ value: a.name, label: `${a.name} — ${a.command.join(' ')}` }))} />
        </label>
      {:else}
        <label class="field-label">
          Command
          <Input placeholder="npm run dev" bind:value={commandLine} />
        </label>
        <label class="field-label">
          Working directory (optional)
          <Input placeholder="defaults to the workspace root" bind:value={workdir} />
        </label>
      {/if}
      <label class="field-label">
        Environment (one KEY=value per line)
        <textarea rows="3" bind:value={envText}></textarea>
      </label>
      <div class="row">
        <label class="field-label inline">
          Lifetime
          <Select
            bind:value={lifetime}
            options={[
              { value: 'persistent', label: 'persistent' },
              { value: 'session', label: 'session' }
            ]}
          />
        </label>
        <Checkbox bind:checked={pty}>Allocate a PTY</Checkbox>
      </div>
      {#if error}
        <p class="error">{error}</p>
      {/if}
      <button type="submit" class="visually-hidden" tabindex="-1" aria-hidden="true"></button>
    </form>
  {/snippet}
  {#snippet footer()}
    <Button variant="secondary" onclick={onClose}>Cancel</Button>
    <Button variant="primary" loading={starting} onclick={doStart}>Start</Button>
  {/snippet}
</Dialog>

<style>
  .fields {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
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
  .field-label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  .field-label.inline {
    flex: 1;
  }
  textarea {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    color: var(--text-0);
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
  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    border: none;
    padding: 0;
  }
</style>
