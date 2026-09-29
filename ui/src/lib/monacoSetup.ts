import * as monaco from 'monaco-editor/esm/vs/editor/editor.api';
import 'monaco-editor/esm/vs/editor/editor.all';
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution';
import { configureMonacoYaml } from 'monaco-yaml';
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
import YamlWorker from './yamlWorker?worker';

let configured = false;
let colorCtx: CanvasRenderingContext2D | null = null;

export function ensureMonacoConfigured(schema: unknown): void {
  if (configured) return;
  configured = true;

  self.MonacoEnvironment = {
    getWorker(_moduleId: string, label: string) {
      if (label === 'yaml') return new YamlWorker();
      return new EditorWorker();
    }
  };

  configureMonacoYaml(monaco, {
    enableSchemaRequest: false,
    hover: true,
    completion: true,
    validate: true,
    schemas: [
      {
        uri: 'inmemory://agent-runtime-v3-schema.json',
        fileMatch: ['*'],
        schema: schema as Record<string, unknown>
      }
    ]
  });
}

export function isMonacoConfigured(): boolean {
  return configured;
}

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function hex(value: string, alpha = 1): string {
  if (!colorCtx) {
    const canvas = document.createElement('canvas');
    canvas.width = 1;
    canvas.height = 1;
    colorCtx = canvas.getContext('2d', { willReadFrequently: true });
  }
  if (!colorCtx) return '#888888';
  colorCtx.clearRect(0, 0, 1, 1);
  colorCtx.fillStyle = '#888888';
  colorCtx.fillStyle = value;
  colorCtx.fillRect(0, 0, 1, 1);
  const d = colorCtx.getImageData(0, 0, 1, 1).data;
  const h = (n: number | undefined) => (n ?? 0).toString(16).padStart(2, '0');
  const a = Math.round(((d[3] ?? 255) / 255) * alpha * 255);
  return `#${h(d[0])}${h(d[1])}${h(d[2])}${a >= 255 ? '' : h(a)}`;
}

export function isLightTheme(): boolean {
  return document.documentElement.dataset['theme'] === 'light';
}

export function monoFontFamily(): string {
  return cssVar('--font-mono') || 'monospace';
}

export function applyMonacoTheme(): string {
  const light = isLightTheme();
  const name = light ? 'ar-light' : 'ar-dark';
  const v = (n: string, a = 1) => hex(cssVar(n), a);
  const c = (n: string) => v(n).replace('#', '');

  monaco.editor.defineTheme(name, {
    base: light ? 'vs' : 'vs-dark',
    inherit: true,
    rules: [
      { token: '', foreground: c('--text-0') },
      { token: 'comment', foreground: c('--text-2'), fontStyle: 'italic' },
      { token: 'type', foreground: c('--info') },
      { token: 'string', foreground: c('--ok') },
      { token: 'string.yaml', foreground: c('--ok') },
      { token: 'number', foreground: c('--warn') },
      { token: 'keyword', foreground: c('--ansi-13') },
      { token: 'operators', foreground: c('--text-1') },
      { token: 'delimiter', foreground: c('--text-2') },
      { token: 'tag', foreground: c('--accent') },
      { token: 'meta.tag', foreground: c('--accent') },
      { token: 'namespace', foreground: c('--ansi-6') },
      { token: 'string.escape', foreground: c('--warn') }
    ],
    colors: {
      'editor.background': v('--bg-1'),
      'editor.foreground': v('--text-0'),
      'editorLineNumber.foreground': v('--text-2'),
      'editorLineNumber.activeForeground': v('--text-1'),
      'editor.lineHighlightBackground': v('--text-0', 0.045),
      'editor.lineHighlightBorder': '#00000000',
      'editor.selectionBackground': v('--accent', 0.28),
      'editor.inactiveSelectionBackground': v('--accent', 0.16),
      'editor.selectionHighlightBackground': v('--accent', 0.14),
      'editor.wordHighlightBackground': v('--accent', 0.14),
      'editor.findMatchBackground': v('--warn', 0.35),
      'editor.findMatchHighlightBackground': v('--warn', 0.2),
      'editorCursor.foreground': v('--accent'),
      'editorIndentGuide.background1': v('--border'),
      'editorIndentGuide.activeBackground1': v('--border-strong'),
      'editorWhitespace.foreground': v('--border'),
      'editorRuler.foreground': v('--border'),
      'editorGutter.background': v('--bg-1'),
      'editorOverviewRuler.border': '#00000000',
      'editorError.foreground': v('--err'),
      'editorWarning.foreground': v('--warn'),
      'editorInfo.foreground': v('--info'),
      'editorWidget.background': v('--bg-2'),
      'editorWidget.border': v('--border-strong'),
      'editorWidget.foreground': v('--text-0'),
      'editorSuggestWidget.background': v('--bg-2'),
      'editorSuggestWidget.border': v('--border-strong'),
      'editorSuggestWidget.foreground': v('--text-0'),
      'editorSuggestWidget.selectedBackground': v('--accent', 0.2),
      'editorSuggestWidget.highlightForeground': v('--accent'),
      'editorHoverWidget.background': v('--bg-2'),
      'editorHoverWidget.border': v('--border-strong'),
      'editorHoverWidget.foreground': v('--text-0'),
      'editorHoverWidget.statusBarBackground': v('--bg-3'),
      'editorMarkerNavigation.background': v('--bg-2'),
      'input.background': v('--bg-0'),
      'input.foreground': v('--text-0'),
      'input.border': v('--border-strong'),
      'inputOption.activeBorder': v('--accent'),
      'focusBorder': v('--accent', 0.6),
      'list.hoverBackground': v('--text-0', 0.06),
      'list.activeSelectionBackground': v('--accent', 0.2),
      'list.activeSelectionForeground': v('--text-0'),
      'list.focusBackground': v('--accent', 0.2),
      'list.highlightForeground': v('--accent'),
      'scrollbar.shadow': '#00000000',
      'scrollbarSlider.background': v('--text-2', 0.25),
      'scrollbarSlider.hoverBackground': v('--text-2', 0.4),
      'scrollbarSlider.activeBackground': v('--text-2', 0.55),
      'editorBracketMatch.background': v('--accent', 0.18),
      'editorBracketMatch.border': v('--accent', 0.5),
      'editorStickyScroll.background': v('--bg-1'),
      'menu.background': v('--bg-2'),
      'menu.foreground': v('--text-0'),
      'menu.selectionBackground': v('--accent', 0.2),
      'menu.separatorBackground': v('--border')
    }
  });
  monaco.editor.setTheme(name);
  return name;
}

export function watchMonacoTheme(onChange?: () => void): () => void {
  const observer = new MutationObserver(() => {
    applyMonacoTheme();
    onChange?.();
  });
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
  return () => observer.disconnect();
}

export function refreshMonacoFonts(): void {
  void document.fonts.ready.then(() => monaco.editor.remeasureFonts());
}

export { monaco };
