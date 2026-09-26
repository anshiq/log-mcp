export interface Platform {
  readonly name: 'wails' | 'browser';
  notify(title: string, body: string): void;
  openInEditor(path: string): void;
  saveFile(suggested: string, content: string): Promise<void>;
  copyText(text: string): Promise<void>;
}

declare global {
  interface Window {
    __wailsBinding?: {
      notify(title: string, body: string): void;
      openInEditor(path: string): void;
      saveDialog(suggested: string): Promise<string | null>;
      writeFile(path: string, content: string): Promise<void>;
    };
  }
}

class WailsPlatform implements Platform {
  readonly name = 'wails' as const;
  notify(title: string, body: string) {
    window.__wailsBinding?.notify(title, body);
  }
  openInEditor(path: string) {
    window.__wailsBinding?.openInEditor(path);
  }
  async saveFile(suggested: string, content: string) {
    const b = window.__wailsBinding;
    if (!b) return;
    const path = await b.saveDialog(suggested);
    if (path) await b.writeFile(path, content);
  }
  async copyText(text: string) {
    await navigator.clipboard.writeText(text);
  }
}

class BrowserPlatform implements Platform {
  readonly name = 'browser' as const;
  notify(title: string, body: string) {
    if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
      new Notification(title, { body });
    }
  }
  openInEditor(_path: string) {
    alert('Open-in-editor is a desktop feature.');
  }
  async saveFile(suggested: string, content: string) {
    const blob = new Blob([content], { type: 'text/plain' });
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
}

const wails = new WailsPlatform();
const browser = new BrowserPlatform();

export function getPlatform(): Platform {
  return typeof window !== 'undefined' && window.__wailsBinding ? wails : browser;
}

export function whenPlatformReady(): Promise<Platform> {
  if (__APP_TARGET__ !== 'wails') return Promise.resolve(browser);
  if (typeof window !== 'undefined' && window.__wailsBinding) return Promise.resolve(wails);
  return new Promise((resolve) => {
    const start = Date.now();
    const poll = () => {
      if (typeof window !== 'undefined' && window.__wailsBinding) {
        resolve(wails);
        return;
      }
      if (Date.now() - start > 1500) {
        resolve(getPlatform());
        return;
      }
      setTimeout(poll, 25);
    };
    poll();
  });
}
