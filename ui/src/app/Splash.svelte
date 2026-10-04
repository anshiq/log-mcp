<script lang="ts">
  import Activity from '@lucide/svelte/icons/activity';
  import Spinner from '../lib/ui/Spinner.svelte';
</script>

<div class="splash" role="status" aria-live="polite">
  <span class="logo"><Activity size={26} strokeWidth={2.4} /></span>
  <span class="name">agent-runtime</span>
  <span class="hint"><Spinner size={14} />Connecting to daemon…</span>
</div>

<style>
  .splash {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100vh;
    gap: var(--space-4);
    background: var(--bg-0);
    animation: appear 400ms var(--ease) both;
  }
  .logo {
    display: grid;
    place-items: center;
    width: 56px;
    height: 56px;
    border-radius: 4px;
    background: var(--accent);
    color: #fff;
    box-shadow:
      0 0 0 6px var(--accent-subtle),
      var(--shadow-1);
  }
  .name {
    font-family: var(--font-display);
    font-size: 30px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.28em;
    text-indent: 0.28em;
  }
  .hint {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  @keyframes appear {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
</style>
