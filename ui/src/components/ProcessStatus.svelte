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

<span class="pill {tone}" title={info ? `${process.status} (${info})` : label}>
  <span class="dot {tone}"></span>
  <span class="label">{label}</span>
  {#if info}<span class="info mono">{info.replace(/^code /, '')}</span>{/if}
</span>

<style>
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 2px 9px 2px 8px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-3);
    color: var(--text-1);
    font-size: var(--fs-xs);
    font-weight: 500;
    line-height: 1.6;
    white-space: nowrap;
    max-width: 100%;
    text-transform: capitalize;
  }
  .pill .dot {
    width: 7px;
    height: 7px;
    box-shadow: none;
  }
  .pill.ok {
    color: var(--ok);
    background: color-mix(in srgb, var(--ok) 11%, transparent);
    border-color: color-mix(in srgb, var(--ok) 28%, transparent);
  }
  .pill.err {
    color: var(--err);
    background: color-mix(in srgb, var(--err) 11%, transparent);
    border-color: color-mix(in srgb, var(--err) 30%, transparent);
  }
  .pill.warn {
    color: var(--warn);
    background: color-mix(in srgb, var(--warn) 11%, transparent);
    border-color: color-mix(in srgb, var(--warn) 30%, transparent);
  }
  .pill.busy {
    color: var(--info);
    background: color-mix(in srgb, var(--info) 11%, transparent);
    border-color: color-mix(in srgb, var(--info) 30%, transparent);
  }
  .pill.off .dot {
    background: var(--neutral);
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .info {
    text-transform: none;
    font-size: 10.5px;
    padding: 0 5px;
    border-radius: 4px;
    background: color-mix(in srgb, currentColor 14%, transparent);
    line-height: 1.5;
  }
</style>
