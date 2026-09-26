import { router } from './router.svelte';
import { ProcessService } from './api';

export interface PaletteItem {
  id: string;
  title: string;
  hint?: string;
  group: 'go to' | 'process' | 'action';
  run: () => void;
}

const ROUTES: { path: string; title: string }[] = [
  { path: '/processes', title: 'Processes' },
  { path: '/apps', title: 'Apps' },
  { path: '/config', title: 'Config' },
  { path: '/config/revisions', title: 'Config Revisions' },
  { path: '/events', title: 'Events' },
  { path: '/projects', title: 'Projects' },
  { path: '/sessions', title: 'Sessions' },
  { path: '/integrations', title: 'Integrations' },
  { path: '/audit', title: 'Audit' },
  { path: '/settings', title: 'Settings' }
];

class Palette {
  open = $state(false);
  query = $state('');
  processes = $state<{ id: string; command: string; status: string }[]>([]);
  activeIndex = $state(0);

  async show() {
    this.open = true;
    this.query = '';
    this.activeIndex = 0;
    try {
      const res = await ProcessService.list('', true);
      this.processes = (res.processes as Record<string, unknown>[]).map((p) => ({
        id: String(p.id ?? p.process_id ?? ''),
        command: String(p.command ?? ''),
        status: String(p.status ?? '')
      }));
    } catch {
      this.processes = [];
    }
  }

  hide() {
    this.open = false;
  }

  items(): PaletteItem[] {
    const q = this.query.trim().toLowerCase();
    const routeItems: PaletteItem[] = ROUTES.filter((r) => !q || r.title.toLowerCase().includes(q)).map((r) => ({
      id: `route:${r.path}`,
      title: r.title,
      group: 'go to',
      run: () => router.navigate(r.path)
    }));
    const processItems: PaletteItem[] = this.processes
      .filter((p) => !q || p.command.toLowerCase().includes(q) || p.id.toLowerCase().includes(q))
      .slice(0, 8)
      .map((p) => ({
        id: `process:${p.id}`,
        title: p.command || p.id,
        hint: `${p.status} · ${p.id}`,
        group: 'process',
        run: () => router.navigate(`/processes/${p.id}`)
      }));
    return [...routeItems, ...processItems];
  }
}

export const palette = new Palette();
