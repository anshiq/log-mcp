import { ProcessService } from '../api';
import { toProcess } from '../api/normalize';
import type { Process } from '../api/types';
import { scopeState } from './scope.svelte';

const RANK: Record<string, number> = { failed: 0, crashed: 0, starting: 1, running: 2, ready: 2, exited: 3, stopped: 3 };

function tally(list: Process[]): { running: number; failed: number; exited: number; starting: number } {
  let running = 0;
  let failed = 0;
  let exited = 0;
  let starting = 0;
  for (const p of list) {
    if (p.status === 'running' || p.status === 'ready') running++;
    else if (p.status === 'failed' || p.status === 'crashed') failed++;
    else if (p.status === 'starting') starting++;
    else exited++;
  }
  return { running, failed, exited, starting };
}

class Processes {
  map = $state(new Map<string, Process>());
  streamState = $state<'connecting' | 'open' | 'reconnecting' | 'closed'>('connecting');
  optimistic = $state(new Map<string, string>());
  lastEventAt = $state(new Map<string, number>());
  private closer: (() => void) | null = null;

  get list(): Process[] {
    return [...this.map.values()].sort((a, b) => {
      const ra = RANK[a.status] ?? 4;
      const rb = RANK[b.status] ?? 4;
      if (ra !== rb) return ra - rb;
      return (b.startedAt ?? 0) - (a.startedAt ?? 0) || a.command.localeCompare(b.command);
    });
  }

  get scoped(): Process[] {
    if (scopeState.all) return this.list;
    const ws = scopeState.workspaceId;
    return this.list.filter((p) => p.workspaceId === ws);
  }

  get counts(): { running: number; failed: number; exited: number; starting: number } {
    return tally(this.scoped);
  }

  get totals(): { running: number; failed: number; exited: number; starting: number } {
    return tally(this.list);
  }

  byWorkspace(ws: string): Process[] {
    return this.list.filter((p) => p.workspaceId === ws);
  }

  filtered(filter: string, status: string): Process[] {
    const q = filter.trim().toLowerCase();
    return this.list.filter((p) => {
      if (status !== 'all' && status !== '' && p.status !== status) return false;
      if (!q) return true;
      return p.command.toLowerCase().includes(q) || p.id.toLowerCase().includes(q) || String(p.pid).includes(q) || p.ports.some((x) => String(x).includes(q));
    });
  }

  connect(workspaceId = '', all = true) {
    this.disconnect();
    this.handleSnapshot(workspaceId, all);
    this.closer = ProcessService.watch(
      workspaceId,
      all,
      (msg) => {
        if (msg.kind === 'snapshot' && Array.isArray(msg['snapshot'])) {
          const next = new Map<string, Process>();
          for (const p of msg['snapshot'] as Record<string, unknown>[]) {
            const n = toProcess(p);
            next.set(n.id, n);
          }
          this.map = next;
        } else if (msg.kind === 'upsert' && msg['process']) {
          const n = toProcess(msg['process'] as Record<string, unknown>);
          const next = new Map(this.map);
          next.set(n.id, n);
          this.map = next;
          const le = new Map(this.lastEventAt);
          le.set(n.id, Date.now());
          this.lastEventAt = le;
        } else if (msg.kind === 'removed' && msg['processId']) {
          const next = new Map(this.map);
          next.delete(String(msg['processId']));
          this.map = next;
        } else if (msg.kind === 'gap') {
          void this.handleSnapshot(workspaceId, all);
        }
      },
      (s) => {
        this.streamState = s;
      },
      {
        onGap: () => void this.handleSnapshot(workspaceId, all),
        onSnapshot: (m) => {
          if (Array.isArray(m['snapshot'])) {
            const next = new Map<string, Process>();
            for (const p of m['snapshot'] as Record<string, unknown>[]) {
              const n = toProcess(p);
              next.set(n.id, n);
            }
            this.map = next;
          }
        }
      }
    ).close;
  }

  async handleSnapshot(workspaceId: string, all: boolean) {
    try {
      const arr = await ProcessService.list(workspaceId, all);
      const next = new Map<string, Process>();
      for (const p of arr) {
        const prev = this.map.get(p.id);
        const sparse = p.args.length === 0 && p.startedAt === null;
        next.set(p.id, prev && sparse ? { ...prev, status: p.status, pid: p.pid || prev.pid, exitCode: p.exitCode ?? prev.exitCode, workspaceId: p.workspaceId || prev.workspaceId, projectId: p.projectId || prev.projectId } : p);
      }
      this.map = next;
    } catch {
    }
  }

  setOptimistic(id: string, label: string) {
    const next = new Map(this.optimistic);
    next.set(id, label);
    this.optimistic = next;
  }

  clearOptimistic(id: string) {
    const next = new Map(this.optimistic);
    next.delete(id);
    this.optimistic = next;
  }

  disconnect() {
    this.closer?.();
    this.closer = null;
  }
}

export const processes = new Processes();
