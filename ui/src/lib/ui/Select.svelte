<script lang="ts">
  import ChevronDown from '@lucide/svelte/icons/chevron-down';

  let {
    value = $bindable(''),
    options,
    label,
    id,
    disabled = false,
    invalid = false,
    size = 'md',
    onchange
  }: {
    value?: string;
    options: { value: string; label: string; disabled?: boolean }[];
    label?: string;
    id?: string;
    disabled?: boolean;
    invalid?: boolean;
    size?: 'sm' | 'md';
    onchange?: (e: Event) => void;
  } = $props();
</script>

<div class="wrap {size}" class:disabled class:invalid>
  <select class="field" {id} bind:value {disabled} aria-label={label} aria-invalid={invalid || undefined} {onchange}>
    {#each options as opt (opt.value)}
      <option value={opt.value} disabled={opt.disabled}>{opt.label}</option>
    {/each}
  </select>
  <span class="chev"><ChevronDown size={14} /></span>
</div>

<style>
  .wrap {
    position: relative;
    display: flex;
    align-items: center;
    width: 100%;
    height: var(--input-h);
  }
  .wrap.sm {
    height: var(--control-h);
  }
  .field {
    appearance: none;
    -webkit-appearance: none;
    width: 100%;
    height: 100%;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0 30px 0 10px;
    color: var(--text-0);
    font-family: var(--font-ui);
    font-size: var(--fs-sm);
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease),
      box-shadow var(--dur-fast) var(--ease);
  }
  .field:hover:not(:disabled) {
    border-color: var(--border-strong);
  }
  .field:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-subtle);
  }
  .invalid .field {
    border-color: var(--err);
  }
  .field:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }
  .field option {
    background: var(--bg-2);
    color: var(--text-0);
  }
  .chev {
    position: absolute;
    right: 9px;
    display: inline-flex;
    color: var(--text-2);
    pointer-events: none;
  }
</style>
