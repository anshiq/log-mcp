<script lang="ts">
  import type { Snippet } from 'svelte';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import CircleHelp from '@lucide/svelte/icons/circle-help';
  import Dialog from './Dialog.svelte';
  import Button from './Button.svelte';

  let {
    open = $bindable(false),
    title = 'Confirm',
    message = '',
    danger = false,
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    loading = false,
    onConfirm = () => {},
    onCancel = () => {},
    children
  }: {
    open?: boolean;
    title?: string;
    message?: string;
    danger?: boolean;
    confirmLabel?: string;
    cancelLabel?: string;
    loading?: boolean;
    onConfirm?: () => void;
    onCancel?: () => void;
    children?: Snippet;
  } = $props();

  function cancel() {
    open = false;
    onCancel();
  }

  function confirm() {
    open = false;
    onConfirm();
  }
</script>

{#if open}
  <Dialog {title} width={440} icon={danger ? TriangleAlert : CircleHelp} tone={danger ? 'danger' : 'default'} onClose={cancel}>
    {#if message}<p class="msg">{message}</p>{/if}
    {#if children}<div class="extra">{@render children()}</div>{/if}
    {#snippet footer()}
      <Button variant="secondary" autofocus={danger} onclick={cancel}>{cancelLabel}</Button>
      <Button variant={danger ? 'destructive' : 'primary'} autofocus={!danger} {loading} onclick={confirm}>{confirmLabel}</Button>
    {/snippet}
  </Dialog>
{/if}

<style>
  .msg {
    margin: 0;
    color: var(--text-1);
    white-space: pre-line;
    overflow-wrap: anywhere;
    line-height: 1.55;
  }
  .extra {
    margin-top: var(--space-4);
  }
</style>
