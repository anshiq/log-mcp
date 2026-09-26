<script lang="ts">
  import { toasts } from '../toasts.svelte';
</script>

<div class="region" aria-live="polite">
  {#each toasts.items as t (t.id)}
    <div class="toast {t.tone}">
      <span class="msg">{t.message}</span>
      {#if t.action}
        <button
          class="action"
          onclick={() => {
            t.action?.onClick();
            toasts.dismiss(t.id);
          }}
        >
          {t.action.label}
        </button>
      {/if}
      <button class="close" onclick={() => toasts.dismiss(t.id)} aria-label="Dismiss">✕</button>
    </div>
  {/each}
</div>

<style>
  .region {
    position: fixed;
    bottom: var(--space-6);
    right: var(--space-6);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    z-index: 300;
    max-width: 380px;
  }
  .toast {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    padding: var(--space-3) var(--space-4);
    box-shadow: var(--shadow-pop);
    font-size: var(--fs-sm);
    color: var(--text-0);
    animation: slide-in var(--dur) var(--ease);
  }
  .toast.ok {
    border-left: 3px solid var(--ok);
  }
  .toast.err {
    border-left: 3px solid var(--err);
  }
  .toast.info {
    border-left: 3px solid var(--info);
  }
  .msg {
    flex: 1;
  }
  .action {
    background: transparent;
    border: none;
    color: var(--accent);
    font-size: var(--fs-xs);
    white-space: nowrap;
  }
  .close {
    background: transparent;
    border: none;
    color: var(--text-2);
  }
  @keyframes slide-in {
    from {
      transform: translateY(8px);
      opacity: 0;
    }
    to {
      transform: translateY(0);
      opacity: 1;
    }
  }
</style>
