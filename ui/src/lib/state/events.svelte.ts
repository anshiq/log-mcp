import { EventService } from '../api';
import { toEvent } from '../api/normalize';
import type { DaemonEvent } from '../api/types';

type Cb = (e: DaemonEvent) => void;

const CAP = 5000;
const PAGE = 400;

function merge(a: DaemonEvent[], b: DaemonEvent[]): DaemonEvent[] {
  const byId = new Map<number, DaemonEvent>();
  for (const e of a) byId.set(e.id, e);
  for (const e of b) byId.set(e.id, e);
  return [...byId.values()].sort((x, y) => x.id - y.id);
}

class Events {
  items = $state<DaemonEvent[]>([]);
  hasMore = $state(false);
  loadingOlder = $state(false);
  loaded = $state(false);
  private subs = new Map<string, Set<Cb>>();
  private closer: (() => void) | null = null;
  private floor = 0;
  private generation = 0;

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
    const gen = ++this.generation;
    this.loaded = false;
    void this.loadTail(workspaceId, gen);
    const h = EventService.watch(workspaceId, (msg) => {
      const e = toEvent(msg as Record<string, unknown>);
      this.items = merge(this.items, [e]).slice(-CAP);
      this.emit(e);
    });
    this.closer = () => h.close();
  }

  private async loadTail(workspaceId: string, gen: number) {
    try {
      const latest = await EventService.latestId();
      const w = await EventService.window(workspaceId, latest, PAGE);
      if (gen !== this.generation) return;
      this.items = merge(this.items, w.events).slice(-CAP);
      this.floor = w.floor;
      this.hasMore = w.floor > 0;
    } catch {
      this.hasMore = false;
    } finally {
      if (gen === this.generation) this.loaded = true;
    }
  }

  async loadOlder(workspaceId = ''): Promise<number> {
    if (this.loadingOlder || !this.hasMore) return 0;
    this.loadingOlder = true;
    try {
      const w = await EventService.window(workspaceId, this.floor, PAGE);
      this.items = merge(w.events, this.items);
      this.floor = w.floor;
      this.hasMore = w.floor > 0;
      return w.events.length;
    } catch {
      return 0;
    } finally {
      this.loadingOlder = false;
    }
  }

  disconnect() {
    this.closer?.();
    this.closer = null;
  }
}

export const events = new Events();
