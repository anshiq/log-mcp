<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import ShieldCheck from '@lucide/svelte/icons/shield-check';
  import GitCompare from '@lucide/svelte/icons/git-compare';
  import Save from '@lucide/svelte/icons/save';
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
  import AlignLeft from '@lucide/svelte/icons/align-left';
  import Lock from '@lucide/svelte/icons/lock';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import ChevronUp from '@lucide/svelte/icons/chevron-up';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import FileCode from '@lucide/svelte/icons/file-code';
  import Sparkles from '@lucide/svelte/icons/sparkles';
  import Spinner from '../lib/ui/Spinner.svelte';
  import RelativeTime from '../lib/ui/RelativeTime.svelte';
  import PlanPanel from './config/PlanPanel.svelte';
  import { ConfigService } from '../lib/api';
  import { applyMonacoTheme, ensureMonacoConfigured, isMonacoConfigured, monaco, monoFontFamily, refreshMonacoFonts, watchMonacoTheme } from '../lib/monacoSetup';
  import type { ConfigSession, ValidationIssue } from '../lib/config/session.svelte';
  import { toastError } from '../lib/toasts.svelte';

  let { session }: { session: ConfigSession } = $props();

  let host: HTMLDivElement | null = $state(null);
  let editor: ReturnType<typeof monaco.editor.create> | null = null;
  let ready = $state(false);
  let suppress = false;
  let problemsOpen = $state(false);
  let cursor = $state({ line: 1, col: 1 });
  let disposers: (() => void)[] = [];

  const problemCount = $derived(session.errors.length + session.validationWarnings.length);
  const layerPath = $derived(session.layerInfo?.path || `${session.layer} layer`);
  const canApply = $derived(session.writable && !session.empty && session.dirty && !session.invalid && !session.applying && !session.validating);
  const canPreview = $derived(session.writable && !session.empty && !session.invalid);

  function syncFromSession() {
    if (!editor) return;
    const model = editor.getModel();
    if (!model) return;
    if (model.getValue() !== session.text) {
      suppress = true;
      editor.setValue(session.text);
      suppress = false;
    }
    editor.updateOptions({ readOnly: !session.writable });
  }

  onMount(() => {
    let disposed = false;
    void (async () => {
      try {
        if (!isMonacoConfigured()) {
          const res = await ConfigService.schema();
          ensureMonacoConfigured(res.schema);
        }
      } catch (err) {
        toastError(err);
      }
      if (disposed || !host) return;
      const theme = applyMonacoTheme();
      editor = monaco.editor.create(host, {
        value: session.text,
        language: 'yaml',
        theme,
        automaticLayout: true,
        minimap: { enabled: false },
        fontSize: 13,
        lineHeight: 21,
        fontFamily: monoFontFamily(),
        fontLigatures: false,
        tabSize: 2,
        insertSpaces: true,
        detectIndentation: false,
        scrollBeyondLastLine: false,
        renderLineHighlight: 'line',
        padding: { top: 12, bottom: 12 },
        lineNumbersMinChars: 3,
        glyphMargin: false,
        folding: true,
        stickyScroll: { enabled: false },
        overviewRulerBorder: false,
        overviewRulerLanes: 2,
        bracketPairColorization: { enabled: false },
        guides: { indentation: true, bracketPairs: false },
        scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10, useShadows: false },
        smoothScrolling: true,
        fixedOverflowWidgets: true,
        readOnly: !session.writable,
        ariaLabel: 'Config YAML editor'
      });
      const e = editor;
      disposers.push(() => e.dispose());
      const change = e.onDidChangeModelContent(() => {
        if (suppress) return;
        session.setText(e.getValue());
      });
      disposers.push(() => change.dispose());
      const cur = e.onDidChangeCursorPosition((ev) => {
        cursor = { line: ev.position.lineNumber, col: ev.position.column };
      });
      disposers.push(() => cur.dispose());
      e.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => void session.apply());
      disposers.push(watchMonacoTheme());
      refreshMonacoFonts();
      ready = true;
    })();
    return () => {
      disposed = true;
    };
  });

  onDestroy(() => {
    for (const d of disposers) d();
    disposers = [];
    editor = null;
  });

  $effect(() => {
    void session.version;
    void session.layer;
    void session.writable;
    if (ready) syncFromSession();
  });

  $effect(() => {
    const errs = session.errors;
    const warns = session.validationWarnings;
    if (!ready || !editor) return;
    const model = editor.getModel();
    if (!model) return;
    const markers: import('monaco-editor/esm/vs/editor/editor.api').editor.IMarkerData[] = errs.map((er) => {
      const line = Math.min(Math.max(1, er.line), model.getLineCount());
      const word = model.getWordAtPosition({ lineNumber: line, column: Math.min(er.column, model.getLineMaxColumn(line)) });
      const start = Math.min(er.column, model.getLineMaxColumn(line));
      return {
        severity: monaco.MarkerSeverity.Error,
        message: er.path ? `${er.message} (${er.path})` : er.message,
        startLineNumber: line,
        startColumn: word ? word.startColumn : start,
        endLineNumber: line,
        endColumn: word ? word.endColumn : model.getLineMaxColumn(line),
        source: 'agent-runtime'
      };
    });
    void warns;
    monaco.editor.setModelMarkers(model, 'agent-runtime', markers);
  });

  function jump(issue: ValidationIssue) {
    if (!editor) return;
    editor.revealLineInCenter(issue.line);
    editor.setPosition({ lineNumber: issue.line, column: issue.column });
    editor.focus();
  }

  function format() {
    editor?.getAction('editor.action.formatDocument')?.run();
  }

  async function validate() {
    const ok = await session.validateNow();
    if (!ok && session.errors.length > 0) problemsOpen = true;
  }
