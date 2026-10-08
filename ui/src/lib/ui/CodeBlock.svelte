<script lang="ts">
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import { getPlatform } from '../platform';

  let {
    code = '',
    lang = '',
    maxHeight = '',
    wrap = false,
    prompt = ''
  }: { code?: string; lang?: string; maxHeight?: string; wrap?: boolean; prompt?: string } = $props();

  let copied = $state(false);
  let timer: ReturnType<typeof setTimeout> | null = null;

  async function copy() {
    await getPlatform().copyText(code);
    copied = true;
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => (copied = false), 1400);
  }
</script>

<div class="code">
  <div class="head">
    <span class="lang">{lang}</span>
    <button type="button" class="copy" onclick={() => void copy()} aria-label="Copy code">
      {#if copied}<Check size={14} />Copied{:else}<Copy size={14} />Copy{/if}
    </button>
  </div>
  <pre class:wrap style={maxHeight ? `max-height:${maxHeight}` : undefined}>{#if prompt}<span class="prompt">{prompt} </span>{/if}{code}</pre>
</div>

<style>
  .code {
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 30px;
    padding: 0 6px 0 var(--space-4);
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .lang {
    
    letter-spacing: 0;
    font-weight: 600;
  }
  .copy {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 22px;
    padding: 0 8px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-1);
    font-size: var(--fs-xs);
    cursor: pointer;
  }
  .copy:hover {
    background: var(--bg-hover);
    color: var(--text-0);
  }
  pre {
    margin: 0;
    padding: var(--space-4);
    overflow: auto;
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    line-height: 1.6;
    color: var(--text-0);
  }
  pre.wrap {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .prompt {
    color: var(--text-2);
    user-select: none;
  }
</style>
