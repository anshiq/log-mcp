<script lang="ts">
  import Eye from '@lucide/svelte/icons/eye';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import Search from '@lucide/svelte/icons/search';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import { ProcessService } from '../lib/api';
  import { getPlatform } from '../lib/platform';
  import { toastError } from '../lib/toasts.svelte';
  import Switch from '../lib/ui/Switch.svelte';
  import { splitEnvLine } from './processView';

  let { processId }: { processId: string } = $props();

  let env = $state<string[]>([]);
  let redacted = $state(0);
  let loading = $state(true);
  let revealed = $state(false);
  let filter = $state('');
  let copied = $state('');
  let seq = 0;

  async function load() {
    const my = ++seq;
    loading = true;
    try {
      const res = await ProcessService.getEnv(processId, revealed, false);
      if (my !== seq) return;
      env = (res.env as string[]) ?? [];
      redacted = res.redacted ?? 0;
    } catch (err) {
      if (my === seq) toastError(err);
    } finally {
      if (my === seq) loading = false;
    }
  }

  $effect(() => {
    void processId;
    void revealed;
    void load();
  });

  const entries = $derived(
    env
      .map(splitEnvLine)
      .sort((a, b) => a.key.localeCompare(b.key))
      .filter((e) => {
        const q = filter.trim().toLowerCase();
        return !q || e.key.toLowerCase().includes(q) || e.value.toLowerCase().includes(q);
      })
  );

  async function copy(text: string, key: string) {
    try {
      await getPlatform().copyText(text);
      copied = key;
      setTimeout(() => {
        if (copied === key) copied = '';
      }, 1200);
    } catch {
      copied = '';
    }
  }
</script>

<div class="env">
  <div class="bar">
    <div class="input-wrap grow">
      <Search size={13} />
      <input placeholder="Filter variables…" bind:value={filter} aria-label="Filter environment variables" />
    </div>
    <div class="reveal">
      <Switch bind:checked={revealed} label="Reveal secrets" />
      <span class="lbl">{#if revealed}<Eye size={13} />{:else}<EyeOff size={13} />{/if} Reveal secrets</span>
    </div>
  </div>
  <div class="list">
    {#if loading && env.length === 0}
      <div class="state muted">Loading environment…</div>
    {:else if entries.length === 0}
      <div class="state">
        <KeyRound size={20} />
        <span class="muted">{env.length === 0 ? 'No environment variables.' : 'No variables match the filter.'}</span>
      </div>
    {:else}
      <ul>
        {#each entries as e (e.key)}
          <li>
            <span class="k mono">{e.key}</span>
            <span class="v mono" title={e.value}>{e.value || ' '}</span>
            <button type="button" class="copy" aria-label="Copy {e.key}" title="Copy value" onclick={() => void copy(e.value, e.key)}>
              {#if copied === e.key}<Check size={12} />{:else}<Copy size={12} />{/if}
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
  <div class="foot">
    <span>{env.length} variables</span>
    {#if !revealed && redacted > 0}<span>{redacted} values hidden</span>{/if}
  </div>
</div>

<style>
  .env {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-3) var(--space-5);
    border-bottom: 1px solid var(--border);
  }
  .grow {
    flex: 1;
    min-width: 0;
  }
  .grow input {
    padding-top: 4px;
    padding-bottom: 4px;
    font-size: var(--fs-xs);
  }
  .reveal {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    white-space: nowrap;
  }
  .lbl {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-xs);
    color: var(--text-1);
  }
  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    display: grid;
    grid-template-columns: minmax(96px, 38%) minmax(0, 1fr) 24px;
    align-items: center;
    gap: var(--space-4);
    padding: 6px var(--space-5);
    border-bottom: 1px solid var(--border);
    font-size: 11.5px;
  }
  li:hover {
    background: var(--bg-hover);
  }
  .k {
    color: var(--accent);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
  }
  .v {
    color: var(--text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .copy {
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
  li:hover .copy,
  .copy:focus-visible {
    opacity: 1;
  }
  .copy:hover {
    background: var(--bg-3);
    color: var(--text-0);
  }
  .state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    height: 100%;
    min-height: 160px;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .foot {
    display: flex;
    justify-content: space-between;
    padding: 5px var(--space-5);
    border-top: 1px solid var(--border);
    font-size: var(--fs-xs);
    color: var(--text-2);
    min-height: 26px;
  }
</style>
