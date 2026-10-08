<script lang="ts">
  import { onMount } from 'svelte';
  import FolderKanban from '@lucide/svelte/icons/folder-kanban';
  import FolderPlus from '@lucide/svelte/icons/folder-plus';
  import FolderOpen from '@lucide/svelte/icons/folder-open';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import Pencil from '@lucide/svelte/icons/pencil';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Check from '@lucide/svelte/icons/check';
  import X from '@lucide/svelte/icons/x';
  import Link from '@lucide/svelte/icons/link';
  import Eraser from '@lucide/svelte/icons/eraser';
  import Copy from '@lucide/svelte/icons/copy';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import { ProjectService, ConfigService } from '../../lib/api';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { palette } from '../../lib/palette.svelte';
  import { dialogs } from '../../lib/state/dialogs.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import { getPlatform } from '../../lib/platform';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import SearchField from '../../lib/ui/SearchField.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Banner from '../../lib/ui/Banner.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import RelativeTime from '../../lib/ui/RelativeTime.svelte';

  interface ProjectRow {
    id: string;
    name: string;
    configMode: string;
    lastUsedAt: number;
    workspaceCount: number;
  }
  interface WorkspaceRow {
    id: string;
    projectId: string;
    path: string;
    confirmed: boolean;
    lastSeenAt: number;
    missing: boolean;
  }

  function ms(n: unknown): number {
    const v = typeof n === 'number' ? n : Number(n ?? 0);
    if (!v) return 0;
    return v < 1e12 ? v * 1000 : v;
  }

  let projects = $state<ProjectRow[]>([]);
  let workspaces = $state<Record<string, WorkspaceRow[]>>({});
  let expanded = $state<Record<string, boolean>>({});
  let loading = $state(true);
  let loadError = $state('');
  let filter = $state('');
  let gcRunning = $state(false);
  let missingIds = $state<Set<string>>(new Set());

  let openDialog = $state(false);
  let openPath = $state('');
  let openError = $state('');
  let openBusy = $state(false);
  let linkTarget = $state<ProjectRow | null>(null);

  let renamingId = $state('');
  let renameValue = $state('');
  let renameBusy = $state(false);


  const visible = $derived.by(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return projects;
    return projects.filter((p) => p.name.toLowerCase().includes(q) || (workspaces[p.id] ?? []).some((w) => w.path.toLowerCase().includes(q)));
  });

  const pathInvalid = $derived(openPath.trim() !== '' && !/^(\/|~|[A-Za-z]:[\\/]|\\\\)/.test(openPath.trim()));

  function baseName(path: string): string {
    return path.split(/[\\/]/).filter(Boolean).pop() ?? path;
  }

  async function load(initial = false) {
    if (initial) loading = true;
    loadError = '';
    try {
      const list = (await ProjectService.list()) as Record<string, unknown>[];
      const rows: ProjectRow[] = list.map((p) => ({
        id: String(p['id'] ?? ''),
        name: String(p['name'] ?? p['id'] ?? ''),
        configMode: String(p['configMode'] ?? ''),
        lastUsedAt: ms(p['lastUsedAt']),
        workspaceCount: Number(p['workspaceCount'] ?? 0)
      }));
      rows.sort((a, b) => b.lastUsedAt - a.lastUsedAt);
      const entries = await Promise.all(
        rows.map(async (p) => {
          try {
            const raw = (await ProjectService.workspaces(p.id)) as Record<string, unknown>[];
            return [
              p.id,
              raw.map<WorkspaceRow>((w) => ({
                id: String(w['id'] ?? ''),
                projectId: p.id,
                path: String(w['path'] ?? ''),
                confirmed: w['confirmed'] !== false,
                lastSeenAt: ms(w['lastSeenAt']),
                missing: w['missing'] === true
              }))
            ] as const;
          } catch {
            return [p.id, [] as WorkspaceRow[]] as const;
          }
        })
      );
      projects = rows;
      workspaces = Object.fromEntries(entries);
      if (initial && rows.length <= 3) expanded = Object.fromEntries(rows.map((p) => [p.id, true]));
    } catch (err) {
      loadError = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  async function refreshAll() {
    await Promise.all([load(), scopeState.refresh()]);
  }

  function toggle(id: string) {
    expanded = { ...expanded, [id]: !expanded[id] };
  }

  function openOpenDialog() {
    openPath = '';
    openError = '';
    linkTarget = null;
    scopeState.error = '';
    openDialog = true;
  }

  function openLinkDialog(p: ProjectRow) {
    openPath = '';
    openError = '';
    linkTarget = p;
    openDialog = true;
  }

  async function submitPath(e: Event) {
    e.preventDefault();
    const path = openPath.trim();
    if (!path || openBusy) return;
    openBusy = true;
    openError = '';
    try {
      if (linkTarget) {
        const target = linkTarget;
        await ProjectService.linkWorkspace(target.id, path);
        toasts.ok(`Linked ${baseName(path)} to ${target.name}`);
        expanded = { ...expanded, [target.id]: true };
        openDialog = false;
        await refreshAll();
      } else {
        const ok = await scopeState.open(path);
        if (ok) {
          toasts.ok(`Opened ${scopeState.label}`);
          openDialog = false;
          await load();
        } else {
          openError = scopeState.error || 'Could not open that folder.';
        }
      }
    } catch (err) {
      openError = err instanceof Error ? err.message : String(err);
    } finally {
      openBusy = false;
    }
  }

  function startRename(p: ProjectRow) {
    renamingId = p.id;
    renameValue = p.name;
  }

  function cancelRename() {
    renamingId = '';
    renameValue = '';
  }

  async function commitRename(p: ProjectRow) {
    const name = renameValue.trim();
    if (!name || name === p.name) {
      cancelRename();
      return;
    }
    renameBusy = true;
    try {
      await ProjectService.update(p.id, name);
      toasts.ok(`Renamed to ${name}`);
      cancelRename();
      await refreshAll();
    } catch (err) {
      toastError(err);
    } finally {
      renameBusy = false;
    }
  }

  async function forget(p: ProjectRow) {
    const ok = await dialogs.confirm(`“${p.name}” and its workspaces move to a 30-day trash. Your files on disk are not touched.`, {
      title: 'Forget project',
      confirmLabel: 'Forget project',
      danger: true
    });
    if (!ok) return;
    try {
      await ProjectService.forget(p.id);
      toasts.ok(`Forgot ${p.name}`);
      await refreshAll();
    } catch (err) {
      toastError(err);
    }
  }

  async function gc() {
    gcRunning = true;
    try {
      const res = await ProjectService.gc();
      const removed = res.removedWorkspaces ?? [];
      missingIds = new Set([...missingIds, ...removed]);
      if (removed.length === 0) toasts.info('No missing workspaces found');
      else toasts.ok(`Marked ${removed.length} missing workspace${removed.length === 1 ? '' : 's'} as unused`);
      await refreshAll();
    } catch (err) {
      toastError(err);
    } finally {
      gcRunning = false;
    }
  }

  function useWorkspace(w: WorkspaceRow) {
    scopeState.select(w.id);
    toasts.ok(`Scope set to ${scopeState.workspaceLabel(w.id)}`);
  }

  async function copyPath(path: string) {
    await getPlatform().copyText(path);
    toasts.info('Path copied');
  }

  $effect(() => {
    if (palette.wantsOpenWorkspace) {
      palette.wantsOpenWorkspace = false;
      openOpenDialog();
    }
  });

  onMount(() => void load(true));
</script>

<div class="page">
  <div class="page-header">
    <div class="titles">
      <h1>Projects</h1>
      <p class="subtitle">Every project the daemon knows about, and the workspace folders linked to it.</p>
    </div>
    <div class="actions">
      <Button variant="secondary" loading={gcRunning} onclick={() => void gc()} title="Flag workspaces whose folder no longer exists">
        {#if !gcRunning}<Eraser size={14} />{/if}Clean up missing
      </Button>
      <Button variant="primary" onclick={openOpenDialog}><FolderPlus size={14} />Open workspace</Button>
    </div>
  </div>

  {#if loadError}
    <Banner tone="err" title="Couldn’t load projects" message={loadError}>
      {#snippet actions()}<Button size="sm" onclick={() => void load(true)}>Retry</Button>{/snippet}
    </Banner>
  {/if}

  {#if loading}
    <div class="list">
      {#each [0, 1, 2] as i (i)}
        <div class="card skel">
          <Skeleton width="28px" height="28px" radius="8px" />
          <div class="skel-text">
            <Skeleton width={`${180 + i * 30}px`} height="14px" />
            <Skeleton width="260px" height="11px" />
          </div>
        </div>
      {/each}
    </div>
  {:else if projects.length === 0 && !loadError}
    <div class="card">
      <EmptyState icon={FolderKanban} title="No projects yet" body="Open a project folder to register it. The daemon creates a project and workspace for it, and your processes, logs and config are grouped under it.">
        {#snippet action()}
          <Button variant="primary" onclick={openOpenDialog}><FolderPlus size={14} />Open workspace</Button>
        {/snippet}
      </EmptyState>
    </div>
  {:else if projects.length > 0}
    {#if projects.length > 3}
      <div class="toolbar">
        <div class="search"><SearchField bind:value={filter} placeholder="Filter projects or paths…" label="Filter projects" /></div>
        <span class="muted count">{visible.length} of {projects.length}</span>
      </div>
    {/if}

    <div class="list">
      {#each visible as p (p.id)}
        {@const wss = workspaces[p.id] ?? []}
        {@const isOpen = !!expanded[p.id]}
        <section class="card project" class:current={scopeState.projectId === p.id}>
          <div class="head">
            <button class="expander" type="button" aria-expanded={isOpen} aria-controls={`ws-${p.id}`} aria-label={`${isOpen ? 'Collapse' : 'Expand'} ${p.name}`} onclick={() => toggle(p.id)}>
              <span class="chev" class:open={isOpen}><ChevronRight size={16} /></span>
            </button>
            <span class="tile"><FolderKanban size={16} /></span>

            <div class="ident">
              {#if renamingId === p.id}
                <form
                  class="rename"
                  onsubmit={(e) => {
                    e.preventDefault();
                    void commitRename(p);
                  }}
                >
                  <Input
                    bind:value={renameValue}
                    label="Project name"
                    size="sm"
                    autofocus
                    onkeydown={(e) => {
                      if (e.key === 'Escape') {
                        e.stopPropagation();
                        cancelRename();
                      }
                    }}
                  />
                  <IconButton label="Save name" variant="secondary" onclick={() => void commitRename(p)} disabled={renameBusy || !renameValue.trim()}><Check size={14} /></IconButton>
                  <IconButton label="Cancel rename" onclick={cancelRename}><X size={14} /></IconButton>
                </form>
              {:else}
                <button class="name" type="button" onclick={() => toggle(p.id)}>{p.name}</button>
                {#if scopeState.projectId === p.id}<Badge tone="accent" size="sm">Current scope</Badge>{/if}
              {/if}
              <div class="meta">
                {#if p.configMode}<Badge tone="neutral" size="sm">{p.configMode} config</Badge>{/if}
                <span>{p.workspaceCount} workspace{p.workspaceCount === 1 ? '' : 's'}</span>
                <span class="dotsep">·</span>
                <span>Last used <RelativeTime ts={p.lastUsedAt} empty="never" /></span>
              </div>
            </div>

            {#if renamingId !== p.id}
              <div class="row-actions">
                <IconButton label={`Rename ${p.name}`} onclick={() => startRename(p)}><Pencil size={14} /></IconButton>
                <IconButton label={`Forget ${p.name}`} variant="danger" onclick={() => void forget(p)}><Trash2 size={14} /></IconButton>
              </div>
            {/if}
          </div>

          {#if isOpen}
            <div class="workspaces" id={`ws-${p.id}`}>
              {#each wss as w (w.id)}
                {@const active = scopeState.workspaceId === w.id}
                {@const missing = w.missing || missingIds.has(w.id)}
                <div class="ws" class:active>
                  <span class="wico"><FolderOpen size={15} /></span>
                  <div class="wbody">
                    <div class="path mono truncate" title={w.path}>{w.path}</div>
                    <div class="wmeta">
                      {#if active}<Badge tone="accent" size="sm">In use</Badge>{/if}
                      {#if missing}<Badge tone="warn" size="sm" dot>missing</Badge>{/if}
                      {#if !w.confirmed}<Badge tone="info" size="sm">unconfirmed</Badge>{/if}
                      <span class="muted">Last seen <RelativeTime ts={w.lastSeenAt} empty="never" /></span>
                    </div>
                  </div>
                  <div class="wactions">
                    <IconButton label="Copy path" size="sm" onclick={() => void copyPath(w.path)}><Copy size={13} /></IconButton>
                    <Button size="sm" variant={active ? 'secondary' : 'primary'} disabled={active} onclick={() => useWorkspace(w)}>
                      {#if active}<Check size={13} />Selected{:else}Use this workspace{/if}
                    </Button>
                  </div>
                </div>
              {:else}
                <div class="none"><CircleAlert size={14} />No workspaces linked to this project.</div>
              {/each}
              <div class="foot">
                <Button size="sm" variant="ghost" onclick={() => openLinkDialog(p)}><Link size={13} />Link another folder</Button>
              </div>
            </div>
          {/if}
        </section>
      {:else}
        <div class="card">
          <EmptyState compact icon={FolderKanban} title="No matching projects" body={`Nothing matches “${filter}”.`} />
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if openDialog}
  <Dialog
    title={linkTarget ? `Link a folder to ${linkTarget.name}` : 'Open workspace'}
    description={linkTarget ? 'The folder becomes another workspace of this project.' : 'Register a project folder with the daemon and switch to it.'}
    icon={linkTarget ? Link : FolderPlus}
    width={500}
    onClose={() => (openDialog = false)}
  >
    <form id="open-workspace-form" onsubmit={submitPath}>
      <div class="form-field">
        <label for="ws-path">Folder path</label>
        <div class="path-row">
          <Input
            id="ws-path"
            bind:value={openPath}
            label="Workspace path"
            placeholder="/home/you/code/my-project"
            mono
            autofocus
            invalid={pathInvalid || !!openError}
            oninput={() => (openError = '')}
          />
        </div>
        {#if pathInvalid}
          <span class="hint bad">Enter an absolute path, e.g. /home/you/code/app or C:\code\app.</span>
        {:else}
          <span class="hint">The path is resolved on the machine running the daemon.</span>
        {/if}
      </div>
      {#if openError}
        <div class="dlg-error"><Banner tone="err" message={openError} /></div>
      {/if}
    </form>
    {#snippet footer()}
      <Button variant="secondary" onclick={() => (openDialog = false)}>Cancel</Button>
      <Button variant="primary" loading={openBusy || scopeState.resolving} disabled={!openPath.trim() || pathInvalid} onclick={submitPath}>
        {linkTarget ? 'Link folder' : 'Open'}
      </Button>
    {/snippet}
  </Dialog>
{/if}

<style>
  .actions {
    align-self: center;
  }
  .toolbar .search {
    width: 320px;
    max-width: 100%;
  }
  .count {
    font-size: var(--fs-sm);
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .card.skel {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-5);
  }
  .skel-text {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .project {
    transition: border-color var(--dur-fast) var(--ease);
  }
  .project:hover {
    border-color: var(--border-strong);
  }
  .project.current {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-4) var(--space-4) var(--space-3);
  }
  .expander {
    width: 26px;
    height: 26px;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-2);
    flex: none;
  }
  .expander:hover:not(:disabled) {
    background: var(--bg-hover);
    border: none;
    color: var(--text-0);
  }
  .chev {
    display: inline-flex;
    transition: transform var(--dur) var(--ease);
  }
  .chev.open {
    transform: rotate(90deg);
  }
  .tile {
    display: grid;
    place-items: center;
    flex: none;
    width: 32px;
    height: 32px;
    border-radius: 9px;
    background: var(--accent-subtle);
    color: var(--accent);
  }
  .ident {
    display: flex;
    flex-direction: column;
    gap: 3px;
    flex: 1;
    min-width: 0;
  }
  .ident > :global(.badge),
  .ident > .name {
    align-self: flex-start;
  }
  .ident {
    flex-flow: row wrap;
    align-items: center;
    column-gap: var(--space-3);
  }
  .name {
    height: auto;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-0);
    font-size: var(--fs-lg);
    font-weight: 600;
    letter-spacing: -0.01em;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .name:hover:not(:disabled) {
    background: transparent;
    border: none;
    color: var(--accent);
  }
  .meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    flex-basis: 100%;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .dotsep {
    color: var(--border-strong);
  }
  .rename {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: min(420px, 100%);
  }
  .row-actions {
    display: flex;
    align-items: center;
    gap: 2px;
    flex: none;
  }
  .workspaces {
    border-top: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-0) 40%, var(--bg-1));
    border-radius: 0 0 var(--radius-lg) var(--radius-lg);
  }
  .ws {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-3) var(--space-4) var(--space-3) 62px;
    border-bottom: 1px solid var(--border);
  }
  .ws.active {
    background: var(--accent-subtle);
  }
  .wico {
    color: var(--text-2);
    display: inline-flex;
    flex: none;
  }
  .wbody {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .path {
    font-size: var(--fs-sm);
    color: var(--text-0);
  }
  .wmeta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    font-size: var(--fs-xs);
  }
  .wactions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: none;
  }
  .none {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: var(--space-5) var(--space-5) var(--space-5) 62px;
    color: var(--text-2);
    font-size: var(--fs-sm);
    border-bottom: 1px solid var(--border);
  }
  .foot {
    padding: var(--space-3) var(--space-4) var(--space-3) 54px;
  }
  .path-row {
    display: flex;
    gap: var(--space-3);
  }
  .hint.bad {
    color: var(--err);
  }
  .dlg-error {
    margin-top: var(--space-4);
  }

  @media (max-width: 900px) {
    .ws {
      flex-wrap: wrap;
      padding-left: var(--space-5);
    }
    .wactions {
      flex-wrap: wrap;
      margin-left: 27px;
    }
    .none,
    .foot {
      padding-left: var(--space-5);
    }
  }
</style>
