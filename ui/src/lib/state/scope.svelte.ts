import { ProjectService } from '../api';

export interface ScopeProject {
  id: string;
  name: string;
}

export interface ScopeWorkspace {
  id: string;
  projectId: string;
  path: string;
}

class ScopeState {
  projects = $state<ScopeProject[]>([]);
  workspaces = $state<ScopeWorkspace[]>([]);
  projectId = $state('');
  workspaceId = $state('');
  all = $state(true);

  get path(): string {
    if (this.all) return 'All workspaces';
    const ws = this.workspaces.find((w) => w.id === this.workspaceId);
    return ws?.path ?? '';
  }

  async refresh() {
    try {
      const list = await ProjectService.list();
      this.projects = (list as unknown as Record<string, unknown>[]).map((p) => ({
        id: String(p['id'] ?? ''),
        name: String(p['name'] ?? p['id'] ?? '')
      }));
      if (this.projects.length === 1 && !this.projectId) {
        const only = this.projects[0];
        if (only) {
          this.projectId = only.id;
          await this.loadWorkspaces(only.id);
          if (this.workspaces.length === 1) {
            const w = this.workspaces[0];
            if (w) {
              this.workspaceId = w.id;
              this.all = false;
            }
          }
        }
      }
      try {
        const saved = localStorage.getItem('ar.scope.v1');
        if (saved) {
          const s = JSON.parse(saved) as { projectId?: string; workspaceId?: string; all?: boolean };
          if (s.projectId) this.projectId = s.projectId;
          if (s.workspaceId) this.workspaceId = s.workspaceId;
          if (typeof s.all === 'boolean') this.all = s.all;
        }
      } catch {
      }
    } catch {
    }
  }

  async loadWorkspaces(projectId: string) {
    try {
      const wss = await ProjectService.workspaces(projectId);
      this.workspaces = (wss as unknown as Record<string, unknown>[]).map((w) => ({
        id: String(w['id'] ?? ''),
        projectId,
        path: String(w['path'] ?? '')
      }));
    } catch {
      this.workspaces = [];
    }
  }

  set(projectId: string, workspaceId: string, all: boolean) {
    this.projectId = projectId;
    this.workspaceId = workspaceId;
    this.all = all;
    try {
      localStorage.setItem('ar.scope.v1', JSON.stringify({ projectId, workspaceId, all }));
    } catch {
    }
  }
}

export const scopeState = new ScopeState();
