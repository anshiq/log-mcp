<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    value = $bindable(''),
    placeholder = '',
    type = 'text',
    label,
    id,
    name,
    disabled = false,
    readonly = false,
    autofocus = false,
    invalid = false,
    mono = false,
    size = 'md',
    min,
    max,
    step,
    autocomplete = 'off',
    el = $bindable(null),
    leading,
    trailing,
    onkeydown,
    onchange,
    oninput
  }: {
    value?: string;
    placeholder?: string;
    type?: 'text' | 'password' | 'search' | 'number' | 'url' | 'email';
    label?: string;
    id?: string;
    name?: string;
    disabled?: boolean;
    readonly?: boolean;
    autofocus?: boolean;
    invalid?: boolean;
    mono?: boolean;
    size?: 'sm' | 'md';
    min?: number;
    max?: number;
    step?: number;
    autocomplete?: 'on' | 'off' | 'username' | 'current-password' | 'new-password';
    el?: HTMLInputElement | null;
    leading?: Snippet;
    trailing?: Snippet;
    onkeydown?: (e: KeyboardEvent) => void;
    onchange?: (e: Event) => void;
    oninput?: (e: Event) => void;
  } = $props();

  function focusAction(node: HTMLInputElement) {
    if (autofocus) node.focus();
  }
</script>

<div class="wrap {size}" class:invalid class:disabled class:has-lead={!!leading} class:has-trail={!!trailing}>
  {#if leading}<span class="lead">{@render leading()}</span>{/if}
  <input
    class="field"
    class:mono
    {type}
    {id}
    {name}
    {placeholder}
    {disabled}
    {readonly}
    {min}
    {max}
    {step}
    {autocomplete}
    spellcheck="false"
    bind:value
    bind:this={el}
    aria-label={label}
    aria-invalid={invalid || undefined}
    data-autofocus={autofocus ? '' : undefined}
    {onkeydown}
    {onchange}
    {oninput}
    use:focusAction
  />
  {#if trailing}<span class="trail">{@render trailing()}</span>{/if}
</div>

<style>
  .wrap {
    position: relative;
    display: flex;
    align-items: center;
    width: 100%;
    height: var(--input-h);
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    transition:
      border-color var(--dur-fast) var(--ease),
      box-shadow var(--dur-fast) var(--ease);
  }
  .wrap.sm {
    height: var(--control-h);
  }
  .wrap:hover:not(.disabled) {
    border-color: var(--border-strong);
  }
  .wrap:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-subtle);
  }
  .wrap.invalid {
    border-color: var(--err);
  }
  .wrap.invalid:focus-within {
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--err) 20%, transparent);
  }
  .wrap.disabled {
    opacity: 0.55;
  }
  .field {
    flex: 1;
    min-width: 0;
    height: 100%;
    background: transparent;
    border: none;
    outline: none;
    box-shadow: none;
    padding: 0 10px;
    border-radius: inherit;
    color: var(--text-0);
    font-family: var(--font-ui);
    font-size: var(--fs-sm);
  }
  .field.mono {
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
  }
  .field:disabled {
    cursor: not-allowed;
  }
  .field::placeholder {
    color: var(--text-2);
  }
  .lead,
  .trail {
    display: inline-flex;
    align-items: center;
    color: var(--text-2);
    flex: none;
  }
  .lead {
    padding-left: 10px;
  }
  .has-lead .field {
    padding-left: 8px;
  }
  .trail {
    padding-right: 4px;
  }
  .has-trail .field {
    padding-right: 6px;
  }
  input[type='number']::-webkit-inner-spin-button {
    opacity: 0.4;
  }
</style>
