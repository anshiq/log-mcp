<script lang="ts">
  import { setAuthToken, SystemService } from '../lib/api';

  let { onAuthed }: { onAuthed: () => void } = $props();

  let token = $state('');
  let remember = $state(false);
  let error = $state('');
  let checking = $state(false);

  function focusOnMount(node: HTMLInputElement) {
    node.focus();
  }

  async function submit(e: Event) {
    e.preventDefault();
    if (!token.trim()) return;
    checking = true;
    error = '';
    setAuthToken(token.trim(), remember);
    try {
      await SystemService.version();
      onAuthed();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      checking = false;
    }
  }
</script>

<div class="wrap">
  <form class="card" onsubmit={submit}>
    <h1>agent-runtime</h1>
    <p class="hint">
      Paste the bearer token from <code>$XDG_CONFIG_HOME/agent-runtime/token</code>, or run
      <code>agent-runtime web open</code> to open this page with the token pre-filled.
    </p>
    <input type="password" placeholder="Token…" bind:value={token} aria-label="Bearer token" use:focusOnMount />
    <label class="remember">
      <input type="checkbox" bind:checked={remember} />
      Remember on this device
    </label>
    {#if error}
      <p class="error">{error}</p>
    {/if}
    <button type="submit" disabled={checking}>{checking ? 'Checking…' : 'Continue'}</button>
  </form>
</div>

<style>
  .wrap {
    height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-0);
  }
  .card {
    width: 380px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: var(--space-8);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    box-shadow: var(--shadow-pop);
  }
  h1 {
    margin: 0;
    font-size: var(--fs-xl);
  }
  .hint {
    color: var(--text-1);
    font-size: var(--fs-sm);
    margin: 0;
  }
  .hint code {
    font-family: var(--font-mono);
    background: var(--bg-3);
    padding: 0 var(--space-1);
    border-radius: var(--radius-sm);
  }
  input[type='password'] {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: var(--space-3) var(--space-4);
    color: var(--text-0);
  }
  .remember {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  .error {
    color: var(--err);
    font-size: var(--fs-sm);
    margin: 0;
  }
  button {
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: var(--radius);
    padding: var(--space-3);
    font-size: var(--fs-md);
  }
  button:disabled {
    opacity: 0.6;
  }
</style>
