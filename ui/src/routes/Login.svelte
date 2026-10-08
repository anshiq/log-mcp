<script lang="ts">
  import Activity from '@lucide/svelte/icons/activity';
  import Eye from '@lucide/svelte/icons/eye';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import { setAuthToken, clearAuthToken, SystemService } from '../lib/api';
  import Spinner from '../lib/ui/Spinner.svelte';
  import Checkbox from '../lib/ui/Checkbox.svelte';
  import { getPlatform } from '../lib/platform';

  let { onAuthed }: { onAuthed: () => void } = $props();

  let token = $state('');
  let remember = $state(false);
  let reveal = $state(false);
  let error = $state('');
  let checking = $state(false);
  let copied = $state(false);

  const command = 'agent-runtime web open';

  function focusOnMount(node: HTMLInputElement) {
    node.focus();
  }

  async function submit(e: Event) {
    e.preventDefault();
    if (!token.trim() || checking) return;
    checking = true;
    error = '';
    setAuthToken(token.trim(), remember);
    try {
      await SystemService.version();
      onAuthed();
    } catch (err) {
      clearAuthToken();
      error = err instanceof Error ? err.message : String(err);
    } finally {
      checking = false;
    }
  }

  async function copy() {
    await getPlatform().copyText(command);
    copied = true;
    setTimeout(() => (copied = false), 1600);
  }
</script>

<div class="wrap">
  <div class="stack">
    <div class="brand">
      <span class="logo"><Activity size={20} strokeWidth={2.4} /></span>
      <span class="name">agent-runtime</span>
    </div>

    <form class="card" onsubmit={submit}>
      <div class="head">
        <h1>Connect to the daemon</h1>
        <p>Enter the access token to open the dashboard.</p>
      </div>

      <div class="field">
        <label for="login-token">Access token</label>
        <div class="input" class:invalid={!!error}>
          <span class="lead"><KeyRound size={14} /></span>
          <input
            id="login-token"
            type={reveal ? 'text' : 'password'}
            placeholder="Paste your token"
            bind:value={token}
            oninput={() => (error = '')}
            aria-label="Bearer token"
            aria-invalid={!!error}
            aria-describedby={error ? 'login-error' : undefined}
            autocomplete="off"
            spellcheck="false"
            use:focusOnMount
          />
          <button class="eye" type="button" onclick={() => (reveal = !reveal)} aria-label={reveal ? 'Hide token' : 'Show token'} aria-pressed={reveal}>
            {#if reveal}<EyeOff size={14} />{:else}<Eye size={14} />{/if}
          </button>
        </div>
        {#if error}
          <p class="error" id="login-error" role="alert">
            <CircleAlert size={14} />
            <span><strong>That token was rejected.</strong> <span class="detail">{error}</span></span>
          </p>
        {/if}
      </div>

      <Checkbox bind:checked={remember}>Remember on this device</Checkbox>

      <button class="submit" type="submit" disabled={checking || !token.trim()}>
        {#if checking}<Spinner size={14} />Checking…{:else}Continue<ArrowRight size={14} />{/if}
      </button>

      <div class="help">
        <span class="label">Where do I find it?</span>
        <p>
          Run this in a terminal to print a sign-in link with the token filled in, and open it in your browser:
        </p>
        <div class="cmd">
          <code><span class="prompt">$</span> {command}</code>
          <button class="copy" type="button" onclick={() => void copy()} aria-label="Copy command">
            {#if copied}<Check size={14} />{:else}<Copy size={14} />{/if}
          </button>
        </div>
        <p class="alt">Or paste the contents of <code>~/.config/agent-runtime/token</code>.</p>
      </div>
    </form>
  </div>
</div>

<style>
  .wrap {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    padding: var(--space-6) var(--space-5);
    background: var(--bg-0);
    overflow: auto;
  }
  .stack {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-6);
    width: 100%;
    max-width: 360px;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }
  .logo {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border-radius: 3px;
    background: var(--accent);
    color: var(--accent-fg);
    box-shadow: 0 0 0 5px var(--accent-subtle);
  }
  .name {
    font-family: var(--font-mono);
    font-size: var(--fs-xl);
    font-weight: 600;
    

  }
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    width: 100%;
    padding: 20px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
  }
  .head h1 {
    font-size: var(--fs-lg);
  }
  .head p {
    margin-top: 2px;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .field label {
    color: var(--text-1);
    font-size: var(--fs-xs);
    font-weight: 500;
    
    letter-spacing: 0;
  }
  .input {
    display: flex;
    align-items: center;
    height: 38px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    transition:
      border-color var(--dur-fast) var(--ease),
      box-shadow var(--dur-fast) var(--ease);
  }
  .input:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-subtle);
  }
  .input.invalid {
    border-color: var(--err);
  }
  .input.invalid:focus-within {
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--err) 20%, transparent);
  }
  .lead {
    display: inline-flex;
    padding-left: 11px;
    color: var(--text-2);
  }
  .input input {
    flex: 1;
    min-width: 0;
    height: 100%;
    padding: 0 8px;
    background: transparent;
    border: none;
    outline: none;
    box-shadow: none;
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
    color: var(--text-0);
  }
  .eye {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    margin-right: 3px;
    padding: 0;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--text-2);
  }
  .eye:hover:not(:disabled) {
    background: var(--bg-hover);
    border: none;
    color: var(--text-0);
  }
  .error {
    display: flex;
    align-items: flex-start;
    gap: 7px;
    color: var(--err);
    font-size: var(--fs-sm);
    line-height: 1.45;
  }
  .error :global(svg) {
    flex: none;
    margin-top: 2px;
  }
  .error .detail {
    color: var(--text-1);
    overflow-wrap: anywhere;
  }
  .submit {
    height: 38px;
    gap: 8px;
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fs-md);
  }
  .submit:hover:not(:disabled) {
    background: var(--accent-hover);
    border-color: var(--accent-hover);
  }
  .help {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: var(--space-5);
    border-top: 1px solid var(--border);
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .help .label {
    color: var(--text-1);
    font-size: var(--fs-xs);
    font-weight: 600;
    
    letter-spacing: 0;
  }
  .cmd {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 4px 4px 4px var(--space-4);
    background: var(--bg-0);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
  }
  .cmd code {
    flex: 1;
    min-width: 0;
    overflow-x: auto;
    white-space: nowrap;
    color: var(--text-0);
    font-size: var(--fs-xs);
  }
  .prompt {
    margin-right: 6px;
    color: var(--text-2);
    user-select: none;
  }
  .copy {
    width: 26px;
    height: 26px;
    padding: 0;
    border-color: transparent;
    background: transparent;
    color: var(--text-1);
  }
  .alt code {
    padding: 1px 5px;
    border-radius: 4px;
    background: var(--bg-3);
    color: var(--text-1);
    font-size: var(--fs-xs);
  }
</style>
