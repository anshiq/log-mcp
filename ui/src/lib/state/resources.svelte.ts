import { ProcessService } from '../api';
import { toResource } from '../api/normalize';
import type { ResourceSample } from '../api/types';

class Resources {
  samples = $state(new Map<string, ResourceSample[]>());
  private timers = new Map<string, ReturnType<typeof setInterval>>();

  watch(ids: string[]) {
    const wanted = new Set(ids.slice(0, 20));
    for (const [id, t] of this.timers) {
      if (!wanted.has(id)) {
        clearInterval(t);
        this.timers.delete(id);
      }
    }
    for (const id of wanted) {
      if (this.timers.has(id)) continue;
      const poll = async () => {
        try {
          const s = await ProcessService.getResourceUsage(id);
          const arr = this.samples.get(id) ?? [];
          const next = [...arr.slice(-150), s];
          const m = new Map(this.samples);
          m.set(id, next);
          this.samples = m;
        } catch {
        }
      };
      void poll();
      this.timers.set(id, setInterval(poll, 5000));
    }
  }

  live(id: string): ResourceSample | null {
    const arr = this.samples.get(id);
    return arr && arr.length > 0 ? (arr[arr.length - 1] as ResourceSample) : null;
  }

  history(id: string): ResourceSample[] {
    return this.samples.get(id) ?? [];
  }
}

export const resources = new Resources();

export async function fetchResource(id: string): Promise<ResourceSample | null> {
  try {
    const r = await ProcessService.getResourceUsage(id);
    return toResource(r as unknown as Record<string, unknown>);
  } catch {
    return null;
  }
}
