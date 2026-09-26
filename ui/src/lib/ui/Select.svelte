<script lang="ts">
  let {
    value = $bindable(''),
    options,
    label,
    disabled = false,
    onchange
  }: {
    value?: string;
    options: { value: string; label: string }[];
    label?: string;
    disabled?: boolean;
    onchange?: (e: Event) => void;
  } = $props();
</script>

<select class="field" bind:value {disabled} aria-label={label} {onchange}>
  {#each options as opt}
    <option value={opt.value}>{opt.label}</option>
  {/each}
</select>

<style>
  .field {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    color: var(--text-0);
    font-family: var(--font-ui);
    font-size: var(--fs-sm);
  }
  .field:focus {
    outline: none;
    border-color: var(--accent);
  }
  .field:disabled {
    opacity: 0.6;
  }
</style>
