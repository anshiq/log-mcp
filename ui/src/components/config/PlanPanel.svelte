<script lang="ts">
  import X from '@lucide/svelte/icons/x';
  import Plus from '@lucide/svelte/icons/plus';
  import Minus from '@lucide/svelte/icons/minus';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import Zap from '@lucide/svelte/icons/zap';
  import Pencil from '@lucide/svelte/icons/pencil';
  import Save from '@lucide/svelte/icons/save';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import GitCompare from '@lucide/svelte/icons/git-compare';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import DiffView from '../../lib/ui/DiffView.svelte';
  import type { ConfigSession } from '../../lib/config/session.svelte';

  let { session }: { session: ConfigSession } = $props();

  const plan = $derived(session.planCurrent);
  const changes = $derived(plan?.changes ?? []);
  const counts = $derived({
    added: changes.filter((c) => c.kind === 'added').length,
    removed: changes.filter((c) => c.kind === 'removed').length,
    restart: changes.filter((c) => c.kind === 'restart-required').length,
    live: changes.filter((c) => c.kind === 'applied-live').length
  });
  const blocked = $derived(session.applying || session.planning || session.invalid || session.empty || !session.writable);

  const kinds: Record<string, { label: string; cls: string }> = {
    added: { label: 'Added', cls: 'ok' },
    removed: { label: 'Removed', cls: 'err' },
    'restart-required': { label: 'Restart required', cls: 'warn' },
    'applied-live': { label: 'Applied live', cls: 'info' }
  };
</script>

