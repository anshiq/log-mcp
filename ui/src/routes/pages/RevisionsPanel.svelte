<script lang="ts">
  import History from '@lucide/svelte/icons/history';
  import Undo2 from '@lucide/svelte/icons/undo-2';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import Bot from '@lucide/svelte/icons/bot';
  import Monitor from '@lucide/svelte/icons/monitor';
  import Terminal from '@lucide/svelte/icons/terminal';
  import Zap from '@lucide/svelte/icons/zap';
  import Sparkles from '@lucide/svelte/icons/sparkles';
  import GitCompare from '@lucide/svelte/icons/git-compare';
  import Download from '@lucide/svelte/icons/download';
  import Copy from '@lucide/svelte/icons/copy';
  import FileCode from '@lucide/svelte/icons/file-code';
  import Pencil from '@lucide/svelte/icons/pencil';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import RelativeTime from '../../lib/ui/RelativeTime.svelte';
  import DiffView from '../../lib/ui/DiffView.svelte';
  import { ConfigService } from '../../lib/api';
  import type { Revision } from '../../lib/api/types';
  import type { ConfigSession } from '../../lib/config/session.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';

  let { session, onOpenEditor }: { session: ConfigSession; onOpenEditor?: () => void } = $props();

  let selectedId = $state<number | null>(null);
  let content = $state('');
  let previous = $state('');
  let previousId = $state<number | null>(null);
  let detailLoading = $state(false);
  let view = $state<'diff' | 'content'>('diff');
  let rollingBack = $state(false);
  let token = 0;

  const revisions = $derived(session.revisions);
  const selected = $derived(revisions.find((r) => r.id === selectedId) ?? null);
  const isCurrent = $derived(selected ? session.latestRevisionFor(selected.layer)?.id === selected.id : false);
  const canRollback = $derived(!!selected && selected.layer === 'project' && !isCurrent && selected.valid);
  const lines = $derived(content === '' ? [] : content.replace(/\n$/, '').split('\n'));

  $effect(() => {
    const list = revisions;
    if (list.length === 0) {
      selectedId = null;
      return;
    }
    if (selectedId === null || !list.some((r) => r.id === selectedId)) {
      selectedId = list[0]?.id ?? null;
    }
  });

  $effect(() => {
    const rev = selected;
    if (!rev) return;
    void loadDetail(rev);
  });

  async function loadDetail(rev: Revision) {
    const mine = ++token;
    detailLoading = true;
    try {
      const prev = session.revisions.filter((r) => r.layer === rev.layer && r.id < rev.id).sort((a, b) => b.id - a.id)[0];
      const [cur, prevRes] = await Promise.all([
        ConfigService.revision(session.projectId, rev.id),
        prev ? ConfigService.revision(session.projectId, prev.id) : Promise.resolve(null)
      ]);
      if (mine !== token) return;
      content = cur.content ?? '';
      previous = prevRes?.content ?? '';
      previousId = prev?.id ?? null;
    } catch (err) {
      if (mine === token) toastError(err);
    } finally {
      if (mine === token) detailLoading = false;
    }
  }

  async function rollback() {
    if (!selected) return;
    rollingBack = true;
    try {
      await session.rollback(selected);
    } finally {
      rollingBack = false;
    }
  }

  async function copySha(sha: string) {
    try {
      await navigator.clipboard.writeText(sha);
      toasts.ok('Checksum copied');
    } catch {
      toasts.err('Could not copy to the clipboard');
    }
  }

  function exact(ts: number): string {
    return ts ? new Date(ts).toLocaleString() : 'unknown';
  }

  function shortSession(id: string): string {
    return id.length > 14 ? `${id.slice(0, 12)}…` : id;
  }
</script>

