import { cubicOut } from 'svelte/easing';

export function reducedMotion(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

interface PopParams {
  duration?: number;
  from?: number;
  y?: number;
}

export function pop(_node: Element, { duration = 150, from = 0.96, y = 6 }: PopParams = {}) {
  return {
    duration: reducedMotion() ? 0 : duration,
    easing: cubicOut,
    css: (t: number) => `opacity:${t};transform:translateY(${(1 - t) * y}px) scale(${from + (1 - from) * t})`
  };
}

interface FadeParams {
  duration?: number;
}

export function fadeIn(_node: Element, { duration = 140 }: FadeParams = {}) {
  return {
    duration: reducedMotion() ? 0 : duration,
    easing: cubicOut,
    css: (t: number) => `opacity:${t}`
  };
}

interface SlideParams {
  duration?: number;
  x?: number;
  y?: number;
}

export function slide(_node: Element, { duration = 220, x = 0, y = 0 }: SlideParams = {}) {
  return {
    duration: reducedMotion() ? 0 : duration,
    easing: cubicOut,
    css: (t: number) => `opacity:${0.4 + 0.6 * t};transform:translate(${(1 - t) * x}px,${(1 - t) * y}px)`
  };
}
