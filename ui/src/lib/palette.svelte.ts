import type { Component } from 'svelte';
import LayoutDashboard from '@lucide/svelte/icons/layout-dashboard';
import Terminal from '@lucide/svelte/icons/terminal';
import ScrollText from '@lucide/svelte/icons/scroll-text';
import Boxes from '@lucide/svelte/icons/boxes';
import FileCog from '@lucide/svelte/icons/file-cog';
import History from '@lucide/svelte/icons/history';
import Activity from '@lucide/svelte/icons/activity';
import Users from '@lucide/svelte/icons/users';
import ShieldCheck from '@lucide/svelte/icons/shield-check';
import FolderKanban from '@lucide/svelte/icons/folder-kanban';
import Plug from '@lucide/svelte/icons/plug';
import Settings from '@lucide/svelte/icons/settings';
import Sun from '@lucide/svelte/icons/sun';
import Moon from '@lucide/svelte/icons/moon';
import FolderPlus from '@lucide/svelte/icons/folder-plus';
import FolderOpen from '@lucide/svelte/icons/folder-open';
import Keyboard from '@lucide/svelte/icons/keyboard';
import Layers from '@lucide/svelte/icons/layers';
import Cpu from '@lucide/svelte/icons/cpu';
import { router } from './router.svelte';
import { processes } from './state/processes.svelte';
import { scopeState } from './state/scope.svelte';
import { prefs } from './state/prefs.svelte';
import { processDisplayName } from './format';

export type PaletteGroup = 'go to' | 'action' | 'workspace' | 'process';

type Icon = Component<{ size?: number; strokeWidth?: number }>;

export interface PaletteItem {
  id: string;
  title: string;
  hint?: string;
  group: PaletteGroup;
  icon?: Icon;
  keys?: string;
  keywords?: string;
  run: () => void;
}

export const GROUP_LABEL: Record<PaletteGroup, string> = {
  'go to': 'Pages',
  action: 'Actions',
  workspace: 'Workspaces',
  process: 'Processes'
};

const GROUP_ORDER: PaletteGroup[] = ['go to', 'action', 'workspace', 'process'];

const ROUTES: { path: string; title: string; icon: Icon; keys?: string; keywords?: string }[] = [
  { path: '/', title: 'Overview', icon: LayoutDashboard, keys: 'g o', keywords: 'home dashboard' },
  { path: '/processes', title: 'Processes', icon: Terminal, keys: 'g p', keywords: 'running apps' },
  { path: '/logs', title: 'Logs', icon: ScrollText, keys: 'g l', keywords: 'output stdout stderr' },
  { path: '/apps', title: 'Apps', icon: Boxes, keys: 'g a', keywords: 'definitions' },
  { path: '/config', title: 'Config', icon: FileCog, keys: 'g c', keywords: 'yaml agent-runtime.yaml' },
  { path: '/config/revisions', title: 'Config Revisions', icon: History, keywords: 'history rollback' },
  { path: '/events', title: 'Events', icon: Activity, keys: 'g e', keywords: 'activity stream' },
  { path: '/sessions', title: 'Sessions', icon: Users, keys: 'g s', keywords: 'clients agents' },
  { path: '/audit', title: 'Audit', icon: ShieldCheck, keys: 'g u', keywords: 'log trail' },
  { path: '/projects', title: 'Projects', icon: FolderKanban, keys: 'g j', keywords: 'workspaces folders' },
  { path: '/integrations', title: 'Integrations', icon: Plug, keys: 'g i', keywords: 'mcp harness skills claude codex' },
  { path: '/settings', title: 'Settings', icon: Settings, keys: 'g t', keywords: 'preferences daemon' }
];

export function matchRange(text: string, q: string): [number, number] | null {
  const s = q.trim().toLowerCase();
  if (!s) return null;
  const i = text.toLowerCase().indexOf(s);
  return i < 0 ? null : [i, i + s.length];
}

export function highlight(text: string, q: string): { text: string; match: boolean }[] {
  const r = matchRange(text, q);
  if (!r) return [{ text, match: false }];
  return [
    { text: text.slice(0, r[0]), match: false },
    { text: text.slice(r[0], r[1]), match: true },
    { text: text.slice(r[1]), match: false }
  ].filter((p) => p.text);
}

