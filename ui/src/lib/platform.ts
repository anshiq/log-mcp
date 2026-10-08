export interface Platform {
  readonly name: 'browser';
  notify(title: string, body: string, opts?: { tag?: string; onClick?: () => void }): void;
  saveFile(suggested: string, content: string, mime?: string): Promise<void>;
  copyText(text: string): Promise<void>;
  openExternal(url: string): void;
  setBadge(count: number): void;
  requestNotificationPermission(): Promise<boolean>;
}

class BrowserPlatform implements Platform {
  readonly name = 'browser' as const;
  notify(title: string, body: string, opts?: { onClick?: () => void }) {
    if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
      const n = new Notification(title, { body, tag: 'ar' });
      if (opts?.onClick) n.onclick = opts.onClick;
    }
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

const browser = new BrowserPlatform();

export function getPlatform(): Platform {
  return browser;
}
