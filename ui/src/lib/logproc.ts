import type { Process } from './api/types';

const HUES = [212, 32, 152, 312, 178, 52, 252, 12, 122, 282, 92, 336];

const slots = new Map<string, number>();
const taken = new Set<number>();

function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

export function processHue(id: string): number {
  let slot = slots.get(id);
  if (slot === undefined) {
    let i = hash(id) % HUES.length;
    for (let n = 0; n < HUES.length && taken.has(i); n++) i = (i + 1) % HUES.length;
    slot = i;
    slots.set(id, slot);
    taken.add(slot);
  }
  return HUES[slot] as number;
}

export function resetProcessHues() {
  slots.clear();
  taken.clear();
}

function baseName(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

export function baseLabel(p: Pick<Process, 'app' | 'command'>): string {
  if (p.app) return p.app;
  const first = p.command.trim().split(/\s+/)[0] ?? '';
  return baseName(first) || p.command || 'process';
}

export function idSuffix(id: string): string {
  return id.replace(/^proc_/, '').slice(-4);
}

export function buildLabels(list: Pick<Process, 'id' | 'app' | 'command'>[]): Map<string, string> {
  const base = new Map<string, string>();
  const count = new Map<string, number>();
  for (const p of list) {
    const b = baseLabel(p);
    base.set(p.id, b);
    count.set(b, (count.get(b) ?? 0) + 1);
  }
  const out = new Map<string, string>();
  for (const p of list) {
    const b = base.get(p.id) as string;
    out.set(p.id, (count.get(b) ?? 0) > 1 ? `${b}·${idSuffix(p.id)}` : b);
  }
  return out;
}

export function commandPreview(p: Pick<Process, 'command' | 'args'>): string {
  const args = p.args.map((a) => (/\s/.test(a) ? JSON.stringify(a) : a));
  return [p.command, ...args].join(' ').trim();
}
