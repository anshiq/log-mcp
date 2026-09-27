import { EventService } from '../api';
import { toEvent } from '../api/normalize';
import type { DaemonEvent } from '../api/types';

type Cb = (e: DaemonEvent) => void;

class Events {
  items = $state<DaemonEvent[]>([]);
  private subs = new Map<string, Set<Cb>>();
  private closer: (() => void) | null = null;

  onEvent(type: string, cb: Cb): () => void {
    if (!this.subs.has(type)) this.subs.set(type, new Set());
    this.subs.get(type)?.add(cb);
    return () => this.subs.get(type)?.delete(cb);
  }

  emit(e: DaemonEvent) {
    this.subs.get(e.type)?.forEach((cb) => cb(e));
    this.subs.get('*')?.forEach((cb) => cb(e));
  }

  connect(workspaceId: string) {
    this.disconnect();
    void EventService.list(workspaceId, 200).then((r) => {
      this.items = r.events.slice(-2000);
    }).catch(() => {});
    const h = EventService.watch(workspaceId, (msg) => {
      const e = toEvent(msg as Record<string, unknown>);
      this.items = [...this.items.slice(-1999), e];
      this.emit(e);
    });
    this.closer = () => h.close();
  }

  disconnect() {
    this.closer?.();
    this.closer = null;
  }
}

export const events = new Events();
