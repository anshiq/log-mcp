import type { Component } from 'svelte';
import Play from '@lucide/svelte/icons/play';
import Square from '@lucide/svelte/icons/square';
import CircleX from '@lucide/svelte/icons/circle-x';
import OctagonX from '@lucide/svelte/icons/octagon-x';
import RotateCw from '@lucide/svelte/icons/rotate-cw';
import HeartPulse from '@lucide/svelte/icons/heart-pulse';
import HeartCrack from '@lucide/svelte/icons/heart-crack';
import Bell from '@lucide/svelte/icons/bell';
import FileCog from '@lucide/svelte/icons/file-cog';
import Ban from '@lucide/svelte/icons/ban';
import Activity from '@lucide/svelte/icons/activity';
import type { DaemonEvent, Process } from './api/types';
import { processes } from './state/processes.svelte';
import { scopeState } from './state/scope.svelte';

export type Tone = 'ok' | 'err' | 'warn' | 'info' | 'neutral';
export type Family = 'process' | 'logs' | 'config' | 'other';

export interface EventKind {
  family: Family;
  tone: Tone;
  icon: Component<{ size?: number }>;
  label: string;
}

const KINDS: Record<string, EventKind> = {
  'process.started': { family: 'process', tone: 'ok', icon: Play, label: 'Started' },
  'process.restarted': { family: 'process', tone: 'info', icon: RotateCw, label: 'Restarted' },
  'process.stopped': { family: 'process', tone: 'neutral', icon: Square, label: 'Stopped' },
  'process.exited': { family: 'process', tone: 'neutral', icon: Square, label: 'Exited' },
  'process.failed': { family: 'process', tone: 'err', icon: CircleX, label: 'Failed' },
  'process.crashed': { family: 'process', tone: 'err', icon: OctagonX, label: 'Crashed' },
  'process.healthy': { family: 'process', tone: 'ok', icon: HeartPulse, label: 'Healthy' },
  'process.unhealthy': { family: 'process', tone: 'warn', icon: HeartCrack, label: 'Unhealthy' },
  'process.removed': { family: 'process', tone: 'neutral', icon: Ban, label: 'Removed' },
  'logs.alert': { family: 'logs', tone: 'warn', icon: Bell, label: 'Alert' }
};

export const FAMILY_LABEL: Record<Family, string> = {
  process: 'Process',
  logs: 'Logs',
  config: 'Config',
  other: 'Other'
};

export const KNOWN_TYPES = Object.keys(KINDS);

export function eventKind(type: string): EventKind {
  const known = KINDS[type];
  if (known) return known;
  if (type.startsWith('config.')) return { family: 'config', tone: 'info', icon: FileCog, label: humanizeType(type) };
  if (type.startsWith('logs.')) return { family: 'logs', tone: 'info', icon: Bell, label: humanizeType(type) };
  if (type.startsWith('process.')) return { family: 'process', tone: 'neutral', icon: Activity, label: humanizeType(type) };
  return { family: 'other', tone: 'neutral', icon: Activity, label: humanizeType(type) };
}

export function humanizeType(type: string): string {
  const tail = type.includes('.') ? type.slice(type.indexOf('.') + 1) : type;
  const words = tail.replace(/[._]+/g, ' ').trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : type;
}

export function exitCodeOf(e: DaemonEvent): number | null {
  const v = e.payload['exit_code'] ?? e.payload['exitCode'];
  return typeof v === 'number' ? v : null;
}

export function eventTone(e: DaemonEvent): Tone {
  const k = eventKind(e.type);
  if (e.type === 'process.exited') {
    const code = exitCodeOf(e);
    if (code !== null && code !== 0) return 'warn';
  }
  return k.tone;
}

export function commandLine(p: Pick<Process, 'command' | 'args'>): string {
  return [p.command, ...p.args].filter(Boolean).join(' ');
}

