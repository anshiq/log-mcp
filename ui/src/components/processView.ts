import type { Process } from '../lib/api/types';

export type Tone = 'ok' | 'err' | 'warn' | 'busy' | 'off';
export type StatusFilter = 'all' | 'running' | 'starting' | 'failed' | 'exited';
export type SortKey = 'status' | 'name' | 'pid' | 'uptime' | 'restarts' | 'health';
export type SortDir = 'asc' | 'desc';

export const SIGNALS = ['SIGINT', 'SIGTERM', 'SIGHUP', 'SIGQUIT', 'SIGUSR1', 'SIGUSR2', 'SIGKILL'] as const;

const STATUS_RANK: Record<string, number> = {
  failed: 0,
  crashed: 0,
  starting: 1,
  running: 2,
  ready: 2,
  exited: 3,
  stopped: 3
};

export function isRunning(p: Pick<Process, 'status'>): boolean {
  return p.status === 'running' || p.status === 'ready';
}

export function isStarting(p: Pick<Process, 'status'>): boolean {
  return p.status === 'starting';
}

export function isLive(p: Pick<Process, 'status'>): boolean {
  return isRunning(p) || isStarting(p);
}

export function isFailed(p: Pick<Process, 'status'>): boolean {
  return p.status === 'failed' || p.status === 'crashed';
}

export function isExited(p: Pick<Process, 'status'>): boolean {
  return p.status === 'exited' || p.status === 'stopped';
}

export function matchesStatus(p: Pick<Process, 'status'>, f: StatusFilter): boolean {
  if (f === 'all') return true;
  if (f === 'running') return isRunning(p);
  if (f === 'starting') return isStarting(p);
  if (f === 'failed') return isFailed(p);
  return isExited(p);
}

export function statusTone(status: string): Tone {
  if (status === 'running' || status === 'ready') return 'ok';
  if (status === 'starting') return 'busy';
  if (status === 'failed' || status === 'crashed') return 'err';
  if (status === 'exited' || status === 'stopped') return 'off';
  return 'warn';
}

export function healthTone(health: string): Tone | null {
  if (health === 'healthy') return 'ok';
  if (health === 'unhealthy') return 'err';
  if (!health || health === 'unknown' || health === 'none') return null;
  return 'warn';
}

export function commandLine(p: Pick<Process, 'command' | 'args'>): string {
  return [p.command, ...(p.args ?? [])].filter(Boolean).join(' ');
}

export function baseName(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

const SHELLS = new Set(['sh', 'bash', 'zsh', 'dash', 'fish']);
const NOISE = new Set(['while', 'until', 'for', 'if', 'then', 'do', 'done', 'true', 'false', 'exec', 'cd', 'export', 'set', 'eval', 'time', 'sleep']);

function scriptProgram(script: string): string {
  for (const raw of script.split(/\s+/)) {
    const tok = raw.replace(/^[({]+|[;&|)}]+$/g, '');
    if (!tok || tok.includes('=') || NOISE.has(tok)) continue;
    if (/^[A-Za-z][\w.-]*$/.test(tok)) return tok;
  }
  return '';
}

export function displayName(p: Pick<Process, 'app' | 'command' | 'args'>): string {
  if (p.app) return p.app;
  const first = (p.command ?? '').trim().split(/\s+/)[0] ?? '';
  const prog = baseName(first);
  if (!prog) return 'process';
  if (SHELLS.has(prog) && p.args?.[0] === '-c' && p.args[1]) {
    const inner = scriptProgram(p.args[1]);
    if (inner) return `${prog} · ${inner.slice(0, 24)}`;
  }
  return prog;
}

export function compactDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '0s';
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, '0')}s`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${String(m % 60).padStart(2, '0')}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

export function agoShort(ts: number, now: number): string {
  const d = Math.max(0, now - ts);
  if (d < 5000) return 'just now';
  const s = Math.floor(d / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

export function uptimeMs(p: Pick<Process, 'status' | 'startedAt'>, now: number): number | null {
  if (!isLive(p) || !p.startedAt) return null;
  return Math.max(0, now - p.startedAt);
}

export function uptimeText(p: Pick<Process, 'status' | 'startedAt' | 'exitedAt'>, now: number): string {
  const up = uptimeMs(p, now);
  if (up !== null) return compactDuration(up);
  if (p.exitedAt) return `exited ${agoShort(p.exitedAt, now)}`;
  return '';
}

export function exitInfo(p: Pick<Process, 'status' | 'exitCode' | 'exitSignal'>): string {
  if (p.exitSignal) return p.exitSignal;
  if (p.exitCode !== null && (isExited(p) || isFailed(p))) return `code ${p.exitCode}`;
  return '';
}

export function matchesQuery(p: Process, query: string, workspaceLabel: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const hay = [
    displayName(p),
    p.app,
    commandLine(p),
    p.id,
    String(p.pid || ''),
    p.profile,
    p.health,
    p.status,
    workspaceLabel,
    ...p.ports.map((x) => `:${x}`)
  ]
    .join('\n')
    .toLowerCase();
  return q.split(/\s+/).every((t) => hay.includes(t));
}

export function sortRows(rows: Process[], key: SortKey | null, dir: SortDir): Process[] {
  if (!key) return rows;
  const sign = dir === 'asc' ? 1 : -1;
  const value = (p: Process): number | string => {
    switch (key) {
      case 'status':
        return STATUS_RANK[p.status] ?? 4;
      case 'name':
        return displayName(p).toLowerCase();
      case 'pid':
        return p.pid || 0;
      case 'uptime':
        return isLive(p) && p.startedAt ? p.startedAt : Number.POSITIVE_INFINITY;
      case 'restarts':
        return p.restarts;
      default:
        return p.health;
    }
  };
  return [...rows].sort((a, b) => {
    const av = value(a);
    const bv = value(b);
    if (av === bv) return 0;
    if (key === 'uptime') {
      const ai = av === Number.POSITIVE_INFINITY;
      const bi = bv === Number.POSITIVE_INFINITY;
      if (ai !== bi) return ai ? 1 : -1;
      return av < bv ? sign : -sign;
    }
    return av < bv ? -sign : sign;
  });
}

export function splitEnvLine(line: string): { key: string; value: string } {
  const i = line.indexOf('=');
  if (i < 0) return { key: line, value: '' };
  return { key: line.slice(0, i), value: line.slice(i + 1) };
}
