<script lang="ts">
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import Square from '@lucide/svelte/icons/square';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import type { Process } from '../lib/api/types';
  import ActionMenu from './ActionMenu.svelte';
  import { processMenu } from './processMenu';
  import { procActions } from './processActions.svelte';
  import { isLive } from './processView';

  let {
    process,
    onLogs,
    onCopyId,
    onRemoved
  }: {
    process: Process;
    onLogs: (id: string) => void;
    onCopyId: (id: string) => void;
    onRemoved: (ids: string[]) => void;
  } = $props();

  const live = $derived(isLive(process));
  const busy = $derived(procActions.busy.has(process.id));
  const items = $derived(
    processMenu(process, { includeLifecycle: true, onLogs, onCopyId, afterRemove: onRemoved })
  );
</script>

<div class="actions">
  <button
    type="button"
    class="btn ghost icon sm act lifecycle"
    title="Restart"
    aria-label="Restart {process.id}"
    disabled={busy}
    onclick={() => void procActions.restart([process.id])}
  >
    <RotateCw size={14} />
  </button>
  <button
    type="button"
    class="btn ghost icon sm act lifecycle stop"
    title="Stop"
    aria-label="Stop {process.id}"
    disabled={busy || !live}
    onclick={() => void procActions.stop([process.id])}
  >
    <Square size={13} />
  </button>
  <button
    type="button"
    class="btn ghost icon sm act lifecycle"
    title="Logs"
    aria-label="Open logs for {process.id}"
    onclick={() => onLogs(process.id)}
  >
    <ScrollText size={14} />
  </button>
  <ActionMenu {items} label="More actions" triggerClass="btn ghost icon sm act">
    <Ellipsis size={15} />
  </ActionMenu>
</div>

<style>
  .actions {
    display: inline-flex;
    align-items: center;
    justify-content: flex-end;
    gap: 2px;
  }
  .actions :global(.act) {
    width: 26px;
    height: 26px;
    padding: 0;
    color: var(--text-2);
  }
  .actions :global(.act:hover:not(:disabled)) {
    color: var(--text-0);
  }
  .actions :global(.stop:hover:not(:disabled)) {
    color: var(--err);
  }
  @container procs (max-width: 620px) {
    .lifecycle {
      display: none;
    }
  }
</style>