export function baseName(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

const SHELLS = new Set(['sh', 'bash', 'zsh', 'dash', 'fish']);

export function procTitle(p: Pick<Process, 'app' | 'command'> & { args?: string[] }): string {
  if (p.app) return p.app;
  const bin = baseName(p.command.split(' ')[0] ?? p.command) || p.command || 'process';
  const args = p.args ?? [];
  if (SHELLS.has(bin) && args[0] === '-c' && args[1]) {
    const script = (args[1].split(/;|&&|\|\|?/)[0] ?? args[1]).replace(/\s+/g, ' ').trim() || args[1].trim();
    return script.length > 30 ? `${script.slice(0, 29)}…` : script;
  }
  if (args.length > 0 && !SHELLS.has(bin)) {
    const first = args.find((a) => !a.startsWith('-'));
    if (first && ['node', 'python', 'python3', 'deno', 'bun', 'ruby', 'go', 'npm', 'pnpm', 'yarn', 'npx', 'uv', 'cargo'].includes(bin)) return `${bin} ${baseName(first)}`;
  }
  return bin;
}

export function procLabel(id: string): string {
  const p = processes.map.get(id);
  return p ? procTitle(p) : id;
}

export function eventWorkspace(e: DaemonEvent): string {
  return e.workspaceId || processes.map.get(e.processId)?.workspaceId || '';
}

export function eventSentence(e: DaemonEvent): string {
  const who = e.processId ? procLabel(e.processId) : '';
  const code = exitCodeOf(e);
  switch (e.type) {
    case 'process.started':
      return `${who} started`;
    case 'process.restarted':
      return `${who} restarted`;
    case 'process.stopped':
      return `${who} stopped`;
    case 'process.exited':
      return code === null ? `${who} exited` : `${who} exited with code ${code}`;
    case 'process.failed':
      return `${who} failed${code === null ? '' : ` with code ${code}`}`;
    case 'process.crashed':
      return `${who} crashed${code === null ? '' : ` with code ${code}`}`;
    case 'process.healthy':
      return `${who} is healthy`;
    case 'process.unhealthy':
      return `${who} became unhealthy`;
    case 'process.removed':
      return `${who} was removed`;
    case 'logs.alert': {
      const m = e.payload['message'];
      return typeof m === 'string' && m ? `${who ? `${who}: ` : ''}${m}` : `Log alert${who ? ` on ${who}` : ''}`;
    }
    default:
      return who ? `${who} · ${humanizeType(e.type)}` : humanizeType(e.type);
  }
}

export function dedupeEvents(list: DaemonEvent[]): DaemonEvent[] {
  const seen = new Map<string, DaemonEvent>();
  for (const e of list) {
    const key = `${e.type}|${e.processId}|${e.instanceId}|${e.ts}`;
    const prev = seen.get(key);
    if (!prev || (!prev.workspaceId && e.workspaceId)) seen.set(key, e);
  }
  return [...seen.values()];
}

export function scopedEvents(list: DaemonEvent[]): DaemonEvent[] {
  const uniq = dedupeEvents(list);
  if (scopeState.all) return uniq;
  const ws = scopeState.workspaceId;
  return uniq.filter((e) => eventWorkspace(e) === ws);
}

export function dayKey(ts: number): string {
  const d = new Date(ts);
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}

export function dayLabel(ts: number, now = Date.now()): string {
  const d = new Date(ts);
  const today = new Date(now);
  const yesterday = new Date(now - 86400000);
  if (dayKey(ts) === dayKey(today.getTime())) return 'Today';
  if (dayKey(ts) === dayKey(yesterday.getTime())) return 'Yesterday';
  const sameYear = d.getFullYear() === today.getFullYear();
  return d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric', ...(sameYear ? {} : { year: 'numeric' }) });
}

export function clock(ts: number): string {
  return new Date(ts).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function exactTime(ts: number): string {
  return new Date(ts).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'medium' });
}
