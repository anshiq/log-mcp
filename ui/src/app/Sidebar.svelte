<script lang="ts">
  import LayoutDashboard from '@lucide/svelte/icons/layout-dashboard';
  import Terminal from '@lucide/svelte/icons/terminal';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import Boxes from '@lucide/svelte/icons/boxes';
  import FileCog from '@lucide/svelte/icons/file-cog';
  import Activity from '@lucide/svelte/icons/activity';
  import FolderKanban from '@lucide/svelte/icons/folder-kanban';
  import Users from '@lucide/svelte/icons/users';
  import Plug from '@lucide/svelte/icons/plug';
  import ShieldCheck from '@lucide/svelte/icons/shield-check';
  import Settings from '@lucide/svelte/icons/settings';
  import PanelLeft from '@lucide/svelte/icons/panel-left';
  import { router } from '../lib/router.svelte';
  import { processes } from '../lib/state/processes.svelte';
  import { prefs } from '../lib/state/prefs.svelte';
  import type { Component } from 'svelte';

  interface Item {
    path: string;
    label: string;
    key: string;
    icon: Component<{ size?: number; strokeWidth?: number }>;
  }

  const groups: { title: string; items: Item[] }[] = [
    {
      title: 'Workspace',
      items: [
        { path: '/', label: 'Overview', key: 'g o', icon: LayoutDashboard },
        { path: '/processes', label: 'Processes', key: 'g p', icon: Terminal },
        { path: '/logs', label: 'Logs', key: 'g l', icon: ScrollText },
        { path: '/apps', label: 'Apps', key: 'g a', icon: Boxes },
        { path: '/config', label: 'Config', key: 'g c', icon: FileCog }
      ]
    },
    {
      title: 'Activity',
      items: [
        { path: '/events', label: 'Events', key: 'g e', icon: Activity },
        { path: '/sessions', label: 'Sessions', key: 'g s', icon: Users },
        { path: '/audit', label: 'Audit', key: 'g u', icon: ShieldCheck }
      ]
    },
    {
      title: 'Manage',
      items: [
        { path: '/projects', label: 'Projects', key: 'g j', icon: FolderKanban },
        { path: '/integrations', label: 'Integrations', key: 'g i', icon: Plug },
        { path: '/settings', label: 'Settings', key: 'g t', icon: Settings }
      ]
    }
  ];

  const collapsed = $derived(prefs.data.sidebarCollapsed);

  function isActive(p: string) {
    const cur = router.path.split('?')[0] ?? '/';
    return p === '/' ? cur === '/' : cur === p || cur.startsWith(p + '/');
  }
</script>

<nav class="sidebar" class:collapsed aria-label="Primary">
  <div class="groups">
    {#each groups as g}
      <div class="group">
        {#if !collapsed}<div class="title">{g.title}</div>{:else}<div class="sep"></div>{/if}
        {#each g.items as it}
          {@const Icon = it.icon}
          <button class="item" class:active={isActive(it.path)} onclick={() => void router.navigate(it.path)} title={collapsed ? `${it.label} (${it.key})` : it.key} aria-current={isActive(it.path) ? 'page' : undefined}>
            <span class="ico"><Icon size={16} strokeWidth={isActive(it.path) ? 2.2 : 1.9} /></span>
            {#if !collapsed}
              <span class="label">{it.label}</span>
              {#if it.path === '/processes' && processes.counts.failed > 0}
                <span class="count err">{processes.counts.failed}</span>
              {:else if it.path === '/processes' && processes.counts.running > 0}
                <span class="count">{processes.counts.running}</span>
              {/if}
            {:else if it.path === '/processes' && processes.counts.failed > 0}
              <span class="pip"></span>
            {/if}
          </button>
        {/each}
      </div>
    {/each}
  </div>
  <button class="collapse" onclick={() => prefs.set('sidebarCollapsed', !collapsed)} aria-label="Toggle sidebar" title="Toggle sidebar">
    <PanelLeft size={16} />
    {#if !collapsed}<span>Collapse</span>{/if}
  </button>
</nav>

<style>
  .sidebar {
    width: var(--sidebar-w);
    flex: none;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    padding: var(--space-4) var(--space-3) var(--space-3);
    background: var(--bg-1);
    border-right: 1px solid var(--border);
    overflow-y: auto;
    overflow-x: hidden;
    transition: width var(--dur) var(--ease);
  }
  .sidebar.collapsed {
    width: var(--sidebar-w-collapsed);
    padding-inline: var(--space-3);
  }
  .groups {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .title {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.24em;
    color: var(--text-2);
    padding: 0 10px 6px;
  }
  .sep {
    height: 1px;
    background: var(--border);
    margin: 2px 6px 6px;
  }
  .item,
  .collapse {
    position: relative;
    width: 100%;
    justify-content: flex-start;
    gap: 10px;
    padding: 7px 10px;
    border-color: transparent;
    background: transparent;
    color: var(--text-1);
    font-weight: 500;
    font-size: var(--fs-md);
    text-align: left;
  }
  .item:hover:not(:disabled),
  .collapse:hover:not(:disabled) {
    background: var(--bg-hover);
    border-color: transparent;
    color: var(--text-0);
  }
  .item.active {
    background: var(--accent-subtle);
    color: var(--text-0);
  }
  .item.active .ico {
    color: var(--accent);
  }
  .item.active::before {
    content: '';
    position: absolute;
    left: -12px;
    top: 7px;
    bottom: 7px;
    width: 3px;
    background: var(--accent);
  }
  .ico {
    display: grid;
    place-items: center;
    flex: none;
  }
  .label {
    flex: 1;
  }
  .count {
    font-size: 10.5px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    padding: 0 7px;
    border-radius: 2px;
    background: var(--bg-3);
    color: var(--text-1);
    line-height: 1.7;
  }
  .count.err {
    background: color-mix(in srgb, var(--err) 18%, transparent);
    color: var(--err);
  }
  .pip {
    position: absolute;
    top: 6px;
    right: 8px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--err);
  }
  .collapsed .item,
  .collapsed .collapse {
    justify-content: center;
    padding-inline: 0;
  }
  .collapsed .item.active::before {
    left: -12px;
  }
  .collapse {
    margin-top: var(--space-4);
    color: var(--text-2);
  }
  @media (max-width: 900px) {
    .sidebar {
      width: var(--sidebar-w-collapsed);
    }
    .label,
    .count,
    .title,
    .collapse span {
      display: none;
    }
    .item,
    .collapse {
      justify-content: center;
      padding-inline: 0;
    }
  }
</style>
