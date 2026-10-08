<script lang="ts">
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import Check from '@lucide/svelte/icons/check';
  import { floating } from './floating';
  import { layer, outsideClick } from './overlay';
  import { pop } from './motion';

  type Opt = { value: string; label: string; hint?: string };

  let {
    options = [],
    value = $bindable(''),
    placeholder = 'Select…',
    label = '',
    id,
    disabled = false,
    empty = 'No matches',
    onchange
  }: {
    options: Opt[];
    value?: string;
    placeholder?: string;
    label?: string;
    id?: string;
    disabled?: boolean;
    empty?: string;
    onchange?: (value: string) => void;
  } = $props();

  const uid = `cb-${Math.random().toString(36).slice(2, 9)}`;
  let q = $state('');
  let open = $state(false);
  let active = $state(0);
  let anchor: HTMLDivElement | null = $state(null);
  let input: HTMLInputElement | null = $state(null);
  let list: HTMLDivElement | null = $state(null);

  const selected = $derived(options.find((o) => o.value === value));
  const filtered = $derived(
    options.filter((o) => {
      const s = q.trim().toLowerCase();
      return !s || o.label.toLowerCase().includes(s) || (o.hint ?? '').toLowerCase().includes(s);
    })
  );

  function show() {
    if (disabled) return;
    if (!open) {
      q = '';
      active = Math.max(0, filtered.findIndex((o) => o.value === value));
      open = true;
    }
  }

  function hide() {
    open = false;
    q = '';
  }

  function choose(o: Opt) {
    value = o.value;
    onchange?.(o.value);
    hide();
    input?.focus();
  }

  function scrollActive() {
    queueMicrotask(() => list?.querySelector<HTMLElement>('[data-active="true"]')?.scrollIntoView({ block: 'nearest' }));
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (!open) show();
      else active = Math.min(filtered.length - 1, active + 1);
      scrollActive();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (!open) show();
      else active = Math.max(0, active - 1);
      scrollActive();
    } else if (e.key === 'Enter' && open) {
      e.preventDefault();
      const o = filtered[active];
      if (o) choose(o);
    } else if (e.key === 'Tab') {
      hide();
    }
  }
</script>

<div class="cb" class:open class:disabled bind:this={anchor}>
  <input
    bind:this={input}
    {id}
    class="inp"
    type="text"
    role="combobox"
    autocomplete="off"
    spellcheck="false"
    {disabled}
    aria-label={label || undefined}
    aria-expanded={open}
    aria-controls={`${uid}-list`}
    aria-autocomplete="list"
    aria-activedescendant={open && filtered[active] ? `${uid}-o${active}` : undefined}
    placeholder={open ? (selected?.label ?? placeholder) : placeholder}
    value={open ? q : (selected?.label ?? '')}
    oninput={(e) => {
      q = (e.currentTarget as HTMLInputElement).value;
      active = 0;
      if (!open) open = true;
    }}
    onfocus={show}
    onclick={show}
    onkeydown={onKey}
  />
  <button class="chev" type="button" tabindex="-1" aria-label="Toggle options" {disabled} onclick={() => (open ? hide() : (show(), input?.focus()))}>
    <ChevronDown size={14} />
  </button>
</div>

{#if open}
  <div
    class="list"
    id={`${uid}-list`}
    role="listbox"
    bind:this={list}
    use:floating={{ anchor, matchWidth: true, placement: 'bottom-start', offset: 4 }}
    use:layer={{ onClose: hide, focus: 'none', restoreFocus: false }}
    use:outsideClick={{ onOutside: hide, ignore: () => [anchor] }}
    in:pop={{ duration: 110, y: 3 }}
    out:pop={{ duration: 80, y: 3 }}
  >
    {#each filtered as o, i (o.value)}
      <div
        class="opt"
        id={`${uid}-o${i}`}
        role="option"
        tabindex="-1"
        aria-selected={o.value === value}
        data-active={i === active}
        class:active={i === active}
        onmousedown={(e) => e.preventDefault()}
        onmouseenter={() => (active = i)}
        onclick={() => choose(o)}
        onkeydown={(e) => e.key === 'Enter' && choose(o)}
      >
        <span class="tick">{#if o.value === value}<Check size={14} />{/if}</span>
        <span class="lbl">{o.label}</span>
        {#if o.hint}<span class="hint">{o.hint}</span>{/if}
      </div>
    {:else}
      <div class="none">{empty}</div>
    {/each}
  </div>
{/if}

<style>
  .cb {
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
  .cb:hover:not(.disabled) {
    border-color: var(--border-strong);
  }
  .cb:focus-within,
  .cb.open {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-subtle);
  }
  .cb.disabled {
    opacity: 0.55;
  }
  .inp {
    flex: 1;
    min-width: 0;
    height: 100%;
    padding: 0 4px 0 10px;
    background: transparent;
    border: none;
    outline: none;
    box-shadow: none;
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .inp::placeholder {
    color: var(--text-2);
  }
  .cb.open .inp::placeholder {
    color: var(--text-1);
  }
  .chev {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 100%;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
  }
  .chev:hover {
    background: transparent;
    border: none;
    color: var(--text-0);
  }
  .list {
    max-height: 260px;
    padding: 4px;
    overflow: auto;
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    box-shadow: var(--shadow-pop);
  }
  .opt {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 30px;
    padding: 0 8px 0 4px;
    border-radius: var(--radius-sm);
    color: var(--text-0);
    font-size: var(--fs-sm);
    cursor: pointer;
  }
  .opt.active {
    background: var(--bg-3);
  }
  .tick {
    display: inline-flex;
    width: 16px;
    flex: none;
    justify-content: center;
    color: var(--accent);
  }
  .lbl {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hint {
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .none {
    padding: 12px;
    color: var(--text-2);
    text-align: center;
    font-size: var(--fs-sm);
  }
</style>
