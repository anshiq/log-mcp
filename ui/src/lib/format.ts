export function bytes(n: number): string {
  if (!Number.isFinite(n)) return '—';
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let v = n / 1024;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u++;
  }
  return `${v.toFixed(1)} ${units[u]}`;
}

export function duration(ms: number): string {
  if (!Number.isFinite(ms)) return '—';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

export function relativeTime(ts: number): string {
  const d = Date.now() - ts;
  if (d < 0) return 'now';
  if (d < 5000) return 'just now';
  if (d < 60000) return `${Math.floor(d / 1000)}s ago`;
  if (d < 3600000) return `${Math.floor(d / 60000)}m ago`;
  if (d < 86400000) return `${Math.floor(d / 3600000)}h ago`;
  return new Date(ts).toLocaleDateString();
}

export function statusColor(status: string): string {
  switch (status) {
    case 'running':
    case 'ready':
      return 'var(--ok)';
    case 'starting':
      return 'var(--info)';
    case 'failed':
    case 'crashed':
      return 'var(--err)';
    case 'exited':
    case 'stopped':
      return 'var(--neutral)';
    default:
      return 'var(--warn)';
  }
}

export function processDisplayName(p: { app?: string; command?: string }): string {
  if (p.app) return p.app;
  const cmd = p.command ?? '';
  const base = cmd.split(' ').filter(Boolean)[0] ?? '';
  return base.split('/').pop() ?? cmd;
}
