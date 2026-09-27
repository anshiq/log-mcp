<script lang="ts">
  import { onMount } from 'svelte';
  import { ConfigService } from '../../lib/api';

  let { projectId }: { projectId: string } = $props();

  let revisions = $state<Record<string, unknown>[]>([]);
  let selected = $state<Record<string, unknown> | null>(null);
  let content = $state('');
  let loading = $state(false);
  let rollingBack = $state(false);

  async function load() {
    loading = true;
    try {
      const res = (await ConfigService.revisions(projectId, 50)) as unknown;
      revisions = (Array.isArray(res) ? res : (res as { revisions?: Record<string, unknown>[] }).revisions ?? []) as Record<string, unknown>[];
    } finally {
      loading = false;
    }
  }

  async function open(rev: Record<string, unknown>) {
    selected = rev;
    const res = await ConfigService.revision(projectId, Number(rev.id));
    content = res.content ?? '';
  }

  async function rollback(rev: Record<string, unknown>) {
    if (!confirm(`Roll back to revision ${rev.id}? This creates a new revision with that content.`)) return;
    rollingBack = true;
    try {
      await ConfigService.rollback(projectId, Number(rev.id));
      await load();
    } finally {
      rollingBack = false;
    }
  }

  onMount(load);
  $effect(() => {
    void projectId;
    void load();
  });
</script>

<div class="revisions">
  <div class="list">
    {#if loading}
      <p class="muted">Loading…</p>
    {:else if revisions.length === 0}
      <p class="muted">No revisions yet.</p>
    {:else}
      <ul>
        {#each revisions as rev}
          <li class:active={selected?.id === rev.id}>
            <button class="row" onclick={() => open(rev)}>
              <span class="id">#{rev.id}</span>
              <span class="badge">{rev.source}</span>
              <span class="msg">{rev.message || '—'}</span>
            </button>
            <button class="rollback" onclick={() => rollback(rev)} disabled={rollingBack}>Rollback</button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
  <div class="content">
    {#if selected}
      <h3>Revision #{selected.id}</h3>
      <pre>{content}</pre>
    {:else}
      <p class="muted">Select a revision to view its content.</p>
    {/if}
  </div>
</div>

<style>
  .revisions {
    display: flex;
    height: 100%;
    overflow: hidden;
  }
  .list {
    width: 320px;
    flex: none;
    border-right: 1px solid var(--border);
    overflow: auto;
    padding: var(--space-4);
  }
  .list ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .list li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--border);
  }
  .list li.active .row {
    color: var(--accent);
  }
  .row {
    flex: 1;
    display: flex;
    gap: var(--space-2);
    align-items: center;
    background: transparent;
    border: none;
    text-align: left;
    color: var(--text-0);
    font-size: var(--fs-sm);
    min-width: 0;
  }
  .id {
    font-family: var(--font-mono);
    color: var(--text-2);
  }
  .badge {
    background: var(--bg-3);
    border-radius: var(--radius-sm);
    padding: 0 var(--space-2);
    font-size: var(--fs-xs);
    color: var(--text-1);
  }
  .msg {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rollback {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-2);
    font-size: var(--fs-xs);
    color: var(--text-0);
  }
  .content {
    flex: 1;
    overflow: auto;
    padding: var(--space-5);
  }
  .content pre {
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
    white-space: pre-wrap;
  }
  .muted {
    color: var(--text-2);
  }
</style>
