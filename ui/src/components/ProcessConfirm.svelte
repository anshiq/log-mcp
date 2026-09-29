<script lang="ts">
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Dialog from '../lib/ui/Dialog.svelte';
  import { procActions } from './processActions.svelte';
</script>

{#if procActions.request}
  {@const req = procActions.request}
  <Dialog title={req.title} width={420} onClose={() => procActions.answer(false)}>
    {#snippet children()}
      <div class="body">
        <span class="ico" class:danger={req.danger}><TriangleAlert size={18} /></span>
        <p>{req.message}</p>
      </div>
    {/snippet}
    {#snippet footer()}
      <button class="btn" onclick={() => procActions.answer(false)}>Cancel</button>
      <button class="btn" class:danger={req.danger} class:primary={!req.danger} onclick={() => procActions.answer(true)}>
        {req.confirmLabel}
      </button>
    {/snippet}
  </Dialog>
{/if}

<style>
  .body {
    display: flex;
    gap: var(--space-4);
    align-items: flex-start;
  }
  .ico {
    flex: none;
    width: 34px;
    height: 34px;
    border-radius: 10px;
    display: grid;
    place-items: center;
    background: color-mix(in srgb, var(--warn) 14%, transparent);
    color: var(--warn);
  }
  .ico.danger {
    background: color-mix(in srgb, var(--err) 14%, transparent);
    color: var(--err);
  }
  p {
    margin: 0;
    color: var(--text-1);
    font-size: var(--fs-md);
    line-height: 1.5;
    padding-top: 6px;
  }
  .btn.danger {
    background: var(--err);
    border-color: var(--err);
    color: #fff;
  }
  .btn.danger:hover:not(:disabled) {
    background: color-mix(in srgb, var(--err) 85%, #000);
    border-color: transparent;
  }
</style>
