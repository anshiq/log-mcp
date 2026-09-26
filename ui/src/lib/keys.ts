import { router } from './router.svelte';
import { palette } from './palette.svelte';

const GOTO: Record<string, string> = {
  p: '/processes',
  a: '/apps',
  c: '/config',
  e: '/events',
  j: '/projects',
  s: '/sessions',
  i: '/integrations',
  u: '/audit',
  t: '/settings'
};

function isTyping(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (!el) return false;
  const tag = el.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable || !!el.closest('.monaco-editor');
}

export function installGlobalKeys(): () => void {
  let pendingG = false;
  let pendingTimer: ReturnType<typeof setTimeout> | null = null;

  function handler(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
      e.preventDefault();
      if (palette.open) palette.hide();
      else void palette.show();
      return;
    }
    if (isTyping(e.target)) return;
    if (e.metaKey || e.ctrlKey || e.altKey) return;

    if (pendingG) {
      pendingG = false;
      if (pendingTimer) clearTimeout(pendingTimer);
      const path = GOTO[e.key];
      if (path) {
        e.preventDefault();
        router.navigate(path);
      }
      return;
    }

    if (e.key === 'g') {
      pendingG = true;
      pendingTimer = setTimeout(() => (pendingG = false), 1000);
      return;
    }

    if (e.key === '/') {
      const search = document.querySelector<HTMLInputElement>('.search, [aria-label="Filter processes"]');
      if (search) {
        e.preventDefault();
        search.focus();
      }
    }
  }

  window.addEventListener('keydown', handler);
  return () => window.removeEventListener('keydown', handler);
}
