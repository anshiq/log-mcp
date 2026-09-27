export interface RouteMatch {
  path: string;
  params: Record<string, string>;
}

const titles: [RegExp, string][] = [
  [/^\/$/, 'Overview · agent-runtime'],
  [/^\/processes\/[^/]+/, 'Process detail · agent-runtime'],
  [/^\/processes/, 'Processes · agent-runtime'],
  [/^\/logs/, 'Logs · agent-runtime'],
  [/^\/apps/, 'Apps · agent-runtime'],
  [/^\/config/, 'Config · agent-runtime'],
  [/^\/events/, 'Events · agent-runtime'],
  [/^\/projects/, 'Projects · agent-runtime'],
  [/^\/sessions/, 'Sessions · agent-runtime'],
  [/^\/integrations/, 'Integrations · agent-runtime'],
  [/^\/audit/, 'Audit · agent-runtime'],
  [/^\/settings/, 'Settings · agent-runtime'],
  [/^\/login/, 'Login · agent-runtime']
];

function parseHash(): string {
  const h = window.location.hash.replace(/^#/, '');
  return h || '/';
}

function applyTitle(path: string) {
  const clean = path.split('?')[0] ?? '/';
  for (const [re, t] of titles) {
    if (re.test(clean)) {
      document.title = t;
      return;
    }
  }
  document.title = 'agent-runtime';
}

function compile(pattern: string): { re: RegExp; keys: string[] } {
  const keys: string[] = [];
  const body = pattern
    .split('/')
    .map((seg) => {
      if (seg.startsWith(':')) {
        const optional = seg.endsWith('?');
        keys.push(seg.slice(1).replace(/\?$/, ''));
        return optional ? '(?:/([^/]+))?' : '/([^/]+)';
      }
      return seg ? `/${seg}` : '';
    })
    .join('');
  return { re: new RegExp(`^${body || '/'}$`), keys };
}

type LeaveGuard = () => boolean | Promise<boolean>;

class Router {
  path = $state(typeof window !== 'undefined' ? parseHash() : '/');
  private guards: LeaveGuard[] = [];
  private lastScroll = new Map<string, number>();

  constructor() {
    if (typeof window !== 'undefined') {
      window.addEventListener('hashchange', () => {
        const cur = document.querySelector('.content');
        if (cur) this.lastScroll.set(this.path, cur.scrollTop);
        this.path = parseHash();
        applyTitle(this.path);
        if (__APP_TARGET__ === 'web') {
          const { hasAuthToken } = { hasAuthToken: () => !!sessionStorage.getItem('ar.token') || !!localStorage.getItem('ar.token') };
          if (!hasAuthToken() && !this.path.startsWith('/login')) {
            this.navigate('/login?next=' + encodeURIComponent(this.path), { replace: true });
            return;
          }
        }
        requestAnimationFrame(() => {
          const el = document.querySelector('.content');
          if (el) el.scrollTop = this.lastScroll.get(this.path) ?? 0;
        });
      });
      applyTitle(this.path);
    }
  }

  async navigate(path: string, opts: { replace?: boolean } = {}) {
    for (const g of this.guards) {
      const ok = await g();
      if (!ok) return;
    }
    const target = `#${path}`;
    if (opts.replace) {
      const url = new URL(window.location.href);
      url.hash = target;
      window.history.replaceState(null, '', url.toString());
      this.path = path;
      applyTitle(path);
    } else {
      window.location.hash = target;
    }
  }

  match(pattern: string): RouteMatch | null {
    const { re, keys } = compile(pattern);
    const [rawPath, query] = this.path.split('?');
    const m = re.exec(rawPath ?? '/');
    if (!m) return null;
    const params: Record<string, string> = {};
    keys.forEach((k, i) => {
      if (m[i + 1] !== undefined) params[k] = decodeURIComponent(m[i + 1] as string);
    });
    if (query) {
      for (const [k, v] of new URLSearchParams(query)) params[k] = v;
    }
    return { path: rawPath ?? '/', params };
  }

  onBeforeLeave(g: LeaveGuard): () => void {
    this.guards.push(g);
    return () => {
      this.guards = this.guards.filter((x) => x !== g);
    };
  }

  link(node: HTMLAnchorElement, path: string) {
    const click = (e: MouseEvent) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey) return;
      e.preventDefault();
      void this.navigate(path);
    };
    node.addEventListener('click', click);
    return {
      destroy() {
        node.removeEventListener('click', click);
      }
    };
  }
}

export const router = new Router();

export function navigate(path: string, opts?: { replace?: boolean }) {
  return router.navigate(path, opts);
}
