export interface Platform {
  readonly name: 'wails' | 'browser';
  notify(title: string, body: string, opts?: { tag?: string; onClick?: () => void }): void;
  openInEditor(path: string, line?: number): void;
  saveFile(suggested: string, content: string, mime?: string): Promise<void>;
  copyText(text: string): Promise<void>;
  openExternal(url: string): void;
  pickDirectory(): Promise<string | null>;
  revealInFileManager(path: string): void;
  setBadge(count: number): void;
  requestNotificationPermission(): Promise<boolean>;
}

declare global {
  interface Window {
    __wailsBinding?: {
      notify(title: string, body: string, tag?: string): void;
      openInEditor(path: string, line?: number): void;
      saveDialog(suggested: string): Promise<string | null>;
      writeFile(path: string, content: string): Promise<void>;
      pickDirectory(): Promise<string | null>;
      revealInFileManager(path: string): void;
      openExternal(url: string): void;
      setBadge(count: number): void;
    };
    go?: {
      main?: {
        App?: Record<string, (...args: never[]) => unknown>;
      };
      gui?: {
        App?: Record<string, (...args: never[]) => unknown>;
      };
    };
  }
}

class WailsPlatform implements Platform {
  readonly name = 'wails' as const;
  notify(title: string, body: string, opts?: { tag?: string }) {
    window.__wailsBinding?.notify(title, body, opts?.tag);
  }
  openInEditor(path: string, line?: number) {
    window.__wailsBinding?.openInEditor(path, line);
  }
  async saveFile(suggested: string, content: string) {
    const b = window.__wailsBinding;
    if (!b) return;
    const path = await b.saveDialog(suggested);
    if (path) await b.writeFile(path, content);
  }
  async copyText(text: string) {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      const ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    }
  }
  openExternal(url: string) {
    window.__wailsBinding?.openExternal(url);
  }
  async pickDirectory(): Promise<string | null> {
    return (await window.__wailsBinding?.pickDirectory()) ?? null;
  }
  revealInFileManager(path: string) {
    window.__wailsBinding?.revealInFileManager(path);
  }
  setBadge(count: number) {
    window.__wailsBinding?.setBadge(count);
    document.title = count > 0 ? `(${count}) agent-runtime` : document.title.replace(/^\(\d+\) /, '');
  }
  async requestNotificationPermission(): Promise<boolean> {
    return true;
  }
}

class BrowserPlatform implements Platform {
  readonly name = 'browser' as const;
  notify(title: string, body: string, opts?: { onClick?: () => void }) {
    if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
      const n = new Notification(title, { body, tag: 'ar' });
      if (opts?.onClick) n.onclick = opts.onClick;
    }
  }
  openInEditor(_path: string) {
    alert('Open-in-editor is a desktop feature.');
  }
  async saveFile(suggested: string, content: string, mime = 'text/plain') {
    const blob = new Blob([content], { type: mime });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = suggested;
    a.click();
    URL.revokeObjectURL(a.href);
  }
  async copyText(text: string) {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    }
  }
  openExternal(url: string) {
    window.open(url, '_blank', 'noopener');
  }
  async pickDirectory(): Promise<string | null> {
    const v = prompt('Directory path:');
    return v && v.trim() ? v.trim() : null;
  }
  revealInFileManager(_path: string) {
    alert('Reveal in file manager is a desktop feature.');
  }
  setBadge(count: number) {
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (count > 0) {
      document.title = `(${count}) agent-runtime`;
      if (link) link.dataset['badge'] = String(count);
    } else {
      document.title = document.title.replace(/^\(\d+\) /, '');
    }
  }
  async requestNotificationPermission(): Promise<boolean> {
    if (typeof Notification === 'undefined') return false;
    if (Notification.permission === 'granted') return true;
    return (await Notification.requestPermission()) === 'granted';
  }
}

const wails = new WailsPlatform();
const browser = new BrowserPlatform();

export function getPlatform(): Platform {
  if (typeof window !== 'undefined' && (window.__wailsBinding || window.go?.main?.App || window.go?.gui?.App)) return wails;
  return browser;
}

export function whenPlatformReady(): Promise<Platform> {
  if (__APP_TARGET__ !== 'wails') return Promise.resolve(browser);
  if (typeof window !== 'undefined' && (window.__wailsBinding || window.go?.main?.App || window.go?.gui?.App)) return Promise.resolve(wails);
  return new Promise((resolve) => {
    const start = Date.now();
    const onReady = () => {
      window.removeEventListener('wails:ready', onReady);
      resolve(getPlatform());
    };
    window.addEventListener('wails:ready', onReady);
    const poll = () => {
      if (typeof window !== 'undefined' && (window.__wailsBinding || window.go?.main?.App || window.go?.gui?.App)) {
        window.removeEventListener('wails:ready', onReady);
        resolve(wails);
        return;
      }
      if (Date.now() - start > 1500) {
        window.removeEventListener('wails:ready', onReady);
        resolve(getPlatform());
        return;
      }
      setTimeout(poll, 25);
    };
    poll();
  });
}
