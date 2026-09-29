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
  import OverviewPage from './pages/OverviewPage.svelte';
  import LogsPage from './pages/LogsPage.svelte';
  import { router } from '../lib/router.svelte';
  import { scopeState } from '../lib/state/scope.svelte';
  import { connection } from '../lib/state/connection.svelte';
  import { events } from '../lib/state/events.svelte';
  import { processes } from '../lib/state/processes.svelte';
  import { handleAlertEvent } from '../lib/state/alerts.svelte';
  import { installGlobalKeys } from '../lib/keys';
  import { SystemService, hasAuthToken, setUnauthorizedHandler } from '../lib/api';
  import ToastRegion from '../lib/ui/ToastRegion.svelte';
  import CommandPalette from '../lib/ui/CommandPalette.svelte';
  import ConfirmHost from '../lib/ui/ConfirmHost.svelte';
  import Shell from '../app/Shell.svelte';
  import Splash from '../app/Splash.svelte';
  import DaemonUnreachable from '../app/DaemonUnreachable.svelte';
  import ShortcutsDialog from '../app/ShortcutsDialog.svelte';

  let authed = $state(__APP_TARGET__ !== 'web' || hasAuthToken());
  let booted = $state(false);
  let shortcutsOpen = $state(false);

  onMount(() => {
    if (__APP_TARGET__ === 'web') {
      setUnauthorizedHandler(() => (authed = false));
    }
    const offKeys = installGlobalKeys();
    const onShortcuts = () => (shortcutsOpen = true);
    window.addEventListener('ar:shortcuts', onShortcuts);
    void boot();
    return () => {
      offKeys();
      window.removeEventListener('ar:shortcuts', onShortcuts);
    };
  });

  async function boot() {
    if (!authed) {
      booted = true;
      return;
    }
    if (router.path === '/') router.navigate('/processes', { replace: true });
    try {
      const v = await SystemService.version();
      connection.version = v.daemonVersion;
      connection.apiVersion = v.apiVersion;
      connection.reachable = true;
    } catch {
      connection.reachable = false;
    }
    void scopeState.refresh();
    processes.connect('', true);
    connection.start();
    events.connect('');
    events.onEvent('logs.alert', handleAlertEvent);
    events.onEvent('process.crashed', handleAlertEvent);
    events.onEvent('process.failed', handleAlertEvent);
    booted = true;
  }
</script>

{#if !booted}
  <Splash />
{:else if !authed}
  <Login onAuthed={() => { authed = true; void boot(); }} />
{:else if !connection.reachable}
  <DaemonUnreachable onRetry={() => void boot()} />
  <ToastRegion />
{:else}
  <Shell>
    {#if router.match('/processes/:id?')}
      <ProcessesPage />
    {:else if router.match('/logs')}
      <LogsPage />
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
    {:else if router.match('/')}
      <OverviewPage />
    {:else}
      <ProcessesPage />
    {/if}
  </Shell>
  <ToastRegion />
  <CommandPalette />
  <ShortcutsDialog bind:open={shortcutsOpen} />
  <ConfirmHost />
{/if}
