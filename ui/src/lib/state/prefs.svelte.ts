type Theme = 'system' | 'light' | 'dark';
type Density = 'comfortable' | 'compact';
type TsFormat = 'relative' | 'local' | 'utc' | 'off';

interface Prefs {
  theme: Theme;
  density: Density;
  logFontSize: number;
  wrap: boolean;
  timestamps: TsFormat;
  maxBufferLines: number;
  defaultBacklog: number;
  ansi: boolean;
  dnd: boolean;
  notifyOn: Record<string, boolean>;
  mutedProcesses: string[];
  sidebarCollapsed: boolean;
}

const KEY = 'ar.prefs.v1';

const defaults: Prefs = {
  theme: 'system',
  density: 'comfortable',
  logFontSize: 12,
  wrap: false,
  timestamps: 'local',
  maxBufferLines: 200000,
  defaultBacklog: 500,
  ansi: true,
  dnd: false,
  notifyOn: { crash: true, fail: true, alert: true },
  mutedProcesses: [],
  sidebarCollapsed: false
};

function load(): Prefs {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return { ...defaults };
    return { ...defaults, ...(JSON.parse(raw) as Partial<Prefs>) };
  } catch {
    return { ...defaults };
  }
}

class PrefsStore {
  data = $state<Prefs>(load());

  save() {
    try {
      localStorage.setItem(KEY, JSON.stringify(this.data));
    } catch {
    }
    this.applyTheme();
  }

  applyTheme() {
    if (typeof document === 'undefined') return;
    const t = this.data.theme;
    const resolved = t === 'system' ? (window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark') : t;
    document.documentElement.dataset['theme'] = resolved;
  }

  set<K extends keyof Prefs>(k: K, v: Prefs[K]) {
    this.data[k] = v;
    this.save();
  }

  toggleMute(id: string) {
    if (this.data.mutedProcesses.includes(id)) {
      this.data.mutedProcesses = this.data.mutedProcesses.filter((x) => x !== id);
    } else {
      this.data.mutedProcesses = [...this.data.mutedProcesses, id];
    }
    this.save();
  }
}

export const prefs = new PrefsStore();

if (typeof window !== 'undefined') {
  prefs.applyTheme();
  window.matchMedia('(prefers-color-scheme: light)').addEventListener?.('change', () => prefs.applyTheme());
}
