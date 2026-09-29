export type Placement = 'top' | 'bottom' | 'left' | 'right' | 'top-start' | 'top-end' | 'bottom-start' | 'bottom-end';

export interface FloatOptions {
  anchor: HTMLElement | { x: number; y: number } | null;
  placement?: Placement;
  offset?: number;
  matchWidth?: boolean;
  margin?: number;
}

export function floating(node: HTMLElement, initial: FloatOptions) {
  let opts = initial;
  document.body.appendChild(node);
  node.style.position = 'fixed';
  node.style.top = '0px';
  node.style.left = '0px';
  node.style.zIndex = 'var(--z-popover, 900)';

  function position() {
    const anchor = opts.anchor;
    if (!anchor) return;
    const offset = opts.offset ?? 6;
    const margin = opts.margin ?? 8;
    const rect =
      anchor instanceof HTMLElement
        ? anchor.getBoundingClientRect()
        : ({ left: anchor.x, right: anchor.x, top: anchor.y, bottom: anchor.y, width: 0, height: 0 } as DOMRect);
    if (opts.matchWidth) node.style.minWidth = `${rect.width}px`;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    node.style.maxHeight = `${vh - margin * 2}px`;
    const nr = node.getBoundingClientRect();
    const placement = opts.placement ?? 'bottom-start';
    const [side, align] = placement.split('-') as [string, string | undefined];
    let s = side;
    if (s === 'bottom' && rect.bottom + offset + nr.height > vh - margin && rect.top - offset - nr.height >= margin) s = 'top';
    else if (s === 'top' && rect.top - offset - nr.height < margin && rect.bottom + offset + nr.height <= vh - margin) s = 'bottom';
    else if (s === 'right' && rect.right + offset + nr.width > vw - margin && rect.left - offset - nr.width >= margin) s = 'left';
    else if (s === 'left' && rect.left - offset - nr.width < margin && rect.right + offset + nr.width <= vw - margin) s = 'right';
    let top = 0;
    let left = 0;
    if (s === 'bottom' || s === 'top') {
      top = s === 'bottom' ? rect.bottom + offset : rect.top - offset - nr.height;
      if (align === 'end') left = rect.right - nr.width;
      else if (align === 'start') left = rect.left;
      else left = rect.left + rect.width / 2 - nr.width / 2;
    } else {
      left = s === 'right' ? rect.right + offset : rect.left - offset - nr.width;
      if (align === 'end') top = rect.bottom - nr.height;
      else if (align === 'start') top = rect.top;
      else top = rect.top + rect.height / 2 - nr.height / 2;
    }
    left = Math.max(margin, Math.min(left, vw - nr.width - margin));
    top = Math.max(margin, Math.min(top, vh - nr.height - margin));
    node.style.left = `${Math.round(left)}px`;
    node.style.top = `${Math.round(top)}px`;
    node.dataset['side'] = s;
  }

  position();
  const raf = requestAnimationFrame(position);
  const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(position) : null;
  ro?.observe(node);
  window.addEventListener('resize', position);
  window.addEventListener('scroll', position, true);

  return {
    update(next: FloatOptions) {
      opts = next;
      position();
    },
    destroy() {
      cancelAnimationFrame(raf);
      ro?.disconnect();
      window.removeEventListener('resize', position);
      window.removeEventListener('scroll', position, true);
    }
  };
}
