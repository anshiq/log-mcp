<script lang="ts">
  import type { Snippet } from 'svelte';

  let { title, body, action }: { title: string; body?: string; action?: Snippet } = $props();
</script>

<div class="empty">
  <p class="title">{title}</p>
  {#if body}<p class="body">{body}</p>{/if}
  {#if action}<div class="action">{@render action()}</div>{/if}
</div>

<style>
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-8);
    text-align: center;
  }
  .title {
    color: var(--text-1);
    font-size: var(--fs-md);
    margin: 0;
  }
  .body {
    color: var(--text-2);
    font-size: var(--fs-sm);
    margin: 0;
    max-width: 360px;
  }
</style>
