<script lang="ts">
  import { onMount } from 'svelte';
  import ServerOff from '@lucide/svelte/icons/server-off';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import Spinner from '../lib/ui/Spinner.svelte';
  import { getPlatform } from '../lib/platform';

  const INTERVAL = 10;

  let { onRetry = () => {} }: { onRetry?: () => void } = $props();

  const command = 'agent-runtime daemon start';
  let retrying = $state(false);
  let remaining = $state(INTERVAL);
  let copied = $state(false);

  function retry() {
    if (retrying) return;
    retrying = true;
    remaining = INTERVAL;
    onRetry();
    setTimeout(() => (retrying = false), 900);
  }

  async function copy() {
    await getPlatform().copyText(command);
    copied = true;
    setTimeout(() => (copied = false), 1600);
  }

  onMount(() => {
    const t = setInterval(() => {
      if (retrying) return;
      remaining -= 1;
      if (remaining <= 0) retry();
    }, 1000);
    return () => clearInterval(t);
  });
</script>

<div class="wrap">
  <div class="card" role="alert">
    <span class="ico"><ServerOff size={26} /></span>
    <h1>Can’t reach the daemon</h1>
    <p class="lead">agent-runtime couldn’t connect to <code>agentd</code>. It may not be running, or it may still be starting up.</p>

    <div class="step">
      <span class="label">Start it with</span>
      <div class="cmd">
        <code><span class="prompt">$</span> {command}</code>
        <button class="copy" type="button" onclick={() => void copy()} aria-label="Copy start command">
          {#if copied}<Check size={14} />Copied{:else}<Copy size={14} />Copy{/if}
        </button>
      </div>
    </div>

    <div class="actions">
      <button class="retry" type="button" onclick={retry} disabled={retrying}>
        {#if retrying}<Spinner size={14} />Retrying…{:else}<RefreshCw size={14} />Retry now{/if}
      </button>
      <span class="auto">{retrying ? 'Checking connection…' : `Retrying automatically in ${remaining}s`}</span>
    </div>

    <ul class="tips">
      <li>Check the daemon is healthy with <code>agent-runtime daemon status</code>.</li>
      <li>Using the web UI remotely? Make sure the SSH port-forward to <code>127.0.0.1:7350</code> is still open.</li>
    </ul>
  </div>
</div>

<style>
  .wrap {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    padding: var(--space-6);
    background: var(--bg-0);
    overflow: auto;
  }
  .card {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: var(--space-4);
    width: 100%;
    max-width: 520px;
    padding: var(--space-8);
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
  }
  .ico {
    display: grid;
    place-items: center;
    width: 52px;
    height: 52px;
    border-radius: 14px;
    background: color-mix(in srgb, var(--warn) 14%, transparent);
    color: var(--warn);
  }
  h1 {
    font-size: var(--fs-xl);
    margin-top: var(--space-2);
  }
  .lead {
    color: var(--text-1);
    font-size: var(--fs-md);
    line-height: 1.55;
  }
  code {
    font-size: 0.92em;
  }
  .lead code,
  .tips code {
    padding: 1px 5px;
    border-radius: 4px;
    background: var(--bg-3);
    color: var(--text-0);
  }
  .step {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: var(--space-2);
  }
  .label {
    color: var(--text-2);
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .cmd {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 6px 6px 6px var(--space-4);
    background: var(--bg-0);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
  }
  .cmd code {
    flex: 1;
    min-width: 0;
    overflow-x: auto;
    white-space: nowrap;
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .prompt {
    color: var(--text-2);
    margin-right: 6px;
    user-select: none;
  }
  .copy {
    height: 28px;
    padding: 0 10px;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    flex-wrap: wrap;
    margin-top: var(--space-2);
  }
  .retry {
    height: var(--input-h);
    padding: 0 16px;
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-fg);
  }
  .retry:hover:not(:disabled) {
    background: var(--accent-hover);
    border-color: var(--accent-hover);
  }
  .auto {
    color: var(--text-2);
    font-size: var(--fs-sm);
    font-variant-numeric: tabular-nums;
  }
  .tips {
    margin: var(--space-2) 0 0;
    padding: var(--space-4) 0 0 var(--space-5);
    border-top: 1px solid var(--border);
    color: var(--text-2);
    font-size: var(--fs-sm);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
</style>
