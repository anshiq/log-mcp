<script lang="ts">
  import type { Snippet } from 'svelte';
  import TopBar from './TopBar.svelte';
  import Sidebar from './Sidebar.svelte';
  import StatusBar from './StatusBar.svelte';

  let { children }: { children?: Snippet } = $props();
</script>

<div class="shell">
  <TopBar />
  <div class="body">
    <Sidebar />
    <main class="content">
      {#if children}{@render children()}{/if}
    </main>
  </div>
  <StatusBar />
</div>

<style>
  .shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
    background: var(--bg-0);
  }
  .body {
    flex: 1;
    display: flex;
    min-height: 0;
  }
  .content {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    position: relative;
  }
</style>
