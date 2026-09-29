<script lang="ts">
  import { dialogs } from '../state/dialogs.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';

  const state = $derived(dialogs.confirmState);
</script>

{#if state}
  <ConfirmDialog
    open={true}
    title={state.title ?? 'Confirm'}
    message={state.message}
    danger={state.danger ?? false}
    confirmLabel={state.confirmLabel ?? 'Confirm'}
    onConfirm={() => dialogs.resolveConfirm(true)}
    onCancel={() => dialogs.resolveConfirm(false)}
  />
{/if}
