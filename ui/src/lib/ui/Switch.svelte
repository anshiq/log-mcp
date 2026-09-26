<script lang="ts">
  let {
    checked = $bindable(false),
    label,
    onchange
  }: { checked?: boolean; label?: string; onchange?: (e: Event) => void } = $props();
</script>

<label class="switch">
  <input type="checkbox" bind:checked {onchange} aria-label={label} />
  <span class="track"><span class="thumb"></span></span>
</label>

<style>
  .switch {
    display: inline-flex;
    align-items: center;
    cursor: pointer;
  }
  .switch input {
    position: absolute;
    opacity: 0;
    width: 0;
    height: 0;
  }
  .track {
    width: 34px;
    height: 20px;
    border-radius: 999px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    position: relative;
    transition: background var(--dur-fast) var(--ease);
  }
  .thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--text-1);
    transition: transform var(--dur-fast) var(--ease), background var(--dur-fast) var(--ease);
  }
  input:checked + .track {
    background: var(--accent);
    border-color: var(--accent);
  }
  input:checked + .track .thumb {
    transform: translateX(14px);
    background: #fff;
  }
  input:focus-visible + .track {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
</style>
