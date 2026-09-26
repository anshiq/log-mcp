// Platform abstraction: the ONLY split between the Wails desktop build
// and the web build. Native features (dialogs, notifications, tray,
// editor) go through Wails bindings in WailsPlatform; BrowserPlatform
// uses downloads, the Web Notification API, and no tray.
export interface Platform {
  readonly name: 'wails' | 'browser';
  notify(title: string, body: string): void;
  openInEditor(path: string): void;
  saveFile(suggested: string, content: string): Promise<void>;
  copyText(text: string): Promise<void>;
}

declare global {
  interface Window {
    // Injected by the Wails Go shell (cmd/agent-runtime-gui native bridge).
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
    if ('Notification' in window && Notification.permission === 'granted') {
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
    await navigator.clipboard.writeText(text);
  }
}

export const platform: Platform =
  typeof window !== 'undefined' && window.__wailsBinding ? new WailsPlatform() : new BrowserPlatform();
