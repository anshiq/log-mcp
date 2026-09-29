export type LogMode = 'live' | 'history';
export type LevelFilter = 'all' | 'debug' | 'info' | 'warn' | 'error';

const KEY = 'ar.logs.view.v1';

interface ViewPrefs {
  tags: boolean;
  levels: boolean;
  lineNumbers: boolean;
  zebra: boolean;
}

const defaults: ViewPrefs = { tags: true, levels: true, lineNumbers: true, zebra: true };

function load(): ViewPrefs {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return { ...defaults };
    return { ...defaults, ...(JSON.parse(raw) as Partial<ViewPrefs>) };
  } catch {
    return { ...defaults };
  }
}

export const LEVEL_RANK: Record<LevelFilter, number> = { all: 0, debug: 0, info: 1, warn: 2, error: 3 };

export function parseSelection(path: string): string[] {
  const q = path.split('?')[1];
  if (!q) return [];
  const p = new URLSearchParams(q).get('p');
  if (!p) return [];
  return p.split(',').map((s) => s.trim()).filter(Boolean);
}

export function selectionPath(ids: string[]): string {
  return ids.length === 0 ? '/logs' : `/logs?p=${ids.map(encodeURIComponent).join(',')}`;
}

export class LogsUi {
  mode = $state<LogMode>('live');
  selected = $state<string[]>([]);
  query = $state('');
  regex = $state(false);
  caseSensitive = $state(false);
  level = $state<LevelFilter>('all');
  stdout = $state(true);
  stderr = $state(true);
  view = $state<ViewPrefs>(load());

  get streams(): Set<string> | null {
    if (this.stdout && this.stderr) return null;
    const s = new Set<string>();
    if (this.stdout) s.add('stdout');
    if (this.stderr) s.add('stderr');
    return s;
  }

  get filtersActive(): boolean {
    return this.query !== '' || this.level !== 'all' || !this.stdout || !this.stderr;
  }

  resetFilters() {
    this.query = '';
    this.level = 'all';
    this.stdout = true;
    this.stderr = true;
  }

  toggleSelected(id: string) {
    this.selected = this.selected.includes(id) ? this.selected.filter((x) => x !== id) : [...this.selected, id];
  }

  saveView() {
    try {
      localStorage.setItem(KEY, JSON.stringify(this.view));
    } catch {
    }
  }
}