<div class="revs">
  <section class="list" aria-label="Revision history">
    <header>
      <History size={15} />
      <h3>History</h3>
      <span class="count">{revisions.length}</span>
      {#if session.revisionsLoading}<Spinner size={13} />{/if}
    </header>
    <div class="scroll">
      {#if session.revisionsLoading && revisions.length === 0}
        <ul class="skeleton" aria-busy="true">
          {#each Array.from({ length: 5 }) as _, i (i)}
            <li><span class="sk a"></span><span class="sk b"></span><span class="sk c"></span></li>
          {/each}
        </ul>
      {:else if revisions.length === 0}
        <div class="empty-state">
          <div class="icon-wrap"><History size={20} /></div>
          <h3>No revisions yet</h3>
          <p>Every applied config change is recorded here so you can review and roll it back.</p>
          {#if onOpenEditor}<button class="btn sm" onclick={onOpenEditor}><Pencil size={13} />Open the editor</button>{/if}
        </div>
      {:else}
        <ol class="timeline">
          {#each revisions as rev (rev.id)}
            {@const current = session.latestRevisionFor(rev.layer)?.id === rev.id}
            <li class:active={rev.id === selectedId} class:invalid={!rev.valid}>
              <button onclick={() => (selectedId = rev.id)} aria-current={rev.id === selectedId}>
                <span class="node" class:bad={!rev.valid}></span>
                <span class="top">
                  <span class="id mono">#{rev.id}</span>
                  <span class="badge">{rev.layer || 'project'}</span>
                  {#if current}<span class="badge accent">current</span>{/if}
                  {#if !rev.valid}<span class="badge err">invalid</span>{/if}
                  <span class="when"><RelativeTime ts={rev.time} /></span>
                </span>
                <span class="msg truncate">{rev.message || 'No message'}</span>
                <span class="src">
                  {#if rev.source === 'rollback'}<Undo2 size={11} />{:else if rev.source === 'gui'}<Monitor size={11} />{:else if rev.source === 'api'}<Terminal size={11} />{:else if rev.source === 'auto'}<Zap size={11} />{:else if rev.source === 'learned'}<Sparkles size={11} />{:else if rev.source === 'proposal'}<GitCompare size={11} />{:else if rev.source === 'import'}<Download size={11} />{:else}<Bot size={11} />{/if}
                  {rev.source || 'unknown'}
                  {#if rev.session}<span class="mono sess">{shortSession(rev.session)}</span>{/if}
                </span>
              </button>
            </li>
          {/each}
        </ol>
      {/if}
    </div>
  </section>

  <section class="detail" aria-label="Revision detail">
    {#if selected}
      <header class="head">
        <div class="ttl">
          <h2>Revision <span class="mono">#{selected.id}</span></h2>
          {#if selected.valid}
            <span class="badge ok"><CircleCheck size={11} />valid</span>
          {:else}
            <span class="badge err"><CircleAlert size={11} />invalid</span>
          {/if}
          <span class="badge">{selected.layer || 'project'} layer</span>
          {#if isCurrent}<span class="badge accent">current</span>{/if}
        </div>
        <div class="acts">
          <button
            class="btn danger"
            disabled={!canRollback || rollingBack}
            onclick={rollback}
            title={isCurrent ? 'This is already the current revision' : selected.layer !== 'project' ? 'Rollback is available for project-layer revisions' : !selected.valid ? 'Invalid revisions cannot be restored' : 'Restore this revision as a new one'}
          >
            {#if rollingBack}<Spinner size={14} />{:else}<Undo2 size={14} />{/if}
            Roll back to this
          </button>
        </div>
      </header>

      <dl class="meta">
        <div>
          <dt>Applied</dt>
          <dd title={exact(selected.time)}>{exact(selected.time)} <span class="muted">(<RelativeTime ts={selected.time} />)</span></dd>
        </div>
        <div>
          <dt>Source</dt>
          <dd>{selected.source || 'unknown'}</dd>
        </div>
        <div>
          <dt>Session</dt>
          <dd class="mono" title={selected.session}>{selected.session || 'none'}</dd>
        </div>
        <div>
          <dt>Message</dt>
          <dd>{selected.message || 'No message'}</dd>
        </div>
        {#if selected.sha}
          <div>
            <dt>Checksum</dt>
            <dd>
              <button class="sha mono" onclick={() => copySha(selected.sha)} title="Copy full checksum">{selected.sha.slice(0, 12)}<Copy size={11} /></button>
            </dd>
          </div>
        {/if}
      </dl>

      <div class="view-bar">
        <div class="seg" role="radiogroup" aria-label="Revision view">
          <button role="radio" aria-checked={view === 'diff'} class:active={view === 'diff'} onclick={() => (view = 'diff')}>Changes</button>
          <button role="radio" aria-checked={view === 'content'} class:active={view === 'content'} onclick={() => (view = 'content')}>Full file</button>
        </div>
        <span class="muted note">
          {#if previousId !== null}vs revision #{previousId}{:else}first revision of this layer{/if}
        </span>
      </div>

      <div class="body">
        {#if detailLoading}
          <div class="loading"><Spinner size={16} /> Loading revision…</div>
        {:else if view === 'diff'}
          <DiffView old={previous} next={content} context={5} emptyText="This revision is identical to the previous one" />
        {:else}
          <div class="file">
            <div class="file-head"><FileCode size={13} />{lines.length} line{lines.length === 1 ? '' : 's'}</div>
            <pre class="code">{#each lines as l, i (i)}<span class="ln">{i + 1}</span>{l}
{/each}</pre>
          </div>
        {/if}
      </div>
    {:else if revisions.length > 0}
      <div class="empty-state"><p>Select a revision to inspect it.</p></div>
    {:else}
      <div class="empty-state">
        <div class="icon-wrap"><History size={20} /></div>
        <h3>Nothing to show yet</h3>
        <p>Apply a config from the editor and its revision will appear here with a diff against the previous one.</p>
      </div>
    {/if}
  </section>
</div>

<style>
  .revs {
    display: grid;
    grid-template-columns: minmax(280px, 360px) 1fr;
    gap: var(--space-4);
    min-height: 0;
    height: 100%;
    container-type: inline-size;
  }
  section {
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    display: flex;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
    overflow: hidden;
  }
  .list > header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 0 var(--space-4);
    height: 44px;
    flex: none;
    border-bottom: 1px solid var(--border);
    color: var(--text-1);
  }
  .list h3 {
    font-size: var(--fs-md);
    color: var(--text-0);
  }
  .count {
    font-size: var(--fs-xs);
    padding: 0 7px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-1);
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow: auto;
  }
  .timeline {
    list-style: none;
    margin: 0;
    padding: var(--space-3) 0;
    position: relative;
  }
  .timeline::before {
    content: '';
    position: absolute;
    left: 22px;
    top: 24px;
    bottom: 24px;
    width: 1px;
    background: var(--border-strong);
  }
  .timeline li button {
    position: relative;
    display: grid;
    grid-template-columns: 1fr;
    gap: 3px;
    width: 100%;
    padding: 9px var(--space-4) 9px 42px;
    text-align: left;
    border: none;
    border-radius: 0;
    background: transparent;
    border-left: 2px solid transparent;
    color: var(--text-1);
  }
  .timeline li button:hover {
    background: var(--bg-hover);
  }
  .timeline li.active button {
    background: var(--accent-subtle);
    border-left-color: var(--accent);
  }
  .node {
    position: absolute;
    left: 17px;
    top: 15px;
    width: 11px;
    height: 11px;
    border-radius: 50%;
    background: var(--bg-1);
    border: 2px solid var(--accent);
  }
  .node.bad {
    border-color: var(--err);
  }
  .timeline li.active .node {
    background: var(--accent);
  }
  .top {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }
  .id {
    font-weight: 600;
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .when {
    margin-left: auto;
    color: var(--text-2);
    font-size: var(--fs-xs);
    white-space: nowrap;
    padding-left: var(--space-3);
  }
  .msg {
    font-size: var(--fs-sm);
    color: var(--text-0);
  }
  .src {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .sess {
    opacity: 0.8;
  }
  .badge {
    font-size: 10.5px;
    padding: 0 7px;
  }
  .skeleton {
    list-style: none;
    margin: 0;
    padding: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .skeleton li {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .sk {
    height: 10px;
    border-radius: 4px;
    background: linear-gradient(90deg, var(--bg-3), var(--bg-hover), var(--bg-3));
    background-size: 200% 100%;
    animation: shimmer 1.4s linear infinite;
  }
  .sk.a {
    width: 45%;
  }
  .sk.b {
    width: 80%;
  }
  .sk.c {
    width: 30%;
  }
  @keyframes shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .detail {
    overflow: hidden;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-5);
    border-bottom: 1px solid var(--border);
    flex-wrap: wrap;
    flex: none;
  }
  .ttl {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  h2 {
    font-size: var(--fs-lg);
  }
  .meta {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
    gap: var(--space-4) var(--space-5);
    margin: 0;
    padding: var(--space-4) var(--space-5);
    border-bottom: 1px solid var(--border);
    flex: none;
  }
  .meta dt {
    font-size: var(--fs-xs);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-weight: 600;
    color: var(--text-2);
    margin-bottom: 3px;
  }
  .meta dd {
    margin: 0;
    font-size: var(--fs-sm);
    color: var(--text-0);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sha {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0 6px;
    border: none;
    background: var(--bg-3);
    color: var(--text-1);
    font-size: var(--fs-xs);
    line-height: 1.8;
  }
  .view-bar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-3) var(--space-5);
    flex: none;
  }
  .note {
    font-size: var(--fs-xs);
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0 var(--space-5) var(--space-5);
    display: flex;
    flex-direction: column;
  }
  .body > :global(.diff) {
    flex: none;
  }
  .loading {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    justify-content: center;
    padding: var(--space-8);
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .file {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-1);
    overflow: hidden;
  }
  .file-head {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px var(--space-4);
    background: var(--bg-2);
    border-bottom: 1px solid var(--border);
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .code {
    margin: 0;
    padding: var(--space-3) 0;
    overflow: auto;
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
    line-height: 20px;
    color: var(--text-0);
  }
  .ln {
    display: inline-block;
    width: 44px;
    padding-right: var(--space-4);
    margin-right: var(--space-3);
    text-align: right;
    color: var(--text-2);
    user-select: none;
  }
  .empty-state {
    margin: auto;
  }
  @container (max-width: 820px) {
    .revs {
      grid-template-columns: 1fr;
      grid-template-rows: minmax(150px, 34%) 1fr;
    }
  }
</style>
