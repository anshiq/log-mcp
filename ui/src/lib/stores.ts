// Stream-fed normalized process store, keyed by id. Fed by
// WatchProcesses (snapshot then upserts) with auto-resume.
import { writable, type Writable } from 'svelte/store';
import { stream } from './api';

export interface Process {
  process_id?: string;
  id?: string;
  status?: string;
  command?: string;
  workdir?: string;
  profile?: string;
  pid?: number;
  health?: string;
  workspaceId?: string;
  projectId?: string;
  [key: string]: unknown;
}

export interface ProcessStore {
  processes: Writable<Map<string, Process>>;
  connect(workspaceId?: string, all?: boolean): () => void;
}

function pidOf(p: Process): string {
  return String(p.process_id ?? p.id ?? '');
}

export function createProcessStore(): ProcessStore {
  const processes = writable<Map<string, Process>>(new Map());
  return {
    processes,
    connect(workspaceId = '', all = true) {
      return stream(
        'ProcessService',
        'WatchProcesses',
        { workspaceId, allWorkspaces: all },
        (msg) => {
          processes.update((map) => {
            const next = new Map(map);
            if (msg.kind === 'snapshot' && Array.isArray(msg.snapshot)) {
              next.clear();
              for (const p of msg.snapshot as Process[]) next.set(pidOf(p), p);
            } else if (msg.kind === 'upsert' && msg.process) {
              const p = msg.process as Process;
              next.set(pidOf(p), p);
            } else if (msg.kind === 'removed' && msg.removed_id) {
              next.delete(String(msg.removed_id));
            }
            return next;
          });
        }
      );
    }
  };
}
