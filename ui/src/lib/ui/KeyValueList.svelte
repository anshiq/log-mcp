<script lang="ts">
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import { getPlatform } from '../platform';

  type Item = { k: string; v: string; copy?: boolean; mono?: boolean };

  let { items = [], labelWidth = 160 }: { items?: Item[]; labelWidth?: number } = $props();

  let copied = $state('');

  async function copy(item: Item) {
    await getPlatform().copyText(item.v);
    copied = item.k;
    setTimeout(() => {
      if (copied === item.k) copied = '';
    }, 1400);
  }
</script>

<dl class="kv" style={`--kv-label:${labelWidth}px`}>
  {#each items as it (it.k)}
    <dt>{it.k}</dt>
    <dd class:mono={it.mono !== false}>
      <span class="val">{it.v || '—'}</span>
      {#if it.copy && it.v}
        <button type="button" class="copy" aria-label={`Copy ${it.k}`} title={`Copy ${it.k}`} onclick={() => void copy(it)}>
          {#if copied === it.k}<Check size={14} />{:else}<Copy size={14} />{/if}
        </button>
      {/if}
    </dd>
  {/each}
</dl>

<style>
  .kv {
    display: grid;
    grid-template-columns: max-content 1fr;
    column-gap: 12px;
    row-gap: 4px;
    margin: 0;
    font-size: var(--fs-sm);
  }
  dt,
  dd {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 0;
    padding: 2px 0;
    border-bottom: 1px solid var(--border);
    margin: 0;
  }
  dt {
    color: var(--text-2);
  }
  dd {
    color: var(--text-0);
    min-width: 0;
  }
  dd.mono {
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
  }
  .val {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  dt:nth-last-of-type(1),
  dd:last-of-type {
    border-bottom: none;
  }
  .copy {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 22px;
    height: 22px;
    padding: 0;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
  }
  .copy:hover {
    background: var(--bg-hover);
    color: var(--text-0);
  }
</style>
