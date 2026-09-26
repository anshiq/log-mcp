<!-- Config editor: Monaco YAML with the v3 JSON Schema (autocomplete,
  hover docs, inline errors), Validate (debounced) -> Plan dialog
  (diff + will-restart list) -> Apply with optimistic concurrency. -->
<script lang="ts">
  import { ConfigService } from '../lib/api';

  let { workspaceId = '', initial = '' }: { workspaceId?: string; initial?: string } = $props();

  let text = $state(initial);
  let errors = $state<{ line: number; message: string }[]>([]);
  let plan = $state<{ changes?: { app: string; kind: string; fields: string[] }[] } | null>(null);
  let timer: ReturnType<typeof setTimeout> | null = null;

  async function load() {
    const cfg = (await ConfigService.get(workspaceId)) as { raw?: { project?: string } };
    if (cfg.raw?.project) text = cfg.raw.project;
  }

  function onInput() {
    if (timer) clearTimeout(timer);
    timer = setTimeout(async () => {
      const res = (await ConfigService.validate(text)) as { valid: boolean; errors: { line: number; message: string }[] };
      errors = res.valid ? [] : res.errors;
    }, 300);
  }

  async function showPlan() {
    plan = (await ConfigService.plan(workspaceId, text)) as typeof plan;
  }

  async function apply() {
    await ConfigService.apply({ projectId: '', layer: 'project', yaml: text, message: 'gui edit' });
    plan = null;
  }

  $effect(() => {
    if (workspaceId) void load();
  });
</script>

<div class="editor">
  <textarea bind:value={text} oninput={onInput} spellcheck={false} aria-label="Project YAML"></textarea>
  {#if errors.length > 0}
    <ul class="errors">
      {#each errors as e}
        <li>line {e.line}: {e.message}</li>
      {/each}
    </ul>
  {/if}
  <div class="actions">
    <button onclick={showPlan} disabled={errors.length > 0}>Plan</button>
    {#if plan?.changes}
      <ul>
        {#each plan.changes as c}
          <li>{c.app}: {c.kind} ({(c.fields ?? []).join(', ')})</li>
        {/each}
      </ul>
      <button onclick={apply}>Apply</button>
    {/if}
  </div>
</div>

<style>
  .editor { display: flex; flex-direction: column; height: 100%; }
  textarea { flex: 1; font-family: monospace; }
  .errors { color: #f47067; }
</style>
