import { ProcessService } from '../lib/api';
import { toasts, toastError } from '../lib/toasts.svelte';
import { processes } from '../lib/state/processes.svelte';
import { displayName } from './processView';

export interface ConfirmRequest {
  title: string;
  message: string;
  confirmLabel: string;
  danger: boolean;
  resolve: (ok: boolean) => void;
}

function describe(ids: string[]): string {
  if (ids.length !== 1) return `${ids.length} processes`;
  const p = processes.map.get(ids[0] as string);
  return p ? `"${displayName(p)}"` : '1 process';
}

class ProcessActions {
  busy = $state(new Map<string, string>());
  request = $state<ConfirmRequest | null>(null);

  ask(opts: Omit<ConfirmRequest, 'resolve'>): Promise<boolean> {
    return new Promise((resolve) => {
      this.request?.resolve(false);
      this.request = { ...opts, resolve };
    });
  }

  answer(ok: boolean) {
    const r = this.request;
    this.request = null;
    r?.resolve(ok);
  }

  private mark(ids: string[], label: string) {
    const next = new Map(this.busy);
    for (const id of ids) next.set(id, label);
    this.busy = next;
  }

  private unmark(ids: string[]) {
    const next = new Map(this.busy);
    for (const id of ids) next.delete(id);
    this.busy = next;
  }

  private async run(ids: string[], label: string, fn: (id: string) => Promise<unknown>): Promise<string[]> {
    this.mark(ids, label);
    const done: string[] = [];
    const results = await Promise.allSettled(ids.map((id) => fn(id)));
    results.forEach((r, i) => {
      if (r.status === 'fulfilled') done.push(ids[i] as string);
      else toastError(r.reason);
    });
    this.unmark(ids);
    return done;
  }

  async restart(ids: string[]): Promise<string[]> {
    if (ids.length === 0) return [];
    const done = await this.run(ids, 'restarting', (id) => ProcessService.restart(id));
    if (done.length > 0) toasts.ok(`Restarted ${describe(done)}`);
    return done;
  }

  async stop(ids: string[]): Promise<string[]> {
    if (ids.length === 0) return [];
    const done = await this.run(ids, 'stopping', (id) => ProcessService.stop(id));
    if (done.length > 0) toasts.ok(`Stopped ${describe(done)}`);
    return done;
  }

  async signal(sig: string, ids: string[]): Promise<string[]> {
    if (ids.length === 0) return [];
    if (sig === 'SIGKILL') {
      const ok = await this.ask({
        title: 'Send SIGKILL',
        message: `SIGKILL immediately terminates ${describe(ids)} and does not allow a graceful shutdown. Unsaved state may be lost.`,
        confirmLabel: 'Send SIGKILL',
        danger: true
      });
      if (!ok) return [];
    }
    const done = await this.run(ids, 'signaling', (id) => ProcessService.signal(id, sig));
    if (done.length > 0) toasts.ok(`${sig} sent to ${describe(done)}`);
    return done;
  }

  async remove(ids: string[]): Promise<string[]> {
    if (ids.length === 0) return [];
    const name = describe(ids);
    const ok = await this.ask({
      title: ids.length === 1 ? 'Remove process' : `Remove ${ids.length} processes`,
      message: `Remove ${name} from the runtime? Running processes are force-stopped and their records are deleted.`,
      confirmLabel: 'Remove',
      danger: true
    });
    if (!ok) return [];
    const done = await this.run(ids, 'removing', (id) => ProcessService.remove(id, true));
    if (done.length > 0) toasts.ok(`Removed ${done.length === 1 ? 'process' : `${done.length} processes`}`);
    return done;
  }
}

export const procActions = new ProcessActions();
