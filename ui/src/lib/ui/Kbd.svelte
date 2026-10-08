<script lang="ts">
  let { keys = '' }: { keys?: string } = $props();

  const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);

  function glyph(k: string): string {
    if (k === 'Mod') return mac ? '⌘' : 'Ctrl';
    if (k === 'Shift' && mac) return '⇧';
    if (k === 'Alt' && mac) return '⌥';
    return k;
  }

  const sequence = $derived(keys.split(' ').filter(Boolean).map((part) => part.split('+').map(glyph)));
</script>

<span class="keys">
  {#each sequence as combo, i}
    {#if i > 0}<span class="then">then</span>{/if}
    {#each combo as k}<kbd>{k}</kbd>{/each}
  {/each}
</span>

<style>
  .keys {
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }
  kbd {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 20px;
    height: 20px;
    padding: 0 5px;
    border-radius: 4px;
    border: 1px solid var(--border-strong);
    border-bottom-width: 2px;
    background: var(--bg-3);
    color: var(--text-1);
    font-family: var(--font-mono);
    font-size: var(--fs-micro);
    line-height: 1;
  }
  .then {
    margin: 0 2px;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
</style>
