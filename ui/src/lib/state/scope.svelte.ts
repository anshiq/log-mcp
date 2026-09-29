import { ProjectService } from '../api';

export interface ScopeProject {
  id: string;
  name: string;
}

export interface ScopeWorkspace {
  id: string;
  projectId: string;
  path: string;
  missing: boolean;
}

const KEY = 'ar.scope.v2';
const LAST_PATH_KEY = 'ar.lastWorkspacePath';

function baseName(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

class ScopeState {
  projects = $state<ScopeProject[]>([]);
  workspaces = $state<ScopeWorkspace[]>([]);
  workspaceId = $state('');
  loaded = $state(false);
  resolving = $state(false);
  error = $state('');

  get all(): boolean {
    return this.workspaceId === '';
  }

  get workspace(): ScopeWorkspace | null {
    return this.workspaces.find((w) => w.id === this.workspaceId) ?? null;
  }

  get projectId(): string {
    return this.workspace?.projectId ?? '';
  }

  get projectName(): string {
    const p = this.projects.find((x) => x.id === this.projectId);
    return p?.name ?? '';
  }

  get path(): string {
    return this.workspace?.path ?? '';
  }

  get label(): string {
    if (this.all) return 'All workspaces';
    const ws = this.workspace;
    if (!ws) return 'All workspaces';
    return this.projectName || baseName(ws.path);
  }

  workspaceLabel(id: string): string {
    const ws = this.workspaces.find((w) => w.id === id);
    if (!ws) return id;
    const p = this.projects.find((x) => x.id === ws.projectId);
    return p?.name || baseName(ws.path);
  }

  workspacePath(id: string): string {
    return this.workspaces.find((w) => w.id === id)?.path ?? '';
  }

  async refresh() {
    try {
      const list = await ProjectService.list();
      const projects = (list as unknown as Record<string, unknown>[]).map((p) => ({
        id: String(p['id'] ?? p['projectId'] ?? ''),
        name: String(p['name'] ?? p['id'] ?? '')
      }));
      const groups = await Promise.all(
        projects.map(async (p) => {
          try {
            const wss = await ProjectService.workspaces(p.id);
            return (wss as unknown as Record<string, unknown>[]).map((w) => ({
              id: String(w['id'] ?? w['workspaceId'] ?? ''),
              projectId: p.id,
              path: String(w['path'] ?? ''),
              missing: w['missing'] === true
            }));
          } catch {
            return [] as ScopeWorkspace[];
          }
        })
      );
      this.projects = projects;
      this.workspaces = groups.flat();
      if (!this.loaded) this.restore();
      if (this.workspaceId && !this.workspaces.some((w) => w.id === this.workspaceId)) this.workspaceId = '';
      this.loaded = true;
    } catch {
      this.loaded = true;
    }
  }

  private restore() {
    let saved = '';
    try {
      const raw = localStorage.getItem(KEY);
      if (raw) saved = (JSON.parse(raw) as { workspaceId?: string }).workspaceId ?? '';
      else if (this.workspaces.length === 1) saved = this.workspaces[0]?.id ?? '';
    } catch {
      saved = '';
    }
    if (saved && this.workspaces.some((w) => w.id === saved)) this.workspaceId = saved;
  }

  select(workspaceId: string) {
    this.workspaceId = workspaceId;
    try {
      localStorage.setItem(KEY, JSON.stringify({ workspaceId }));
    } catch {
      return;
    }
  }

  async open(path: string): Promise<boolean> {
    const trimmed = path.trim();
    if (!trimmed) return false;
    this.resolving = true;
    this.error = '';
    try {
      const res = await ProjectService.resolve(trimmed);
      await this.refresh();
      this.select(res.workspaceId);
      try {
        localStorage.setItem(LAST_PATH_KEY, trimmed);
      } catch {
        return true;
      }
      return true;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      return false;
    } finally {
      this.resolving = false;
    }
  }
}

export const scopeState = new ScopeState();
