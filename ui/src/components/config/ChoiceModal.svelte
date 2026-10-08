<script lang="ts">
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import type { ChoiceRequest } from '../../lib/config/session.svelte';

  let { request, onAnswer }: { request: ChoiceRequest; onAnswer: (id: string | null) => void } = $props();

  const danger = $derived(request.options.some((o) => o.tone === 'danger'));
</script>

<Dialog title={request.title} width={480} onClose={() => onAnswer(null)}>
  <div class="body">
    <div class="glyph" class:danger>
      <TriangleAlert size={16} />
    </div>
    <div class="text">
      <p>{request.message}</p>
      {#if request.details && request.details.length > 0}
        <ul>
          {#each request.details as d}
            <li class="mono">{d}</li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>
  {#snippet footer()}
    <button class="btn" onclick={() => onAnswer(null)}>Cancel</button>
    {#each request.options as o (o.id)}
      <button class="btn" class:primary={o.tone === 'primary'} class:danger={o.tone === 'danger'} onclick={() => onAnswer(o.id)}>{o.label}</button>
    {/each}
  {/snippet}
</Dialog>

<style>
  .body {
    display: flex;
    gap: var(--space-4);
  }
  .glyph {
    flex: none;
    width: 36px;
    height: 36px;
    border-radius: 10px;
    display: grid;
    place-items: center;
    background: color-mix(in srgb, var(--warn) 14%, transparent);
    color: var(--warn);
  }
  .glyph.danger {
    background: color-mix(in srgb, var(--err) 14%, transparent);
    color: var(--err);
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    min-width: 0;
  }
  p {
    color: var(--text-1);
    font-size: var(--fs-md);
    line-height: 1.5;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  li {
    padding: 4px 8px;
    background: var(--bg-3);
    border-radius: var(--radius-sm);
    font-size: var(--fs-xs);
    color: var(--text-1);
  }
</style>
