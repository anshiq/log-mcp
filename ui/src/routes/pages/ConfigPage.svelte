<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte';
  import FolderOpen from '@lucide/svelte/icons/folder-open';
  import Lock from '@lucide/svelte/icons/lock';
  import FileCog from '@lucide/svelte/icons/file-cog';
  import Layers from '@lucide/svelte/icons/layers';
  import Users from '@lucide/svelte/icons/users';
  import GitBranch from '@lucide/svelte/icons/git-branch';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import ShieldAlert from '@lucide/svelte/icons/shield-alert';
  import X from '@lucide/svelte/icons/x';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import WorkspacePicker from '../../components/WorkspacePicker.svelte';
  import ChoiceModal from '../../components/config/ChoiceModal.svelte';
  import { router } from '../../lib/router.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { ConfigSession, type LayerName } from '../../lib/config/session.svelte';

  const session = new ConfigSession();
  const onRevisions = $derived(router.match('/config/revisions') !== null);
  const wsId = $derived(scopeState.workspaceId);
  let internalNav = false;
  let offGuard: (() => void) | null = null;

  const layerMeta: { name: LayerName; label: string; hint: string }[] = [
    { name: 'project', label: 'Project', hint: 'Stored by agent-runtime for this project. Editable.' },
    { name: 'workspace', label: 'Workspace', hint: 'Overrides for this workspace folder only. Editable.' },
    { name: 'repo', label: 'Repo', hint: 'agent-runtime.yaml checked into the repository. Read-only here.' }
  ];

  const untrusted = $derived(session.warnings.some((w) => /untrusted/i.test(w)));
  const otherWarnings = $derived(session.warnings.filter((w) => !/untrusted/i.test(w)));

  function go(path: string) {
    internalNav = true;
    void router.navigate(path).finally(() => {
      internalNav = false;
    });
  }

  function beforeUnload(e: BeforeUnloadEvent) {
    if (session.anyDirty) {
      e.preventDefault();
      e.returnValue = '';
    }
  }

  onMount(() => {
    offGuard = router.onBeforeLeave(async () => {
      if (internalNav || !session.anyDirty) return true;
      const names = session.dirtyLayers.join(', ');
      return session.confirm('Leave without applying?', `You have unapplied edits in the ${names} layer. They will be lost if you leave this page.`, 'Discard and leave', 'danger');
    });
    window.addEventListener('beforeunload', beforeUnload);
  });

  onDestroy(() => {
    offGuard?.();
    window.removeEventListener('beforeunload', beforeUnload);
    session.destroy();
  });

  $effect(() => {
    const id = wsId;
    untrack(() => {
      if (!id) {
        session.reset();
        return;
      }
      if (id === session.workspaceId) return;
      void switchTo(id);
    });
  });

  async function switchTo(id: string) {
    const prev = session.workspaceId;
    if (prev && session.anyDirty) {
      const ok = await session.confirm('Discard unsaved changes?', `Switching workspace drops the unapplied edits in the ${session.dirtyLayers.join(', ')} layer.`, 'Discard and switch', 'danger');
      if (!ok) {
        scopeState.select(prev);
        return;
      }
    }
    await session.load(id);
  }

  function layerState(name: LayerName): string {
    const l = session.layers.find((x) => x.name === name);
    if (!l) return '';
    const parts = [l.path || 'no file yet', l.exists ? 'exists' : 'not created yet', l.writable ? 'writable' : 'read-only'];
    return parts.join(' · ');
  }
</script>

