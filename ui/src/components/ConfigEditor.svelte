<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { ConfigService } from '../lib/api';
  import { ensureMonacoConfigured, monaco } from '../lib/monacoSetup';

  let { workspaceId = '' }: { workspaceId?: string } = $props();

  let editorEl: HTMLDivElement | null = $state(null);
  let editor: ReturnType<typeof monaco.editor.create> | null = null;
  let modelListener: { dispose(): void } | null = null;

  let text = $state('');
  let projectId = $state('');
  let revision = $state(0);
  let currentWs = $state('');
  let editorReady = $state(false);
  let errors = $state<{ line: number; column?: number; path?: string; message: string }[]>([]);
  let plan = $state<{ changes?: { app: string; kind: string; fields: string[] }[]; errors?: unknown[] } | null>(null);
  let applying = $state(false);
  let planning = $state(false);
  let saved = $state(false);
  let loadError = $state('');
  let timer: ReturnType<typeof setTimeout> | null = null;

  async function load() {
    if (!workspaceId) return;
    loadError = '';
    try {
      const cfg = await ConfigService.get(workspaceId);
      projectId = String(cfg.projectId ?? '');
      revision = Number(cfg.revision ?? 0);
      const raw = cfg.raw as { project?: string } | undefined;
      text = raw?.project ?? '';
      editor?.setValue(text);
      currentWs = workspaceId;
      plan = null;
      saved = false;
    } catch (err) {
      loadError = err instanceof Error ? err.message : String(err);
    }
  }

  onMount(() => {
    editor = monaco.editor.create(editorEl!, {
      value: text,
      language: 'yaml',
      theme: 'vs-dark',
      automaticLayout: true,
      minimap: { enabled: false },
      fontSize: 13,
      fontFamily: 'JetBrains Mono, ui-monospace, monospace',
      scrollBeyondLastLine: false
    });
    modelListener = editor.onDidChangeModelContent(() => {
      text = editor!.getValue();
      saved = false;
      onInput();
    });
    void (async () => {
      const schemaRes = await ConfigService.schema();
      ensureMonacoConfigured(schemaRes.schema);
      editorReady = true;
      await load();
    })();
  });

  onDestroy(() => {
    modelListener?.dispose();
    editor?.dispose();
    if (timer) clearTimeout(timer);
  });

  $effect(() => {
    if (workspaceId && workspaceId !== currentWs && editorReady) void load();
  });

  function onInput() {
    if (timer) clearTimeout(timer);
    timer = setTimeout(async () => {
      const res = await ConfigService.validate(text);
      errors = res.valid ? [] : res.errors;
    }, 300);
  }

  async function showPlan() {
    planning = true;
    try {
      plan = await ConfigService.plan(workspaceId, text);
    } finally {
      planning = false;
    }
  }

  async function apply() {
    if (!projectId) return;
    applying = true;
    try {
      const res = await ConfigService.apply({
        projectId,
        layer: 'project',
        yaml: text,
        baseRevision: revision,
        message: 'edited from gui'
      });
      if (res.applied) {
        revision = res.revision ?? revision;
        plan = null;
        saved = true;
      } else if (res.errors) {
        errors = res.errors as typeof errors;
      }
    } finally {
      applying = false;
    }
  }

  function format() {
    editor?.getAction('editor.action.formatDocument')?.run();
  }
</script>

<div class="editor">
  <div class="toolbar">
    <span class="path">{projectId ? `project revision ${revision}` : 'no workspace selected'}</span>
    {#if saved}
      <span class="ok">Applied</span>
    {/if}
    <div class="spacer"></div>
    <button onclick={format} disabled={!editorReady}>Format</button>
    <button onclick={showPlan} disabled={errors.length > 0 || !workspaceId || planning}>
      {planning ? 'Planning…' : 'Plan'}
    </button>
  </div>
  {#if loadError}
    <div class="banner err">{loadError}</div>
  {/if}
  <div class="monaco" bind:this={editorEl}></div>
  {#if errors.length > 0}
    <ul class="errors">
      {#each errors as e}
        <li>line {e.line}{e.path ? ` (${e.path})` : ''}: {e.message}</li>
      {/each}
    </ul>
  {/if}
  {#if plan}
    <div class="plan">
      {#if plan.errors?.length}
        <ul class="errors">
          {#each plan.errors as e}
            <li>{JSON.stringify(e)}</li>
          {/each}
        </ul>
      {:else}
        <ul class="changes">
          {#each plan.changes ?? [] as c}
            <li class={c.kind}>
              <strong>{c.app}</strong> — {c.kind}
              {#if c.fields?.length}<span class="fields">({c.fields.join(', ')})</span>{/if}
            </li>
          {/each}
          {#if (plan.changes ?? []).length === 0}
            <li class="unchanged">No changes.</li>
          {/if}
        </ul>
        <button class="primary" onclick={apply} disabled={applying}>{applying ? 'Applying…' : 'Apply'}</button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-5);
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
  }
  .spacer {
    flex: 1;
  }
  .path {
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  .ok {
    color: var(--ok);
    font-size: var(--fs-sm);
  }
  .toolbar button {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-4);
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .toolbar button:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .banner {
    padding: var(--space-3) var(--space-5);
    font-size: var(--fs-sm);
  }
  .banner.err {
    background: color-mix(in srgb, var(--err) 15%, transparent);
    color: var(--err);
  }
  .monaco {
    flex: 1;
    min-height: 0;
  }
  .errors {
    list-style: none;
    margin: 0;
    padding: var(--space-3) var(--space-5);
    background: var(--bg-1);
    border-top: 1px solid var(--border);
    color: var(--err);
    font-size: var(--fs-sm);
    max-height: 140px;
    overflow: auto;
  }
  .plan {
    border-top: 1px solid var(--border);
    background: var(--bg-1);
    padding: var(--space-4) var(--space-5);
  }
  .changes {
    list-style: none;
    margin: 0 0 var(--space-4);
    padding: 0;
    font-size: var(--fs-sm);
  }
  .changes li {
    padding: var(--space-2) 0;
  }
  .changes .fields {
    color: var(--text-2);
  }
  .changes .restart-required {
    color: var(--warn);
  }
  .changes .removed {
    color: var(--err);
  }
  .changes .added {
    color: var(--ok);
  }
  .primary {
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-5);
    font-size: var(--fs-sm);
  }
  .primary:disabled {
    opacity: 0.6;
  }
</style>
