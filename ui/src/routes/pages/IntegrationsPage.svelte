<script lang="ts">
  import { onMount } from 'svelte';
  import { IntegrationService } from '../../lib/api';

  interface Harness {
    id: string;
    displayName: string;
    detected: boolean;
    version?: string;
    mcpGlobal?: boolean;
    skillsGlobal?: string[];
  }
  interface Skill {
    name: string;
    version: string;
    description?: string;
    installed?: boolean;
  }

  let harnesses = $state<Harness[]>([]);
  let skills = $state<Skill[]>([]);
  let busy = $state<string | null>(null);

  async function load() {
    const [h, s] = await Promise.all([IntegrationService.listHarnesses(), IntegrationService.listSkills()]);
    harnesses = (h.harnesses as Harness[]) ?? [];
    skills = (s.skills as Skill[]) ?? [];
  }

  async function installMCP(h: Harness) {
    busy = h.id;
    try {
      const preview = await IntegrationService.previewInstall(h.id, 'global');
      if (!confirm(`Install MCP for ${h.displayName}?\n\n${preview.diff}`)) return;
      await IntegrationService.installMCP(h.id, 'global');
      await load();
    } finally {
      busy = null;
    }
  }

  async function removeMCP(h: Harness) {
    if (!confirm(`Remove MCP integration for ${h.displayName}?`)) return;
    busy = h.id;
    try {
      await IntegrationService.removeMCP(h.id, 'global');
      await load();
    } finally {
      busy = null;
    }
  }

  async function installSkills(h: Harness) {
    busy = h.id;
    try {
      await IntegrationService.installSkills(
        skills.map((s) => s.name),
        'global'
      );
      await load();
    } finally {
      busy = null;
    }
  }

  onMount(load);
</script>

<div class="page">
  <section>
    <h2>Harnesses</h2>
    <table>
      <thead>
        <tr>
          <th>Harness</th>
          <th>Detected</th>
          <th>Version</th>
          <th>MCP</th>
          <th>Skills</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each harnesses as h (h.id)}
          <tr>
            <td>{h.displayName}</td>
            <td>{h.detected ? '✓' : '—'}</td>
            <td class="mono">{h.version ?? ''}</td>
            <td>{h.mcpGlobal ? '✓ installed' : '—'}</td>
            <td>{(h.skillsGlobal ?? []).length}</td>
            <td class="actions">
              {#if h.mcpGlobal}
                <button disabled={busy === h.id} onclick={() => removeMCP(h)}>Remove MCP</button>
              {:else}
                <button disabled={busy === h.id} onclick={() => installMCP(h)}>Install MCP</button>
              {/if}
              <button disabled={busy === h.id} onclick={() => installSkills(h)}>Install skills</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </section>
  <section>
    <h2>Skills</h2>
    <ul class="skills">
      {#each skills as s (s.name)}
        <li>
          <span class="name">{s.name}</span>
          <span class="version mono">{s.version}</span>
          <span class="desc">{s.description ?? ''}</span>
        </li>
      {/each}
    </ul>
  </section>
</div>

<style>
  .page {
    padding: var(--space-5);
    overflow: auto;
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: var(--space-7);
  }
  h2 {
    font-size: var(--fs-lg);
    margin: 0 0 var(--space-4);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-sm);
  }
  th,
  td {
    text-align: left;
    padding: var(--space-2) var(--space-4);
    border-bottom: 1px solid var(--border);
  }
  th {
    color: var(--text-2);
    text-transform: uppercase;
    font-size: var(--fs-xs);
  }
  .actions {
    display: flex;
    gap: var(--space-2);
  }
  .actions button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-3);
    color: var(--text-0);
    font-size: var(--fs-xs);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .skills {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .skills li {
    display: flex;
    gap: var(--space-4);
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--border);
    font-size: var(--fs-sm);
  }
  .skills .desc {
    color: var(--text-2);
  }
</style>
