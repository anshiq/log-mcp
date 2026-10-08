<script lang="ts">
  import type { Process } from '../lib/api/types';
  import { exitInfo, isExited, isFailed, statusTone } from './processView';

  let {
    process,
    pending = '',
    showExit = true
  }: {
    process: Pick<Process, 'status' | 'exitCode' | 'exitSignal'>;
    pending?: string;
    showExit?: boolean;
  } = $props();

  const tone = $derived(pending ? 'busy' : statusTone(process.status));
  const label = $derived(pending ? `${pending}…` : process.status);
  const info = $derived(showExit && !pending && (isExited(process) || isFailed(process)) ? exitInfo(process) : '');
</script>

<span class="st {tone}" title={info ? `${process.status} (${info})` : label}>
  <span class="dot {tone}"></span>
  <span class="label">{label}</span>
  {#if info}<span class="info mono">{info.replace(/^code /, '')}</span>{/if}
</span>

<style>
  .st {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-1);
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    font-weight: 400;
    line-height: 1.4;
    white-space: nowrap;
    max-width: 100%;
  }
  .st .dot {
    width: 7px;
    height: 7px;
    box-shadow: none;
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .info {
    font-size: var(--fs-micro);
    color: var(--text-2);
  }
</style>