function score(item: PaletteItem, q: string): number {
  if (!q) return 0;
  const title = item.title.toLowerCase();
  if (title === q) return 0;
  if (title.startsWith(q)) return 1;
  if (title.split(/[\s/._-]+/).some((w) => w.startsWith(q))) return 2;
  if (title.includes(q)) return 3;
  return 4;
}

function matches(item: PaletteItem, q: string): boolean {
  if (!q) return true;
  return [item.title, item.hint ?? '', item.keywords ?? ''].some((f) => f.toLowerCase().includes(q));
}

function resolvedTheme(): 'light' | 'dark' {
  if (prefs.data.theme !== 'system') return prefs.data.theme;
  return typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

class Palette {
  open = $state(false);
  query = $state('');
  activeIndex = $state(0);
  wantsOpenWorkspace = $state(false);

  show() {
    this.open = true;
    this.query = '';
    this.activeIndex = 0;
    return Promise.resolve();
  }

  hide() {
    this.open = false;
  }

  requestOpenWorkspace() {
    this.wantsOpenWorkspace = true;
    void router.navigate('/projects');
  }

  private actions(): PaletteItem[] {
    const light = resolvedTheme() === 'light';
    const list: PaletteItem[] = [
      {
        id: 'action:theme',
        title: light ? 'Switch to dark theme' : 'Switch to light theme',
        hint: 'Toggle theme',
        group: 'action',
        icon: light ? Moon : Sun,
        keywords: 'toggle theme dark light appearance',
        run: () => prefs.set('theme', light ? 'dark' : 'light')
      },
      {
        id: 'action:open-workspace',
        title: 'Open workspace…',
        hint: 'Register a project folder',
        group: 'action',
        icon: FolderPlus,
        keywords: 'add project folder path',
        run: () => this.requestOpenWorkspace()
      },
      {
        id: 'action:shortcuts',
        title: 'Keyboard shortcuts',
        group: 'action',
        icon: Keyboard,
        keys: '?',
        keywords: 'help keys bindings',
        run: () => window.dispatchEvent(new CustomEvent('ar:shortcuts'))
      }
    ];
    if (!scopeState.all) {
      list.push({
        id: 'action:all-workspaces',
        title: 'Show all workspaces',
        hint: 'Clear workspace scope',
        group: 'action',
        icon: Layers,
        keywords: 'scope everything',
        run: () => scopeState.select('')
      });
    }
    return list;
  }

  items(): PaletteItem[] {
    const q = this.query.trim().toLowerCase();

    const routeItems: PaletteItem[] = ROUTES.map((r) => ({
      id: `route:${r.path}`,
      title: r.title,
      hint: r.path === '/' ? 'home' : r.path,
      group: 'go to' as const,
      icon: r.icon,
      keys: r.keys,
      keywords: r.keywords,
      run: () => void router.navigate(r.path)
    }));

    const workspaceItems: PaletteItem[] = scopeState.workspaces.map((w) => ({
      id: `workspace:${w.id}`,
      title: scopeState.workspaceLabel(w.id),
      hint: w.path,
      group: 'workspace' as const,
      icon: FolderOpen,
      keywords: 'switch scope workspace',
      run: () => scopeState.select(w.id)
    }));

    const processItems: PaletteItem[] = processes.list.map((p) => ({
      id: `process:${p.id}`,
      title: processDisplayName(p) || p.id,
      hint: `${p.status} · ${p.id}`,
      group: 'process' as const,
      icon: Cpu,
      keywords: `${p.command} ${p.id} ${p.pid}`,
      run: () => void router.navigate(`/processes/${p.id}`)
    }));

    const all = [...routeItems, ...this.actions(), ...workspaceItems, ...(q ? processItems : processItems.slice(0, 5))];
    const filtered = all.filter((i) => matches(i, q));
    const limited = filtered.filter((i) => i.group !== 'process').concat(filtered.filter((i) => i.group === 'process').slice(0, 12));
    return GROUP_ORDER.flatMap((g) => {
      const inGroup = limited.filter((i) => i.group === g);
      return q ? inGroup.sort((a, b) => score(a, q) - score(b, q)) : inGroup;
    });
  }
}

export const palette = new Palette();
