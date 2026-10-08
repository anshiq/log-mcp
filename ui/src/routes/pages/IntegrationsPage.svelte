<script lang="ts">
  import { onMount } from 'svelte';
  import Plug from '@lucide/svelte/icons/plug';
  import Bot from '@lucide/svelte/icons/bot';
  import Sparkles from '@lucide/svelte/icons/sparkles';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import FileDiff from '@lucide/svelte/icons/file-diff';
  import Download from '@lucide/svelte/icons/download';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import PackageCheck from '@lucide/svelte/icons/package-check';
  import PackageX from '@lucide/svelte/icons/package-x';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import Circle from '@lucide/svelte/icons/circle';
  import ArrowUp from '@lucide/svelte/icons/arrow-up';
  import { IntegrationService, type Harness, type Skill } from '../../lib/api';
  import { dialogs } from '../../lib/state/dialogs.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Banner from '../../lib/ui/Banner.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import DiffView from '../../lib/ui/DiffView.svelte';
  import RelativeTime from '../../lib/ui/RelativeTime.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';

  interface UpdateRow {
    name: string;
    have: string;
    want: string;
    harness: string;
    outdated: boolean;
  }

  let harnesses = $state<Harness[]>([]);
  let skills = $state<Skill[]>([]);
  let updates = $state<UpdateRow[]>([]);
  let loadingHarnesses = $state(true);
  let loadingSkills = $state(true);
  let harnessError = $state('');
  let skillError = $state('');
  let checking = $state(false);
  let checkedAt = $state(0);
  let busy = $state('');
  let updatingAll = $state(false);

  let preview = $state<{ harness: Harness; diff: string; loading: boolean; error: string; busy: boolean } | null>(null);

  const outdated = $derived(updates.filter((u) => u.outdated));
  const detectedCount = $derived(harnesses.filter((h) => h.detected).length);
  const configuredCount = $derived(harnesses.filter((h) => h.mcpGlobal).length);

  function cleanVersion(v: string): string {
    return /^v?\d+(\.\d+)+/.test(v.trim()) ? v.trim() : '';
  }

  function outdatedFor(name: string): UpdateRow[] {
    return outdated.filter((u) => u.name === name);
  }

  function installedOn(name: string): Harness[] {
    return harnesses.filter((h) => h.skillsGlobal.includes(name));
  }

  async function loadHarnesses(initial = false) {
    if (initial) loadingHarnesses = true;
    harnessError = '';
    try {
      const list = await IntegrationService.listHarnesses();
      harnesses = [...list].sort((a, b) => Number(b.detected) - Number(a.detected) || Number(b.mcpGlobal) - Number(a.mcpGlobal));
    } catch (err) {
      harnessError = err instanceof Error ? err.message : String(err);
    } finally {
      loadingHarnesses = false;
    }
  }

  async function loadSkills(initial = false) {
    if (initial) loadingSkills = true;
    skillError = '';
    try {
      skills = await IntegrationService.listSkills();
    } catch (err) {
      skillError = err instanceof Error ? err.message : String(err);
    } finally {
      loadingSkills = false;
    }
  }

  async function load(initial = false) {
    await Promise.all([loadHarnesses(initial), loadSkills(initial)]);
  }

  async function checkUpdates(silent = true) {
    checking = true;
    try {
      const r = await IntegrationService.checkUpdates();
      updates = (r.updates ?? []).map((u) => ({
        name: String(u['name'] ?? ''),
        have: String(u['have'] ?? u['installedVersion'] ?? ''),
        want: String(u['want'] ?? u['latestVersion'] ?? ''),
        harness: String(u['harness'] ?? ''),
        outdated: u['outdated'] !== false
      }));
      checkedAt = Date.now();
      if (!silent) {
        const n = updates.filter((u) => u.outdated).length;
        if (n === 0) toasts.ok('All installed skills are up to date');
        else toasts.info(`${n} skill install${n === 1 ? '' : 's'} can be updated`);
      }
    } catch (err) {
      updates = [];
      if (!silent) toastError(err);
    } finally {
      checking = false;
    }
  }

  async function refresh() {
    await Promise.all([load(), checkUpdates()]);
  }

  async function openPreview(h: Harness) {
    preview = { harness: h, diff: '', loading: true, error: '', busy: false };
    try {
      const res = await IntegrationService.previewInstall(h.id);
      if (preview && preview.harness.id === h.id) preview.diff = res.diff;
    } catch (err) {
      if (preview) preview.error = err instanceof Error ? err.message : String(err);
    } finally {
      if (preview) preview.loading = false;
    }
  }

  async function confirmInstall() {
    if (!preview) return;
    const p = preview;
    p.busy = true;
    try {
      const res = await IntegrationService.installMCP(p.harness.id);
      toasts.ok(`MCP installed for ${p.harness.displayName}${res.path ? ` · ${res.path}` : ''}`);
      preview = null;
      await load();
    } catch (err) {
      p.error = err instanceof Error ? err.message : String(err);
      toastError(err);
    } finally {
      p.busy = false;
    }
  }

  async function removeMCP(h: Harness) {
    const ok = await dialogs.confirm(`Remove the agent-runtime MCP entry from ${h.displayName}?\n\nThe agent will no longer be able to use agent-runtime tools.`, {
      title: `Remove MCP from ${h.displayName}`,
      confirmLabel: 'Remove',
      danger: true
    });
    if (!ok) return;
    busy = `${h.id}:mcp`;
    try {
      const res = await IntegrationService.removeMCP(h.id);
      toasts.ok(res.changed === false ? `${h.displayName} had no MCP entry to remove` : `MCP removed from ${h.displayName}`);
      await load();
    } catch (err) {
      toastError(err);
    } finally {
      busy = '';
    }
  }

  async function installSkills(h: Harness) {
    busy = `${h.id}:skills`;
    try {
      await IntegrationService.installSkillsFor(h.id);
      toasts.ok(`Skills installed for ${h.displayName}`);
      await refresh();
    } catch (err) {
      toastError(err);
    } finally {
      busy = '';
    }
  }

  async function removeSkills(h: Harness) {
    const ok = await dialogs.confirm(`Remove the agent-runtime skills from ${h.displayName}?`, {
      title: `Remove skills from ${h.displayName}`,
      confirmLabel: 'Remove skills',
      danger: true
    });
    if (!ok) return;
    busy = `${h.id}:skills`;
    try {
      await IntegrationService.removeSkillsFor(h.id);
      toasts.ok(`Skills removed from ${h.displayName}`);
      await refresh();
    } catch (err) {
      toastError(err);
    } finally {
      busy = '';
    }
  }

  async function updateAll() {
    const targets = [...new Set(outdated.map((u) => u.harness))];
    if (targets.length === 0) return;
    updatingAll = true;
    let failed = 0;
    for (const harness of targets) {
      try {
        await IntegrationService.installSkillsFor(harness);
      } catch {
        failed++;
      }
    }
    updatingAll = false;
    if (failed) toasts.err(`${failed} of ${targets.length} skill installs failed to update`);
    else toasts.ok(`Updated skills for ${targets.length} agent${targets.length === 1 ? '' : 's'}`);
    await refresh();
  }

  onMount(() => {
    void load(true);
    void checkUpdates();
  });
