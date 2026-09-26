export interface RouteMatch {
  path: string;
  params: Record<string, string>;
}

function parseHash(): string {
  const h = window.location.hash.replace(/^#/, '');
  return h || '/';
}

function compile(pattern: string): { re: RegExp; keys: string[] } {
  const keys: string[] = [];
  const re = pattern
    .split('/')
    .map((seg) => {
      if (seg.startsWith(':')) {
        keys.push(seg.slice(1).replace(/\?$/, ''));
        return seg.endsWith('?') ? '(?:/([^/]+))?' : '([^/]+)';
      }
      return seg ? `/${seg}` : '';
    })
    .join('');
  return { re: new RegExp(`^${re || '/'}$`), keys };
}

class Router {
  path = $state(parseHash());

  constructor() {
    if (typeof window !== 'undefined') {
      window.addEventListener('hashchange', () => {
        this.path = parseHash();
      });
    }
  }

  navigate(path: string, opts: { replace?: boolean } = {}) {
    const target = `#${path}`;
    if (opts.replace) {
      const url = new URL(window.location.href);
      url.hash = target;
      window.history.replaceState(null, '', url.toString());
      this.path = path;
    } else {
      window.location.hash = target;
    }
  }

  match(pattern: string): RouteMatch | null {
    const { re, keys } = compile(pattern);
    const [rawPath, query] = this.path.split('?');
    const m = re.exec(rawPath);
    if (!m) return null;
    const params: Record<string, string> = {};
    keys.forEach((k, i) => {
      if (m[i + 1] !== undefined) params[k] = decodeURIComponent(m[i + 1]);
    });
    if (query) {
      for (const [k, v] of new URLSearchParams(query)) params[k] = v;
    }
    return { path: rawPath, params };
  }
}

export const router = new Router();
