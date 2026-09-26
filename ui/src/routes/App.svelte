<script lang="ts">
  import { onMount } from 'svelte';
  import Login from './Login.svelte';
  import ProcessesPage from './pages/ProcessesPage.svelte';
  import AppsPage from './pages/AppsPage.svelte';
  import ConfigPage from './pages/ConfigPage.svelte';
  import ProjectsPage from './pages/ProjectsPage.svelte';
  import EventsPage from './pages/EventsPage.svelte';
  import SessionsPage from './pages/SessionsPage.svelte';
  import IntegrationsPage from './pages/IntegrationsPage.svelte';
  import AuditPage from './pages/AuditPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import { router } from '../lib/router.svelte';
  import { scope } from '../lib/scope.svelte';
  import { SystemService, hasAuthToken, setUnauthorizedHandler } from '../lib/api';

  let daemonVersion = $state('');
  let daemonOk = $state(false);
  let workspacePathInput = $state('');
  let authed = $state(__APP_TARGET__ !== 'web' || hasAuthToken());

  onMount(() => {
    if (__APP_TARGET__ === 'web') {
      setUnauthorizedHandler(() => (authed = false));
    }
    if (!authed) return;
    if (router.path === '/') router.navigate('/processes', { replace: true });
    void SystemService.version().then((v) => {
      daemonVersion = v.daemonVersion;
      daemonOk = true;
    });
    scope.restoreLast();
  });

  async function openWorkspace(e: Event) {
    e.preventDefault();
    if (!workspacePathInput.trim()) return;
    await scope.resolve(workspacePathInput.trim());
    workspacePathInput = '';
  }

  const navItems = [
    { path: '/processes', label: 'Processes' },
    { path: '/apps', label: 'Apps' },
    { path: '/config', label: 'Config' },
    { path: '/events', label: 'Events' },
    { path: '/projects', label: 'Projects' },
    { path: '/sessions', label: 'Sessions' },
    { path: '/integrations', label: 'Integrations' },
    { path: '/audit', label: 'Audit' },
    { path: '/settings', label: 'Settings' }
  ];

  function isActive(path: string): boolean {
    return router.path === path || router.path.startsWith(path + '/');
  }
</script>

{#if !authed}
  <Login onAuthed={() => (authed = true)} />
{:else}
  <main>
    <header class="topbar">
      <span class="brand">agent-runtime</span>
      <form class="scope" onsubmit={openWorkspace}>
        <input placeholder="Workspace path…" bind:value={workspacePathInput} aria-label="Workspace path" />
        <button type="submit" disabled={scope.resolving}>{scope.resolving ? 'Opening…' : 'Open'}</button>
      </form>
      {#if scope.projectName}
        <span class="scope-name">{scope.projectName}</span>
      {/if}
      {#if scope.error}
        <span class="scope-error">{scope.error}</span>
      {/if}
      <div class="spacer"></div>
      <span class="status" class:ok={daemonOk}>
        <span class="dot"></span>
        {daemonVersion ? `daemon ${daemonVersion}` : 'connecting…'}
      </span>
    </header>
    <div class="body">
      <nav class="sidebar">
        {#each navItems as item}
          <button class:active={isActive(item.path)} onclick={() => router.navigate(item.path)}>{item.label}</button>
        {/each}
      </nav>
      <div class="content">
        {#if router.match('/processes/:id?')}
          <ProcessesPage />
        {:else if router.match('/apps')}
          <AppsPage />
        {:else if router.match('/config/:sub?')}
          <ConfigPage />
        {:else if router.match('/projects')}
          <ProjectsPage />
        {:else if router.match('/events')}
          <EventsPage />
        {:else if router.match('/sessions')}
          <SessionsPage />
        {:else if router.match('/integrations')}
          <IntegrationsPage />
        {:else if router.match('/audit')}
          <AuditPage />
        {:else if router.match('/settings')}
          <SettingsPage />
        {:else}
          <ProcessesPage />
        {/if}
      </div>
    </div>
  </main>
{/if}

<style>
  main {
    display: flex;
    flex-direction: column;
    height: 100vh;
  }
  .topbar {
    height: var(--topbar-h);
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: 0 var(--space-5);
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
  }
  .brand {
    font-weight: 600;
    color: var(--text-0);
  }
  .scope {
    display: flex;
    gap: var(--space-2);
  }
  .scope input {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    color: var(--text-0);
    width: 260px;
  }
  .scope button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-4);
    color: var(--text-0);
  }
  .scope-name {
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  .scope-error {
    color: var(--err);
    font-size: var(--fs-sm);
  }
  .spacer {
    flex: 1;
  }
  .status {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  .status .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--neutral);
  }
  .status.ok .dot {
    background: var(--ok);
  }
  .body {
    flex: 1;
    display: flex;
    min-height: 0;
  }
  .sidebar {
    width: var(--sidebar-w);
    flex: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-4);
    background: var(--bg-1);
    border-right: 1px solid var(--border);
  }
  .sidebar button {
    text-align: left;
    background: transparent;
    border: none;
    border-radius: var(--radius);
    padding: var(--space-3) var(--space-4);
    color: var(--text-1);
    font-size: var(--fs-md);
  }
  .sidebar button.active {
    background: var(--accent-subtle);
    color: var(--text-0);
  }
  .sidebar button:hover:not(.active) {
    background: var(--bg-2);
  }
  .content {
    flex: 1;
    min-width: 0;
    overflow: auto;
  }
</style>