</script>

<div class="page">
  <div class="page-header">
    <div class="titles">
      <h1>Integrations</h1>
      <p class="subtitle">Connect AI coding agents to agent-runtime over MCP, and install the skills that teach them how to use it.</p>
    </div>
    <div class="actions">
      <Button onclick={() => void refresh()} loading={checking} title="Re-detect agents and check for skill updates">
        {#if !checking}<RefreshCw size={14} />{/if}Refresh
      </Button>
    </div>
  </div>

  <section class="block" aria-labelledby="mcp-h">
    <div class="block-head">
      <div>
        <h2 id="mcp-h"><Bot size={16} />AI agents (MCP)</h2>
        <p class="muted small">
          {#if loadingHarnesses}Detecting agents…{:else}{detectedCount} detected · {configuredCount} connected{/if}
        </p>
      </div>
    </div>

    {#if harnessError}
      <Banner tone="err" title="Couldn’t load agents" message={harnessError}>
        {#snippet actions()}<Button size="sm" onclick={() => void loadHarnesses(true)}>Retry</Button>{/snippet}
      </Banner>
    {:else if loadingHarnesses}
      <div class="grid">
        {#each [0, 1, 2, 3] as i (i)}
          <div class="card hcard">
            <div class="hhead">
              <Skeleton width="36px" height="36px" radius="10px" />
              <div class="stack"><Skeleton width="120px" height="14px" /><Skeleton width="70px" height="11px" /></div>
            </div>
            <Skeleton height="12px" />
            <Skeleton width="60%" height="12px" />
            <div class="hfoot"><Skeleton width="110px" height="28px" radius="7px" /><Skeleton width="90px" height="28px" radius="7px" /></div>
          </div>
        {/each}
      </div>
    {:else if harnesses.length === 0}
      <div class="card"><EmptyState icon={Plug} title="No agents known" body="The daemon didn’t report any supported AI coding agents." /></div>
    {:else}
      <div class="grid">
        {#each harnesses as h (h.id)}
          {@const version = cleanVersion(h.version)}
          {@const mcpBusy = busy === `${h.id}:mcp`}
          {@const skillBusy = busy === `${h.id}:skills`}
          <article class="card hcard" class:dim={!h.detected}>
            <div class="hhead">
              <span class="hico" class:on={h.mcpGlobal}><Bot size={18} /></span>
              <div class="hname">
                <h3>{h.displayName}</h3>
                <div class="hsub">
                  {#if h.detected}<Badge tone="ok" size="sm" dot>Detected</Badge>{:else}<Badge tone="neutral" size="sm">Not detected</Badge>{/if}
                  {#if version}<span class="mono ver">{version}</span>{/if}
                </div>
              </div>
            </div>

            <ul class="status">
              <li>
                <span class="k">{#if h.mcpGlobal}<CircleCheck size={14} class="okc" />{:else}<Circle size={14} />{/if}MCP server</span>
                <Badge tone={h.mcpGlobal ? 'ok' : 'neutral'} size="sm">{h.mcpGlobal ? 'Configured' : 'Not configured'}</Badge>
              </li>
              <li>
                <span class="k">{#if h.skillsGlobal.length}<PackageCheck size={14} class="okc" />{:else}<PackageX size={14} />{/if}Skills</span>
                {#if h.skillsGlobal.length}<Badge tone="ok" size="sm">{h.skillsGlobal.length} installed</Badge>{:else}<Badge tone="neutral" size="sm">None</Badge>{/if}
              </li>
            </ul>

            <div class="hfoot">
              {#if h.mcpGlobal}
                <Button size="sm" variant="danger" loading={mcpBusy} disabled={!!busy} onclick={() => void removeMCP(h)}>
                  {#if !mcpBusy}<Trash2 size={13} />{/if}Remove MCP
                </Button>
              {:else}
                <Button size="sm" variant="primary" disabled={!!busy} onclick={() => void openPreview(h)}><Download size={13} />Install MCP</Button>
              {/if}
              <Button size="sm" variant="ghost" disabled={!!busy} onclick={() => void openPreview(h)} title="Preview the config change before installing">
                <FileDiff size={13} />Preview diff
              </Button>
              <span class="grow"></span>
              {#if h.skillsGlobal.length}
                <Button size="sm" variant="ghost" loading={skillBusy} disabled={!!busy} onclick={() => void removeSkills(h)}>Remove skills</Button>
              {:else}
                <Button size="sm" loading={skillBusy} disabled={!!busy} onclick={() => void installSkills(h)}>Install skills</Button>
              {/if}
            </div>
          </article>
        {/each}
      </div>
    {/if}
  </section>

  <section class="block" aria-labelledby="skills-h">
    <div class="block-head">
      <div>
        <h2 id="skills-h"><Sparkles size={16} />Skills</h2>
        <p class="muted small">
          {#if checking}Checking for updates…{:else if checkedAt}Checked <RelativeTime ts={checkedAt} /> · {outdated.length ? `${outdated.length} update${outdated.length === 1 ? '' : 's'} available` : 'up to date'}{:else}Bundled with this daemon{/if}
        </p>
      </div>
      <div class="actions-row">
        <Button size="sm" loading={checking} onclick={() => void checkUpdates(false)}>
          {#if !checking}<RefreshCw size={13} />{/if}Check for updates
        </Button>
        <Button size="sm" variant="primary" loading={updatingAll} disabled={outdated.length === 0} onclick={() => void updateAll()}>
          {#if !updatingAll}<ArrowUp size={13} />{/if}Update all{outdated.length ? ` (${outdated.length})` : ''}
        </Button>
      </div>
    </div>

    {#if skillError}
      <Banner tone="err" title="Couldn’t load skills" message={skillError}>
        {#snippet actions()}<Button size="sm" onclick={() => void loadSkills(true)}>Retry</Button>{/snippet}
      </Banner>
    {:else}
      <div class="card list">
        {#if loadingSkills}
          {#each [0, 1] as i (i)}
            <div class="skill">
              <Skeleton width="34px" height="34px" radius="9px" />
              <div class="stack grow"><Skeleton width="180px" height="14px" /><Skeleton width="60%" height="11px" /></div>
            </div>
          {/each}
        {:else if skills.length === 0}
          <EmptyState compact icon={Sparkles} title="No skills bundled" body="This daemon build doesn’t ship any agent skills." />
        {:else}
          {#each skills as s (s.name)}
            {@const on = installedOn(s.name)}
            {@const stale = outdatedFor(s.name)}
            <div class="skill">
              <span class="sico" class:on={on.length > 0}><Sparkles size={16} /></span>
              <div class="sbody">
                <div class="sname">
                  <span class="mono">{s.name}</span>
                  <Badge size="sm">v{s.version || '0'}</Badge>
                  {#if stale.length}<Badge tone="warn" size="sm" dot>Outdated</Badge>{/if}
                </div>
                <p class="sdesc">{s.description || 'Teaches agents how to run and read logs through agent-runtime.'}</p>
                <div class="chips-row">
                  {#if loadingHarnesses}
                    <Skeleton width="110px" height="16px" radius="999px" />
                  {:else if on.length}
                    {#each on as h (h.id)}<Badge tone="ok" size="sm">{h.displayName}</Badge>{/each}
                  {:else}
                    <span class="muted small">Not installed on any agent</span>
                  {/if}
                  {#each stale as u (u.harness)}
                    <span class="muted small">{u.harness}: {u.have || 'unversioned'} → {u.want}</span>
                  {/each}
                </div>
              </div>
            </div>
          {/each}
        {/if}
      </div>
    {/if}
  </section>
</div>

{#if preview}
  {@const p = preview}
  <Dialog
    title={`Install MCP for ${p.harness.displayName}`}
    description="Adds the agent-runtime server to your user config. A backup is written next to the file."
    icon={FileDiff}
    width={620}
    flush
    onClose={() => (preview = null)}
  >
    <div class="preview">
      {#if p.loading}
        <div class="pv-loading"><Spinner size={16} />Building preview…</div>
      {:else if p.error}
        <Banner tone="err" message={p.error} />
      {:else}
        <DiffView diff={p.diff} emptyText="No changes: agent-runtime is already configured here" />
      {/if}
    </div>
    {#snippet footer()}
      <Button onclick={() => (preview = null)}>Cancel</Button>
      <Button variant="primary" loading={p.busy} disabled={p.loading || !!p.error} onclick={() => void confirmInstall()}>
        <Download size={14} />Install
      </Button>
    {/snippet}
  </Dialog>
{/if}

<style>
  .block {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .block-head {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--space-4);
    flex-wrap: wrap;
  }
  h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: var(--fs-lg);
  }
  h2 :global(svg) {
    color: var(--text-2);
  }
  .small {
    font-size: var(--fs-sm);
  }
  .actions-row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: var(--space-4);
  }
  .hcard {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-5);
    transition: border-color var(--dur-fast) var(--ease);
  }
  .hcard:hover {
    border-color: var(--border-strong);
  }
  .hcard.dim .hhead {
    opacity: 0.75;
  }
  .hhead {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }
  .hico,
  .sico {
    display: grid;
    place-items: center;
    flex: none;
    width: 36px;
    height: 36px;
    border-radius: 10px;
    background: var(--bg-3);
    color: var(--text-2);
  }
  .hico.on,
  .sico.on {
    background: var(--accent-subtle);
    color: var(--accent);
  }
  .hname h3 {
    font-size: var(--fs-md);
  }
  .hsub {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 3px;
  }
  .ver {
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .status {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--bg-0) 35%, var(--bg-1));
  }
  .status li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px var(--space-4);
    font-size: var(--fs-sm);
  }
  .status li + li {
    border-top: 1px solid var(--border);
  }
  .k {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--text-1);
  }
  .k :global(svg) {
    color: var(--text-2);
  }
  .k :global(.okc) {
    color: var(--ok);
  }
  .hfoot {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-top: auto;
  }
  .grow {
    flex: 1;
  }
  .stack {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .list {
    overflow: hidden;
  }
  .skill {
    display: flex;
    align-items: flex-start;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-5);
  }
  .skill + .skill {
    border-top: 1px solid var(--border);
  }
  .sbody {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .sname {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    font-size: var(--fs-md);
    font-weight: 600;
  }
  .sdesc {
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  .chips-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 2px;
  }
  .preview {
    padding: var(--space-4) var(--space-6) var(--space-5);
  }
  .pv-loading {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: var(--space-6) 0;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
</style>
