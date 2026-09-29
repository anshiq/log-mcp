import { scopeState } from './state/scope.svelte';

class Scope {
  get workspaceId(): string {
    return scopeState.workspaceId;
  }

  get projectId(): string {
    return scopeState.projectId;
  }

  get projectName(): string {
    return scopeState.projectName;
  }

  get workspacePath(): string {
    return scopeState.path;
  }

  get error(): string {
    return scopeState.error;
  }

  get resolving(): boolean {
    return scopeState.resolving;
  }

  async resolve(path: string) {
    await scopeState.open(path);
  }

  restoreLast() {
    void scopeState.refresh();
  }

  clear() {
    scopeState.select('');
  }
}

export const scope = new Scope();
