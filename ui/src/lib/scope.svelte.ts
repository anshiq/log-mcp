import { ProjectService } from './api';

const LAST_PATH_KEY = 'ar.lastWorkspacePath';

class Scope {
  workspaceId = $state('');
  projectId = $state('');
  projectName = $state('');
  workspacePath = $state('');
  error = $state('');
  resolving = $state(false);

  async resolve(path: string) {
    const trimmed = path.trim();
    if (!trimmed) return;
    this.resolving = true;
    this.error = '';
    try {
      const res = await ProjectService.resolve(trimmed);
      this.workspaceId = res.workspaceId;
      this.projectId = res.projectId;
      this.projectName = res.projectName || trimmed;
      this.workspacePath = trimmed;
      try {
        localStorage.setItem(LAST_PATH_KEY, trimmed);
      } catch {
      }
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
    } finally {
      this.resolving = false;
    }
  }

  restoreLast() {
    let last: string | null = null;
    try {
      last = localStorage.getItem(LAST_PATH_KEY);
    } catch {
    }
    if (last) void this.resolve(last);
  }

  clear() {
    this.workspaceId = '';
    this.projectId = '';
    this.projectName = '';
    this.workspacePath = '';
  }
}

export const scope = new Scope();
