<script lang="ts">
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import { toasts } from '../../lib/toasts.svelte';
  import { prettyJson, tokenizeJson } from './json';

  let { value, label = 'Payload' }: { value: unknown; label?: string } = $props();

  let copied = $state(false);
  const source = $derived(prettyJson(value));
  const segments = $derived(tokenizeJson(source));

  async function copy() {
    try {
      await navigator.clipboard.writeText(source);
      copied = true;
      toasts.ok(`${label} copied`);
      setTimeout(() => (copied = false), 1500);
    } catch {
      toasts.err('Copy failed');
    }
  }
</script>

<div class="json">
  <button class="btn ghost sm icon copy" onclick={copy} aria-label={`Copy ${label.toLowerCase()}`} title="Copy">
    {#if copied}<Check size={14} />{:else}<Copy size={14} />{/if}
  </button>
  <pre>{#each segments as s, i (i)}<span class={s.kind}>{s.text}</span>{/each}</pre>
</div>

<style>
  .json {
    position: relative;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }
  pre {
    margin: 0;
    padding: 12px 40px 12px 14px;
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    line-height: 1.4;
    overflow: auto;
    max-height: 280px;
    color: var(--text-1);
    white-space: pre;
  }
  .copy {
    position: absolute;
    top: 6px;
    right: 6px;
  }
  .key {
    color: var(--info);
  }
  .string {
    color: var(--ok);
  }
  .number {
    color: var(--warn);
  }
  .literal {
    color: var(--accent);
  }
  .punct {
    color: var(--text-2);
  }
</style>