<aside class="panel" aria-label="Planned changes">
  <header>
    <GitCompare size={16} />
    <h3>Review changes</h3>
    <span class="layer badge">{session.layer} layer</span>
    <span class="grow"></span>
    <button class="btn icon ghost sm" aria-label="Close changes" onclick={() => (session.drawerOpen = false)}><X size={15} /></button>
  </header>

  <div class="body">
    {#if session.planning && !plan}
      <div class="loading"><Spinner size={16} /> Planning changes…</div>
    {/if}

    {#if plan && plan.errors.length > 0}
      <div class="banner err">
        <TriangleAlert size={16} />
        <div class="grow">
          <strong>Plan failed validation</strong>
          {#each plan.errors as e}
            <div class="mono issue">line {e.line}: {e.message}</div>
          {/each}
        </div>
      </div>
    {/if}

    {#if plan && plan.supported && plan.errors.length === 0}
      <section>
        <div class="section-title">
          <h4>App changes</h4>
          <div class="chips">
            {#if counts.added}<span class="badge ok"><Plus size={11} />{counts.added} added</span>{/if}
            {#if counts.restart}<span class="badge warn"><RotateCw size={11} />{counts.restart} need restart</span>{/if}
            {#if counts.live}<span class="badge info"><Zap size={11} />{counts.live} live</span>{/if}
            {#if counts.removed}<span class="badge err"><Minus size={11} />{counts.removed} removed</span>{/if}
          </div>
        </div>
        {#if changes.length === 0}
          <p class="none">No app-level changes. Only formatting, comments or other settings differ.</p>
        {:else}
          <ul class="changes">
            {#each changes as c (c.app + c.kind)}
              {@const k = kinds[c.kind] ?? { label: c.kind, cls: '' }}
              <li class={c.kind}>
                <span class="mark {k.cls}">
                  {#if c.kind === 'added'}<Plus size={13} />{:else if c.kind === 'removed'}<Minus size={13} />{:else if c.kind === 'restart-required'}<RotateCw size={13} />{:else}<Pencil size={13} />{/if}
                </span>
                <div class="what">
                  <div class="line">
                    <strong class="mono">{c.app}</strong>
                    <span class="badge {k.cls}">{k.label}</span>
                    {#if c.affected.length > 0}
                      <span class="badge">{c.affected.length} running</span>
                    {/if}
                  </div>
                  {#if c.fields.length > 0}
                    <div class="fields">
                      {#each c.fields as f}<span class="field mono">{f}</span>{/each}
                    </div>
                  {/if}
                </div>
              </li>
            {/each}
          </ul>
        {/if}
        {#if plan.unaffected.length > 0}
          <p class="none">
            {plan.unaffected.length} app{plan.unaffected.length === 1 ? '' : 's'} defined in other layers
            (<span class="mono">{plan.unaffected.join(', ')}</span>) {plan.unaffected.length === 1 ? 'is' : 'are'} not part of this layer and stay as they are.
          </p>
        {/if}
      </section>
    {:else if plan && !plan.supported}
      <div class="banner info">
        <span>App-level planning covers the project layer. This layer is applied as-is; review the text diff below.</span>
      </div>
    {/if}

    <section>
      <div class="section-title"><h4>Text diff</h4></div>
      <DiffView old={session.saved[session.layer]} next={session.text} context={4} emptyText="No differences from the saved layer" />
    </section>
  </div>

  <footer>
    <div class="msg">
      <input bind:value={session.message} placeholder="Revision message (optional)" aria-label="Revision message" maxlength="200" />
    </div>
    <div class="actions">
      {#if session.restartCount > 0}
        <span class="hint"><RotateCw size={12} />{session.restartCount} running to restart</span>
      {/if}
      <button class="btn" onclick={() => (session.drawerOpen = false)}>Close</button>
      <button class="btn primary" disabled={blocked} onclick={() => void session.apply()}>
        {#if session.applying}<Spinner size={14} />{:else}<Save size={14} />{/if}
        Apply
      </button>
    </div>
  </footer>
</aside>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--bg-1);
    border-left: 1px solid var(--border);
    height: 100%;
    width: 100%;
  }
  header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 0 var(--space-4);
    height: 44px;
    flex: none;
    border-bottom: 1px solid var(--border);
    color: var(--text-1);
  }
  h3 {
    font-size: var(--fs-md);
    color: var(--text-0);
  }
  .grow {
    flex: 1;
    min-width: 0;
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .loading {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    color: var(--text-2);
    font-size: var(--fs-sm);
    padding: var(--space-4);
  }
  .banner {
    align-items: flex-start;
  }
  .banner .grow {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .banner :global(svg) {
    margin-top: 2px;
    flex: none;
  }
  .issue {
    font-size: var(--fs-xs);
    color: var(--err);
  }
  section {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    flex: none;
  }
  .section-title {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  h4 {
    font-size: var(--fs-xs);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-2);
  }
  .none {
    color: var(--text-2);
    font-size: var(--fs-sm);
    padding: var(--space-3) 0;
  }
  .changes {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .changes li {
    display: flex;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-2);
  }
  .mark {
    flex: none;
    width: 22px;
    height: 22px;
    border-radius: 6px;
    display: grid;
    place-items: center;
    background: var(--bg-3);
    color: var(--text-1);
  }
  .mark.ok {
    background: color-mix(in srgb, var(--ok) 16%, transparent);
    color: var(--ok);
  }
  .mark.err {
    background: color-mix(in srgb, var(--err) 16%, transparent);
    color: var(--err);
  }
  .mark.warn {
    background: color-mix(in srgb, var(--warn) 16%, transparent);
    color: var(--warn);
  }
  .mark.info {
    background: color-mix(in srgb, var(--info) 16%, transparent);
    color: var(--info);
  }
  .what {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  .line {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  .fields {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
  }
  .field {
    font-size: var(--fs-xs);
    padding: 0 6px;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-1);
    line-height: 1.7;
  }
  footer {
    flex: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4);
    border-top: 1px solid var(--border);
    background: var(--bg-1);
  }
  .msg input {
    width: 100%;
  }
  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-3);
  }
  .hint {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-right: auto;
    font-size: var(--fs-xs);
    color: var(--warn);
  }
</style>
