<script lang="ts">
  let {
    value = $bindable(''),
    placeholder = '',
    type = 'text',
    label,
    disabled = false,
    autofocus = false,
    onkeydown,
    onchange
  }: {
    value?: string;
    placeholder?: string;
    type?: 'text' | 'password' | 'search';
    label?: string;
    disabled?: boolean;
    autofocus?: boolean;
    onkeydown?: (e: KeyboardEvent) => void;
    onchange?: (e: Event) => void;
  } = $props();

  function focusAction(node: HTMLInputElement) {
    if (autofocus) node.focus();
  }
</script>

<input
  class="field"
  {type}
  {placeholder}
  {disabled}
  bind:value
  aria-label={label}
  {onkeydown}
  {onchange}
  use:focusAction
/>

<style>
  .field {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    color: var(--text-0);
    font-family: var(--font-ui);
    font-size: var(--fs-sm);
    width: 100%;
  }
  .field:focus {
    outline: none;
    border-color: var(--accent);
  }
  .field:disabled {
    opacity: 0.6;
  }
  .field::placeholder {
    color: var(--text-2);
  }
</style>
