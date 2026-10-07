import { toasts } from '../toasts.svelte';
import { getPlatform } from '../platform';
import { prefs } from './prefs.svelte';
import type { DaemonEvent } from '../api/types';

const lastFire = new Map<string, number>();

export function handleAlertEvent(e: DaemonEvent) {
  const t = e.type;
  const interesting = t === 'logs.alert' || t === 'process.crashed' || t === 'process.failed' || t === 'process.unhealthy';
  if (!interesting) return;
  if (prefs.data.dnd) return;
  const key = t === 'logs.alert' ? 'alert' : t.includes('crash') ? 'crash' : 'fail';
  if (!prefs.data.notifyOn[key]) return;
  if (prefs.data.mutedProcesses.includes(e.processId)) return;
  const now = Date.now();
  const last = lastFire.get(e.processId) ?? 0;
  if (now - last < 30000) return;
  lastFire.set(e.processId, now);
  const msg = typeof e.payload['message'] === 'string' ? (e.payload['message'] as string) : t;
  toasts.push(`${t}: ${msg}`, 'err', {
    label: 'View logs',
    onClick: () => {
      window.location.hash = `#/processes/${e.processId}?tab=logs`;
    }
  });
  try {
    getPlatform().notify(t, msg);
  } catch {
  }
}