</script>

<div class="editor" role="group" aria-label="Config editor">
  <div class="toolbar-row">
    <div class="file" title={layerPath}>
      <FileCode size={15} />
      <span class="path mono"><bdi>{layerPath}</bdi></span>
      {#if !session.layerInfo?.exists && session.writable}
        <span class="badge">new file</span>
      {/if}
      {#if !session.writable}
        <span class="badge warn"><Lock size={11} />read-only</span>
      {/if}
      {#if session.dirty}
        <span class="unsaved" title="Unsaved changes"><span class="dot warn"></span><span class="lbl">Unsaved</span></span>
      {/if}
    </div>

    <div class="actions">
      <button class="btn ghost" onclick={format} disabled={!ready || !session.writable} title="Format document">
        <AlignLeft size={15} /><span class="lbl">Format</span>
      </button>
      <button class="btn" onclick={validate} disabled={session.empty || session.validating} title="Validate against the schema">
        {#if session.validating}<Spinner size={14} />{:else}<ShieldCheck size={15} />{/if}<span class="lbl">Validate</span>
      </button>
      <button class="btn" class:active={session.drawerOpen} onclick={() => (session.drawerOpen ? (session.drawerOpen = false) : void session.preview())} disabled={!canPreview} title="Preview planned changes">
        <GitCompare size={15} /><span class="lbl">Preview changes</span>
      </button>
      <button class="btn ghost" onclick={() => session.discard()} disabled={!session.dirty} title="Discard unsaved edits">
        <RotateCcw size={15} /><span class="lbl">Discard</span>
      </button>
      <button class="btn primary" onclick={() => void session.apply()} disabled={!canApply} title="Apply this config (Ctrl+S)">
        {#if session.applying}<Spinner size={14} />{:else}<Save size={15} />{/if}<span>Apply</span>
      </button>
    </div>
  </div>

  <div class="main">
    <div class="monaco-wrap">
      <div class="monaco" bind:this={host}></div>
      {#if !ready}
        <div class="loading"><Spinner size={18} /> Loading editor…</div>
      {:else if session.empty && session.writable}
        <div class="starter">
          <div class="glyph"><Sparkles size={18} /></div>
          <strong>This layer is empty</strong>
          <span>Start typing YAML, or begin with a minimal config that defines one app.</span>
          <button class="btn sm" onclick={() => session.insertStarter()}>Insert starter config</button>
        </div>
      {/if}
    </div>
    {#if session.drawerOpen}
      <div class="drawer">
        <PlanPanel {session} />
      </div>
    {/if}
  </div>

  {#if problemsOpen && problemCount > 0}
    <div class="problems" role="region" aria-label="Problems">
      <ul>
        {#each session.errors as e, i (i)}
          <li>
            <button class="issue" onclick={() => jump(e)}>
              <CircleAlert size={14} class="ic err" />
              <span class="pos mono">Ln {e.line}:{e.column}</span>
              <span class="msg">{e.message}</span>
              {#if e.path}<span class="ipath mono">{e.path}</span>{/if}
            </button>
          </li>
        {/each}
        {#each session.validationWarnings as w, i (i)}
          <li>
            <div class="issue warning">
              <TriangleAlert size={14} class="ic warn" />
              <span class="msg">{w}</span>
            </div>
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  <div class="status" role="status">
    <div class="left">
      {#if session.validating}
        <span class="s muted"><Spinner size={12} />Validating…</span>
      {:else if session.empty}
        <span class="s muted">Empty</span>
      {:else if session.invalid}
        <button class="s bad" onclick={() => (problemsOpen = !problemsOpen)} aria-expanded={problemsOpen}>
          <CircleAlert size={13} />{session.errors.length} error{session.errors.length === 1 ? '' : 's'}
          {#if problemsOpen}<ChevronDown size={12} />{:else}<ChevronUp size={12} />{/if}
        </button>
      {:else if session.valid}
        <span class="s good"><CircleCheck size={13} />Valid</span>
      {:else}
        <span class="s muted">Not validated</span>
      {/if}
      {#if session.validationWarnings.length > 0}
        <button class="s warn" onclick={() => (problemsOpen = !problemsOpen)}>
          <TriangleAlert size={13} />{session.validationWarnings.length} warning{session.validationWarnings.length === 1 ? '' : 's'}
        </button>
      {/if}
      <span class="sep"></span>
      {#if session.dirty}
        <span class="s warn"><span class="dot warn"></span>Unsaved changes</span>
      {:else}
        <span class="s muted">All changes saved</span>
      {/if}
    </div>
    <div class="right">
      <span class="s muted mono">Ln {cursor.line}, Col {cursor.col}</span>
      <span class="s muted">YAML</span>
      <span class="s muted">
        {#if session.baseRevision > 0}
          Revision <span class="mono strong">{session.baseRevision}</span>
        {:else}
          No revisions yet
        {/if}
      </span>
      {#if session.lastApplied}
        <span class="s good">Applied rev {session.lastApplied.revision} · <RelativeTime ts={session.lastApplied.at} /></span>
      {/if}
    </div>
  </div>
</div>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
    container-type: inline-size;
  }
  .toolbar-row {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-3) var(--space-4);
    background: var(--bg-2);
    border-bottom: 1px solid var(--border);
    flex: none;
  }
  .file {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    flex: 1;
    color: var(--text-1);
  }
  .path {
    color: var(--text-1);
    font-size: var(--fs-sm);
    direction: rtl;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .unsaved {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--warn);
    font-size: var(--fs-xs);
    font-weight: 500;
    flex: none;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: none;
  }
  .btn.active {
    background: var(--accent-subtle);
    border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  }
  .btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .main {
    flex: 1;
    min-height: 0;
    display: flex;
    position: relative;
  }
  .monaco-wrap {
    position: relative;
    flex: 1;
    min-width: 0;
    min-height: 0;
  }
  .monaco {
    position: absolute;
    inset: 0;
  }
  .loading {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    color: var(--text-2);
    background: var(--bg-1);
    font-size: var(--fs-sm);
  }
  .starter {
    position: absolute;
    top: 52px;
    left: 72px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    max-width: 320px;
    padding: var(--space-4) var(--space-5);
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--bg-2) 92%, transparent);
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .starter strong {
    color: var(--text-0);
    font-size: var(--fs-md);
  }
  .starter .btn {
    margin-top: var(--space-2);
  }
  .glyph {
    width: 32px;
    height: 32px;
    border-radius: 9px;
    display: grid;
    place-items: center;
    background: var(--accent-subtle);
    color: var(--accent);
    margin-bottom: 2px;
  }
  .drawer {
    width: min(460px, 46%);
    flex: none;
    min-height: 0;
    display: flex;
  }
  .problems {
    flex: none;
    max-height: 168px;
    overflow: auto;
    border-top: 1px solid var(--border);
    background: var(--bg-2);
  }
  .problems ul {
    list-style: none;
    margin: 0;
    padding: var(--space-2) 0;
  }
  .issue {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
    width: 100%;
    padding: 5px var(--space-4);
    border: none;
    border-radius: 0;
    background: transparent;
    text-align: left;
    color: var(--text-0);
    font-size: var(--fs-sm);
    cursor: pointer;
  }
  .issue:hover {
    background: var(--bg-hover);
  }
  .issue.warning {
    cursor: default;
  }
  .issue :global(.ic) {
    flex: none;
    align-self: center;
  }
  .issue :global(.ic.err) {
    color: var(--err);
  }
  .issue :global(.ic.warn) {
    color: var(--warn);
  }
  .pos {
    color: var(--text-2);
    font-size: var(--fs-xs);
    flex: none;
    min-width: 64px;
  }
  .msg {
    flex: 1;
    min-width: 0;
  }
  .ipath {
    color: var(--text-2);
    font-size: var(--fs-xs);
    flex: none;
  }
  .status {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    height: 30px;
    padding: 0 var(--space-4);
    border-top: 1px solid var(--border);
    background: var(--bg-2);
    font-size: var(--fs-xs);
    overflow: hidden;
  }
  .left,
  .right {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    min-width: 0;
    white-space: nowrap;
  }
  .s {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: none;
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    font-size: var(--fs-xs);
  }
  button.s {
    cursor: pointer;
  }
  button.s:hover {
    background: var(--bg-hover);
  }
  .s.good {
    color: var(--ok);
  }
  .s.bad {
    color: var(--err);
    font-weight: 600;
  }
  .s.warn {
    color: var(--warn);
  }
  .s.muted {
    color: var(--text-2);
  }
  .strong {
    color: var(--text-0);
  }
  .sep {
    width: 1px;
    height: 14px;
    background: var(--border);
  }
  @container (max-width: 860px) {
    .lbl {
      display: none;
    }
    .drawer {
      position: absolute;
      inset: 0 0 0 auto;
      width: min(460px, 100%);
      box-shadow: var(--shadow-pop);
      z-index: 5;
    }
    .right .s:not(:last-child):not(:nth-last-child(2)) {
      display: none;
    }
  }
</style>
