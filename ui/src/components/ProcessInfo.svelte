<script lang="ts">
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import type { Process } from '../lib/api/types';
  import { getPlatform } from '../lib/platform';
  import { toasts } from '../lib/toasts.svelte';
  import { scopeState } from '../lib/state/scope.svelte';
  import ProcessStatus from './ProcessStatus.svelte';
  import { agoShort, commandLine, compactDuration, exitInfo, healthTone, isExited, isFailed, isLive } from './processView';

  let { process }: { process: Process } = $props();

  let now = $state(Date.now());
  let copied = $state('');

  $effect(() => {
    const t = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(t);
  });

  async function copy(key: string, value: string) {
    try {
      await getPlatform().copyText(value);
      copied = key;
      setTimeout(() => {
        if (copied === key) copied = '';
      }, 1400);
    } catch {
      toasts.err('Could not copy to the clipboard');
    }
  }

  function stamp(ts: number | null): string {
    return ts ? new Date(ts).toLocaleString() : '';
  }

  const line = $derived(commandLine(process));
  const wsPath = $derived(scopeState.workspacePath(process.workspaceId));
  const tone = $derived(healthTone(process.health));
  const exit = $derived(exitInfo(process));

  const identity = $derived([
    { key: 'id', label: 'Process ID', value: process.id, mono: true, copy: true },
    { key: 'instance', label: 'Instance', value: process.instanceId, mono: true, copy: true },
    { key: 'app', label: 'App', value: process.app },
    { key: 'profile', label: 'Profile', value: process.profile },
    { key: 'pid', label: 'PID', value: process.pid && isLive(process) ? String(process.pid) : '', mono: true, copy: true },
    { key: 'lifetime', label: 'Lifetime', value: process.lifetime },
    { key: 'policy', label: 'Restart policy', value: process.restartPolicy },
    { key: 'session', label: 'Session', value: process.sessionId, mono: true, copy: true }
  ]);
</script>

<div class="info">
  <section class="block">
    <div class="block-head">
      <h3>Command</h3>
      <button type="button" class="btn ghost sm" onclick={() => void copy('cmd', line)}>
        {#if copied === 'cmd'}<Check size={12} /> Copied{:else}<Copy size={12} /> Copy{/if}
      </button>
    </div>
    <pre class="cmd mono">{line || '—'}</pre>
  </section>

  <section class="block">
    <h3>State</h3>
    <dl class="kv">
      <dt>Status</dt>
      <dd><ProcessStatus {process} /></dd>
      {#if exit}
        <dt>{process.exitSignal ? 'Exit signal' : 'Exit code'}</dt>
        <dd class="mono" class:bad={isFailed(process) || (process.exitCode ?? 0) !== 0}>{exit.replace(/^code /, '')}</dd>
      {/if}
      <dt>Health</dt>
      <dd>
        {#if tone}<span class="badge {tone === 'busy' ? 'info' : tone === 'off' ? '' : tone}">{process.health}</span>{:else}<span class="muted">unknown</span>{/if}
      </dd>
      <dt>Restarts</dt>
      <dd>{#if process.restarts > 0}<span class="badge warn">{process.restarts}</span>{:else}<span class="muted">0</span>{/if}</dd>
      {#if isLive(process) && process.startedAt}
        <dt>Uptime</dt>
        <dd class="mono">{compactDuration(now - process.startedAt)}</dd>
      {/if}
      <dt>Started</dt>
      <dd>
        {#if process.startedAt}{stamp(process.startedAt)} <span class="muted">· {agoShort(process.startedAt, now)}</span>{:else}<span class="muted">—</span>{/if}
      </dd>
      {#if process.exitedAt && (isExited(process) || isFailed(process))}
        <dt>Exited</dt>
        <dd>{stamp(process.exitedAt)} <span class="muted">· {agoShort(process.exitedAt, now)}</span></dd>
      {/if}
      <dt>Ports</dt>
      <dd>
        {#if process.ports.length}
          <span class="ports">{#each process.ports as port (port)}<span class="badge mono">:{port}</span>{/each}</span>
        {:else}<span class="muted">none listening</span>{/if}
      </dd>
    </dl>
  </section>

  <section class="block">
    <h3>Location</h3>
    <dl class="kv">
      <dt>Working directory</dt>
      <dd class="path">
        <span class="mono val" title={process.workdir}>{process.workdir || '—'}</span>
        {#if process.workdir}
          <button type="button" class="copy" aria-label="Copy working directory" onclick={() => void copy('workdir', process.workdir)}>
            {#if copied === 'workdir'}<Check size={12} />{:else}<Copy size={12} />{/if}
          </button>
        {/if}
      </dd>
      <dt>Workspace</dt>
      <dd class="path">
        <span class="val"><strong>{scopeState.workspaceLabel(process.workspaceId)}</strong></span>
        {#if wsPath}<span class="mono muted sub" title={wsPath}>{wsPath}</span>{/if}
      </dd>
    </dl>
  </section>

  <section class="block">
    <h3>Identity</h3>
    <dl class="kv">
      {#each identity as row (row.key)}
        {#if row.value}
          <dt>{row.label}</dt>
          <dd class="path">
            <span class="val" class:mono={row.mono} title={row.value}>{row.value}</span>
            {#if row.copy}
              <button type="button" class="copy" aria-label="Copy {row.label}" onclick={() => void copy(row.key, row.value)}>
                {#if copied === row.key}<Check size={12} />{:else}<Copy size={12} />{/if}
              </button>
            {/if}
          </dd>
        {/if}
      {/each}
      <dt>Output</dt>
      <dd class="mono">{process.stdoutLines.toLocaleString()} stdout <span class="muted">·</span> {process.stderrLines.toLocaleString()} stderr</dd>
    </dl>
  </section>
</div>

<style>
  .info {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .block {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  h3 {
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.07em;
    text-transform: uppercase;
    color: var(--text-2);
  }
  .block-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .cmd {
    margin: 0;
    padding: var(--space-4);
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    font-size: var(--fs-sm);
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 168px;
    overflow: auto;
    color: var(--text-0);
  }
  .kv {
    display: grid;
    grid-template-columns: 140px minmax(0, 1fr);
    margin: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    font-size: var(--fs-sm);
  }
  .kv dt,
  .kv dd {
    padding: 8px var(--space-4);
    border-bottom: 1px solid var(--border);
    margin: 0;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .kv dt {
    color: var(--text-2);
    background: color-mix(in srgb, var(--bg-3) 45%, transparent);
  }
  .kv dt:last-of-type,
  .kv dd:last-of-type {
    border-bottom: none;
  }
  .kv dd.bad {
    color: var(--err);
  }
  .kv dd.path {
    justify-content: space-between;
    flex-wrap: wrap;
  }
  .val {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }
  .sub {
    width: 100%;
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .copy {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-2);
    border-radius: 5px;
    opacity: 0;
  }
  .kv dd:hover .copy,
  .copy:focus-visible {
    opacity: 1;
  }
  .copy:hover {
    background: var(--bg-3);
    color: var(--text-0);
  }
  .ports {
    display: inline-flex;
    gap: 4px;
    flex-wrap: wrap;
  }
</style>
