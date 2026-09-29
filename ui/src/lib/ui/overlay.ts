const stack: symbol[] = [];

export interface LayerOptions {
  onClose?: () => void;
  trap?: boolean;
  focus?: 'auto' | 'none' | string;
  restoreFocus?: boolean;
  escape?: boolean;
}

const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function focusables(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => el.offsetParent !== null || el === document.activeElement);
}

export function layer(node: HTMLElement, initial: LayerOptions = {}) {
  let opts = initial;
  const id = Symbol('layer');
  stack.push(id);
  const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;

  function isTop() {
    return stack[stack.length - 1] === id;
  }

  function onKey(e: KeyboardEvent) {
    if (!isTop()) return;
    if (e.key === 'Escape' && opts.escape !== false) {
      e.stopPropagation();
      e.preventDefault();
      opts.onClose?.();
      return;
    }
    if (e.key !== 'Tab' || !opts.trap) return;
    const els = focusables(node);
    if (els.length === 0) {
      e.preventDefault();
      node.focus();
      return;
    }
    const first = els[0]!;
    const last = els[els.length - 1]!;
    const active = document.activeElement;
    if (!node.contains(active)) {
      e.preventDefault();
      first.focus();
    } else if (e.shiftKey && (active === first || active === node)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  }

  window.addEventListener('keydown', onKey, true);

  queueMicrotask(() => {
    const mode = opts.focus ?? 'auto';
    if (mode === 'none') return;
    if (mode !== 'auto') {
      node.querySelector<HTMLElement>(mode)?.focus();
      return;
    }
    const marked = node.querySelector<HTMLElement>('[data-autofocus]');
    if (marked) {
      marked.focus();
      return;
    }
    const body = node.querySelector<HTMLElement>('[data-layer-body]') ?? node;
    const first = focusables(body)[0] ?? focusables(node)[0];
    if (first) first.focus();
    else {
      node.tabIndex = -1;
      node.focus();
    }
  });

  return {
    update(next: LayerOptions) {
      opts = next;
    },
    destroy() {
      window.removeEventListener('keydown', onKey, true);
      const i = stack.indexOf(id);
      if (i >= 0) stack.splice(i, 1);
      if (opts.restoreFocus !== false && previous && document.contains(previous)) previous.focus();
    }
  };
}

export function outsideClick(node: HTMLElement, opts: { onOutside: () => void; ignore?: () => (HTMLElement | null)[] }) {
  let current = opts;
  function handler(e: PointerEvent) {
    const target = e.target as Node | null;
    if (!target) return;
    if (node.contains(target)) return;
    if (current.ignore?.().some((el) => el?.contains(target))) return;
    current.onOutside();
  }
  document.addEventListener('pointerdown', handler, true);
  return {
    update(next: typeof opts) {
      current = next;
    },
    destroy() {
      document.removeEventListener('pointerdown', handler, true);
    }
  };
}
