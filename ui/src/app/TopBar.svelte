<script lang="ts">
  import Search from '@lucide/svelte/icons/search';
  import Sun from '@lucide/svelte/icons/sun';
  import Moon from '@lucide/svelte/icons/moon';
  import Activity from '@lucide/svelte/icons/activity';
  import { palette } from '../lib/palette.svelte';
  import { connection } from '../lib/state/connection.svelte';
  import { prefs } from '../lib/state/prefs.svelte';
  import ScopeSwitcher from './ScopeSwitcher.svelte';

  const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);

  const resolved = $derived(
    prefs.data.theme === 'system'
      ? typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: light)').matches
        ? 'light'
        : 'dark'
      : prefs.data.theme
  );

  function toggleTheme() {
    prefs.set('theme', resolved === 'dark' ? 'light' : 'dark');
  }
</script>

<header class="topbar">
  <div class="brand">
    <span class="logo"><Activity size={14} strokeWidth={1.75} /></span>
    <span class="name">agent-runtime</span>
  </div>
  <span class="divider"></span>
  <ScopeSwitcher />
  <div class="spacer"></div>
  <button class="search" onclick={() => void palette.show()} aria-label="Open command palette">
    <Search size={14} />
    <span>Search or run a command…</span>
    <span class="keys"><span class="kbd">{mac ? '⌘' : 'Ctrl'}</span><span class="kbd">K</span></span>
  </button>
  <button class="btn ghost icon" onclick={toggleTheme} aria-label="Toggle theme" title="Toggle theme">
    {#if resolved === 'dark'}<Sun size={14} />{:else}<Moon size={14} />{/if}
  </button>
  <span class="status" class:ok={connection.reachable} title={connection.socketPath || 'daemon'}>
    <span class="dot" class:ok={connection.reachable} class:warn={!connection.reachable}></span>
    {connection.reachable ? (connection.version ? `daemon ${connection.version}` : 'connected') : 'daemon offline'}
  </span>
</header>

<style>
  .topbar {
    height: var(--topbar-h);
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: calc(var(--sidebar-w) - var(--space-6) - 8px);
  }
  .logo {
    width: 20px;
    height: 20px;
    border-radius: 4px;
    display: grid;
    place-items: center;
    background: var(--accent);
    color: var(--accent-fg);
  }
  .name {
    font-family: var(--font-mono);
    font-weight: 600;
    font-size: var(--fs-sm);
  }
  .divider {
    width: 1px;
    height: 22px;
    background: var(--border);
  }
  .spacer {
    flex: 1;
  }
  .search {
    min-width: 220px;
    height: var(--control-h);
    justify-content: flex-start;
    gap: 8px;
    color: var(--text-2);
    background: var(--bg-2);
    font-weight: 400;
  }
  .search span:first-of-type {
    flex: 1;
    text-align: left;
  }
  .keys {
    display: inline-flex;
    gap: 3px;
  }
  .kbd {
    font-family: var(--font-mono);
    font-size: var(--fs-micro);
    padding: 0 4px;
    border-radius: 3px;
    border: 1px solid var(--border-strong);
    background: var(--bg-3);
    color: var(--text-1);
  }
  .status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-xs);
    font-family: var(--font-mono);
    color: var(--text-1);
    padding: 0 8px;
    height: 22px;
    border: 1px solid var(--border);
    border-radius: 3px;
    background: var(--bg-2);
    white-space: nowrap;
  }
  @media (max-width: 1100px) {
    .brand {
      min-width: 0;
    }
    .search {
      min-width: 0;
    }
    .search span:not(.keys):not(.kbd) {
      display: none;
    }
  }
  @media (max-width: 760px) {
    .name,
    .divider,
    .keys {
      display: none;
    }
  }
</style>
