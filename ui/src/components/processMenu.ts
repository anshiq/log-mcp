import RotateCw from '@lucide/svelte/icons/rotate-cw';
import Square from '@lucide/svelte/icons/square';
import Zap from '@lucide/svelte/icons/zap';
import Skull from '@lucide/svelte/icons/skull';
import Trash from '@lucide/svelte/icons/trash';
import ScrollText from '@lucide/svelte/icons/scroll-text';
import Copy from '@lucide/svelte/icons/copy';
import type { Process } from '../lib/api/types';
import type { MenuEntry } from './ActionMenu.svelte';
import { SIGNALS, isLive } from './processView';
import { procActions } from './processActions.svelte';

const SIGNAL_HINTS: Record<string, string> = {
  SIGINT: 'interrupt',
  SIGTERM: 'terminate',
  SIGHUP: 'reload',
  SIGQUIT: 'quit',
  SIGUSR1: 'user 1',
  SIGUSR2: 'user 2',
  SIGKILL: 'force kill'
};

export interface MenuHandlers {
  onLogs?: (id: string) => void;
  onCopyId?: (id: string) => void;
  afterRemove?: (ids: string[]) => void;
  includeLifecycle?: boolean;
}

export function processMenu(p: Process, h: MenuHandlers = {}): MenuEntry[] {
  const live = isLive(p);
  const entries: MenuEntry[] = [];
  if (h.includeLifecycle) {
    entries.push(
      { id: 'restart', label: 'Restart', icon: RotateCw, onselect: () => void procActions.restart([p.id]) },
      { id: 'stop', label: 'Stop', icon: Square, disabled: !live, onselect: () => void procActions.stop([p.id]) }
    );
  }
  if (h.onLogs) {
    entries.push({ id: 'logs', label: 'View logs', icon: ScrollText, onselect: () => h.onLogs?.(p.id) });
  }
  if (h.onCopyId) {
    entries.push({ id: 'copy', label: 'Copy process id', icon: Copy, onselect: () => h.onCopyId?.(p.id) });
  }
  if (entries.length > 0) entries.push({ type: 'separator' });
  entries.push({ type: 'heading', label: 'Send signal' });
  for (const sig of SIGNALS) {
    entries.push({
      id: sig,
      label: sig,
      icon: sig === 'SIGKILL' ? Skull : Zap,
      hint: SIGNAL_HINTS[sig],
      danger: sig === 'SIGKILL',
      disabled: !live,
      onselect: () => void procActions.signal(sig, [p.id])
    });
  }
  entries.push(
    { type: 'separator' },
    {
      id: 'remove',
      label: 'Remove',
      icon: Trash,
      danger: true,
      onselect: () => void procActions.remove([p.id]).then((done) => h.afterRemove?.(done))
    }
  );
  return entries;
}

export function bulkSignalMenu(ids: () => string[]): MenuEntry[] {
  return SIGNALS.map((sig) => ({
    id: sig,
    label: sig,
    icon: sig === 'SIGKILL' ? Skull : Zap,
    hint: SIGNAL_HINTS[sig],
    danger: sig === 'SIGKILL',
    onselect: () => void procActions.signal(sig, ids())
  }));
}