<div class="page fill">
  {#if scopeState.all}
    <div class="center">
      <WorkspacePicker
        icon={FileCog}
        title="Pick a workspace to configure"
        body="Config is layered per workspace. Choose one to edit its agent-runtime.yaml, preview the impact on running apps and roll back revisions."
      />
    </div>
  {:else}
    <div class="page-header">
      <div class="titles">
        <h1 class="title">
          <span class="truncate">{scopeState.label}</span>
          {#if session.loaded}
            <span class="badge accent" title="Latest project-layer revision">{session.revision > 0 ? `revision ${session.revision}` : 'no revisions'}</span>
          {/if}
          {#if session.anyDirty}
            <span class="badge warn"><span class="dot warn"></span>unsaved</span>
          {/if}
        </h1>
        <span class="subtitle path mono" title={scopeState.path}><FolderOpen size={13} /><span class="rtl"><bdi>{scopeState.path}</bdi></span></span>
      </div>
      <div class="actions">
        <div class="layers">
          <span class="cap"><Layers size={13} />Layer</span>
          <div class="seg" role="radiogroup" aria-label="Config layer">
            {#each layerMeta as l (l.name)}
              {@const info = session.layers.find((x) => x.name === l.name)}
              <button
                role="radio"
                aria-checked={session.layer === l.name}
                class:active={session.layer === l.name}
                title={`${l.hint}\n${layerState(l.name)}`}
                disabled={!session.loaded || onRevisions}
                onclick={() => session.setLayer(l.name)}
              >
                {#if l.name === 'project'}<GitBranch size={13} />{:else if l.name === 'workspace'}<Users size={13} />{:else}<Lock size={13} />{/if}
                {l.label}
                <span class="state" class:exists={info?.exists} title={info?.exists ? 'file exists' : 'file not created yet'}></span>
                {#if session.isDirty(l.name)}<span class="dot warn" title="unsaved edits"></span>{/if}
              </button>
            {/each}
          </div>
        </div>
      </div>
    </div>

    {#if session.external}
      <div class="banner warn" role="alert">
        <TriangleAlert size={16} />
        <span class="grow">
          Config changed elsewhere: revision <span class="mono">{session.external.revision}</span>
          {#if session.external.layer}in the <strong>{session.external.layer}</strong> layer{/if}
          {#if session.external.source}via {session.external.source}{/if}{#if session.external.message}, “{session.external.message}”{/if}.
          {#if session.external.invalid}It failed validation.{/if}
        </span>
        <button class="btn sm" onclick={() => void session.reloadLatest()}><RefreshCw size={13} />Reload latest</button>
        <button class="btn sm icon ghost" aria-label="Dismiss" onclick={() => session.dismissExternal()}><X size={14} /></button>
      </div>
    {/if}
    {#if session.stale && !session.external}
      <div class="banner warn" role="alert">
        <TriangleAlert size={16} />
        <span class="grow">
          Your copy is based on revision <span class="mono">{session.stale.base}</span> but the latest is <span class="mono">{session.stale.latest}</span>. Reload before applying so you do not overwrite newer changes.
        </span>
        <button class="btn sm" onclick={() => void session.reloadLatest()}><RefreshCw size={13} />Reload latest</button>
      </div>
    {/if}
    {#if session.loadError}
      <div class="banner err" role="alert">
        <TriangleAlert size={16} />
        <span class="grow">{session.loadError}</span>
        <button class="btn sm" onclick={() => void session.load(wsId)}><RefreshCw size={13} />Retry</button>
      </div>
    {/if}
    {#if untrusted}
      <div class="banner info">
        <ShieldAlert size={16} />
        <span class="grow">The repo config in this workspace is not trusted yet, so its apps are ignored until you trust it.</span>
        <button class="btn sm" onclick={() => void session.trustRepo()}>Trust repo config</button>
      </div>
    {/if}
    {#each otherWarnings as w}
      <div class="banner warn"><TriangleAlert size={16} /><span class="grow">{w}</span></div>
    {/each}

    <div class="tabs" role="tablist" aria-label="Config sections">
      <button role="tab" aria-selected={!onRevisions} class:active={!onRevisions} onclick={() => go('/config')}>Editor</button>
      <button role="tab" aria-selected={onRevisions} class:active={onRevisions} onclick={() => go('/config/revisions')}>
        Revisions
        {#if session.revisions.length > 0}<span class="count">{session.revisions.length}</span>{/if}
      </button>
    </div>

    <div class="content">
      {#if !session.loaded && (session.loading || !session.loadError)}
        <div class="loading"><Spinner size={18} /> Loading config…</div>
      {:else if session.loaded}
        {#if onRevisions}
          {#await import('./RevisionsPanel.svelte')}
            <div class="loading"><Spinner size={18} /></div>
          {:then { default: RevisionsPanel }}
            <RevisionsPanel {session} onOpenEditor={() => go('/config')} />
          {/await}
        {:else}
          {#await import('../../components/ConfigEditor.svelte')}
            <div class="loading"><Spinner size={18} /> Loading editor…</div>
          {:then { default: ConfigEditor }}
            <ConfigEditor {session} />
          {/await}
        {/if}
      {/if}
    </div>
  {/if}
</div>

{#if session.choice}
  <ChoiceModal request={session.choice} onAnswer={(id) => session.answer(id)} />
{/if}

<style>
  .page {
    gap: var(--space-4);
  }
  .center {
    flex: 1;
    display: flex;
    overflow: auto;
  }
  .page-header {
    flex: none;
    flex-wrap: nowrap;
    align-items: center;
  }
  .titles {
    min-width: 0;
  }
  .title {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }
  .path {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .rtl {
    direction: rtl;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .actions {
    flex: none;
  }
  .layers {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .cap {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-2);
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .seg > button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-sm);
    font-weight: 500;
  }
  .seg > button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .state {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    border: 1.5px solid var(--text-2);
    flex: none;
  }
  .state.exists {
    background: var(--ok);
    border-color: var(--ok);
  }
  .banner {
    flex: none;
  }
  .banner :global(svg) {
    flex: none;
  }
  .banner .grow {
    flex: 1;
    min-width: 0;
  }
  .tabs {
    flex: none;
    margin-top: calc(var(--space-2) * -1);
  }
  .tabs > button {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    font-weight: 500;
  }
  .count {
    font-size: var(--fs-xs);
    padding: 0 6px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-1);
    font-variant-numeric: tabular-nums;
  }
  .content {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .content > :global(*) {
    flex: 1;
    min-height: 0;
  }
  .loading {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    color: var(--text-2);
    font-size: var(--fs-sm);
    padding: var(--space-8);
  }
  @media (max-width: 900px) {
    .page-header {
      flex-wrap: wrap;
    }
    .cap {
      display: none;
    }
  }
</style>
