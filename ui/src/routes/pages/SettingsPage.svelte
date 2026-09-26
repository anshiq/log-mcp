<script lang="ts">
  import { onMount } from 'svelte';
  import { SystemService } from '../../lib/api';

  let settings = $state<Record<string, unknown>>({});
  let logLevel = $state('info');
  let idleExit = $state('');
  let version = $state<{ daemonVersion: string; apiVersion: string } | null>(null);
  let health = $state<{ uptimeMs: number } | null>(null);
  let saving = $state(false);
  let saved = $state(false);

  async function load() {
    const [s, v, h] = await Promise.all([SystemService.getSettings(), SystemService.version(), SystemService.health()]);
    settings = s.settings ?? {};
    logLevel = String(settings.log_level ?? 'info').replace(/"/g, '');
    idleExit = String(settings.idle_exit ?? '').replace(/"/g, '');
    version = v;
    health = h;
  }

  async function save() {
    saving = true;
    saved = false;
    try {
      await SystemService.updateSettings({ logLevel, idleExit });
      saved = true;
    } finally {
      saving = false;
    }
  }

  async function shutdown(keepProcesses: boolean) {
    const msg = keepProcesses
      ? 'Shut down the daemon and keep managed processes running (they reattach on next start)?'
      : 'Shut down the daemon AND stop every managed process?';
    if (!confirm(msg)) return;
    await SystemService.shutdown(keepProcesses);
  }

  function uptime(ms: number): string {
    const s = Math.floor(ms / 1000);
    if (s < 60) return `${s}s`;
    if (s < 3600) return `${Math.floor(s / 60)}m`;
    return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  }

  onMount(load);
</script>

<div class="page">
  <section>
    <h2>Daemon settings</h2>
    <label>
      Log level
      <select bind:value={logLevel}>
        <option value="debug">debug</option>
        <option value="info">info</option>
        <option value="warn">warn</option>
        <option value="error">error</option>
      </select>
    </label>
    <label>
      Idle exit
      <input bind:value={idleExit} placeholder="e.g. 30m" />
    </label>
    <button onclick={save} disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
    {#if saved}<span class="ok">Saved</span>{/if}
  </section>
  <section>
    <h2>About</h2>
    {#if version && health}
      <dl>
        <dt>Daemon version</dt>
        <dd class="mono">{version.daemonVersion}</dd>
        <dt>API version</dt>
        <dd class="mono">{version.apiVersion}</dd>
        <dt>Uptime</dt>
        <dd>{uptime(health.uptimeMs)}</dd>
      </dl>
    {/if}
  </section>
  <section class="danger">
    <h2>Danger zone</h2>
    <div class="row">
      <button onclick={() => shutdown(true)}>Shut down (keep processes)</button>
      <button class="err" onclick={() => shutdown(false)}>Shut down and stop everything</button>
    </div>
  </section>
</div>

<style>
  .page {
    padding: var(--space-5);
    overflow: auto;
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: var(--space-7);
    max-width: 560px;
  }
  h2 {
    font-size: var(--fs-lg);
    margin: 0 0 var(--space-4);
  }
  section {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    align-items: flex-start;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fs-sm);
    color: var(--text-1);
    width: 100%;
  }
  select,
  input {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    color: var(--text-0);
  }
  button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-4);
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  button.err {
    border-color: var(--err);
    color: var(--err);
  }
  .ok {
    color: var(--ok);
    font-size: var(--fs-sm);
  }
  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: var(--space-2) var(--space-4);
    font-size: var(--fs-sm);
    margin: 0;
  }
  dt {
    color: var(--text-2);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .danger {
    border: 1px solid var(--err);
    border-radius: var(--radius);
    padding: var(--space-4);
  }
  .row {
    display: flex;
    gap: var(--space-3);
  }
</style>
