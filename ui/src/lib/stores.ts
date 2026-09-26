import { writable, type Writable } from 'svelte/store';
import { ProcessService, type StreamHandle, type StreamStateHandler } from './api';

export interface Process {
  id?: string;
  process_id?: string;
  instanceId?: string;
  app?: string;
  status?: string;
  command?: string;
  args?: string[];
  workdir?: string;
  profile?: string;
  pid?: number;
  restarts?: number;
  health?: string;
  restartPolicy?: string;
  startedAt?: string;
  exitedAt?: string | null;
  exitCode?: number | null;
  stdoutLines?: number;
  stderrLines?: number;
  ports?: number[];
  stale?: boolean;
  workspaceId?: string;
  projectId?: string;
  [key: string]: unknown;
}

export interface ProcessStore {
  processes: Writable<Map<string, Process>>;
  streamState: Writable<'connecting' | 'open' | 'reconnecting' | 'closed'>;
  connect(workspaceId?: string, all?: boolean): () => void;
}

function pidOf(p: Process): string {
  return String(p.id ?? p.process_id ?? '');
}

export function createProcessStore(): ProcessStore {
  const processes = writable<Map<string, Process>>(new Map());
  const streamState = writable<'connecting' | 'open' | 'reconnecting' | 'closed'>('connecting');

  return {
    processes,
    streamState,
    connect(workspaceId = '', all = true) {
      const onState: StreamStateHandler = (s) => streamState.set(s);
      const handle: StreamHandle = ProcessService.watch(
        workspaceId,
        all,
        (msg) => {
          processes.update((map) => {
            const next = new Map(map);
            if (msg.kind === 'snapshot' && Array.isArray(msg.snapshot)) {
              next.clear();
              for (const p of msg.snapshot as Process[]) next.set(pidOf(p), p);
            } else if (msg.kind === 'upsert' && msg.process) {
              const p = msg.process as Process;
              next.set(pidOf(p), p);
            } else if (msg.kind === 'removed' && msg.processId) {
              next.delete(String(msg.processId));
            } else if (msg.kind === 'gap') {
              void ProcessService.list(workspaceId, all).then((res) => {
                processes.update((m) => {
                  const fresh = new Map(m);
                  fresh.clear();
                  for (const p of res.processes as Process[]) fresh.set(pidOf(p), p);
                  return fresh;
                });
              });
            }
            return next;
          });
        },
        onState
      );
      return () => handle.close();
    }
  };
}
