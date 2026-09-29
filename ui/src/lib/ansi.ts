export interface AnsiSeg {
  t: string;
  style: string;
  cls: string;
}

export interface AnsiSpan extends AnsiSeg {
  hit: boolean;
}

export interface Matcher {
  active: boolean;
  error: string;
  re: RegExp | null;
  test: (text: string) => boolean;
}

const TOKEN = /\u001b\[([0-9;:]*)([@-~])|\u001b\][^\u0007\u001b]*(?:\u0007|\u001b\\)|\u001b[@-_]/g;
const CUBE = [0, 95, 135, 175, 215, 255];

function color256(n: number): string {
  if (n < 0 || n > 255) return '';
  if (n < 16) return `var(--ansi-${n})`;
  if (n < 232) {
    const c = n - 16;
    return `rgb(${CUBE[Math.floor(c / 36)]},${CUBE[Math.floor(c / 6) % 6]},${CUBE[c % 6]})`;
  }
  const g = 8 + (n - 232) * 10;
  return `rgb(${g},${g},${g})`;
}

function rgb(r: number, g: number, b: number): string {
  const ok = (v: number) => Number.isFinite(v) && v >= 0 && v <= 255;
  return ok(r) && ok(g) && ok(b) ? `rgb(${r | 0},${g | 0},${b | 0})` : '';
}

interface Pen {
  fg: string;
  bg: string;
  bold: boolean;
  dim: boolean;
  italic: boolean;
  underline: boolean;
  strike: boolean;
  inverse: boolean;
}

function freshPen(): Pen {
  return { fg: '', bg: '', bold: false, dim: false, italic: false, underline: false, strike: false, inverse: false };
}

function applySgr(pen: Pen, params: number[]) {
  for (let i = 0; i < params.length; i++) {
    const p = params[i] as number;
    if (p === 0) Object.assign(pen, freshPen());
    else if (p === 1) pen.bold = true;
    else if (p === 2) pen.dim = true;
    else if (p === 3) pen.italic = true;
    else if (p === 4) pen.underline = true;
    else if (p === 7) pen.inverse = true;
    else if (p === 9) pen.strike = true;
    else if (p === 22) {
      pen.bold = false;
      pen.dim = false;
    } else if (p === 23) pen.italic = false;
    else if (p === 24) pen.underline = false;
    else if (p === 27) pen.inverse = false;
    else if (p === 29) pen.strike = false;
    else if (p >= 30 && p <= 37) pen.fg = `var(--ansi-${p - 30})`;
    else if (p >= 90 && p <= 97) pen.fg = `var(--ansi-${p - 90 + 8})`;
    else if (p >= 40 && p <= 47) pen.bg = `var(--ansi-${p - 40})`;
    else if (p >= 100 && p <= 107) pen.bg = `var(--ansi-${p - 100 + 8})`;
    else if (p === 39) pen.fg = '';
    else if (p === 49) pen.bg = '';
    else if (p === 38 || p === 48) {
      const mode = params[i + 1];
      let value = '';
      if (mode === 5) {
        value = color256(params[i + 2] as number);
        i += 2;
      } else if (mode === 2) {
        value = rgb(params[i + 2] as number, params[i + 3] as number, params[i + 4] as number);
        i += 4;
      }
      if (p === 38) pen.fg = value;
      else pen.bg = value;
    }
  }
}

function penStyle(pen: Pen): { style: string; cls: string } {
  let fg = pen.fg;
  let bg = pen.bg;
  if (pen.inverse) {
    const f = fg || 'var(--text-0)';
    const b = bg || 'var(--bg-0)';
    fg = b;
    bg = f;
  }
  let style = '';
  if (fg) style += `color:${fg};`;
  if (bg) style += `background:${bg};`;
  let cls = '';
  if (pen.bold) cls += ' ab';
  if (pen.dim) cls += ' ad';
  if (pen.italic) cls += ' ai';
  if (pen.underline) cls += ' au';
  if (pen.strike) cls += ' as';
  return { style, cls: cls.trim() };
}

function clean(text: string): string {
  return text.indexOf('\r') === -1 ? text : text.replace(/\r/g, '');
}

export function parseAnsi(text: string): AnsiSeg[] {
  if (text.indexOf('\u001b') === -1) {
    const t = clean(text);
    return t === '' ? [] : [{ t, style: '', cls: '' }];
  }
  const out: AnsiSeg[] = [];
  const pen = freshPen();
  let last = 0;
  TOKEN.lastIndex = 0;
  const push = (chunk: string) => {
    const t = clean(chunk);
    if (t === '') return;
    const s = penStyle(pen);
    out.push({ t, style: s.style, cls: s.cls });
  };
  let m: RegExpExecArray | null;
  while ((m = TOKEN.exec(text))) {
    push(text.slice(last, m.index));
    last = m.index + m[0].length;
    if (m[2] === 'm') {
      const raw = m[1] ?? '';
      const params = raw === '' ? [0] : raw.split(/[;:]/).map((v) => (v === '' ? 0 : Number(v)));
      applySgr(pen, params);
    }
  }
  push(text.slice(last));
  return out;
}

export function stripAnsi(text: string): string {
  if (text.indexOf('\u001b') === -1) return text;
  return text.replace(TOKEN, '').replace(/\u001b/g, '');
}

export function plainLength(text: string): number {
  return text.indexOf('\u001b') === -1 ? text.length : stripAnsi(text).length;
}

const ESC_MAP: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };

export function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (c) => ESC_MAP[c] as string);
}

export function ansiToHtml(text: string): string {
  let html = '';
  for (const seg of parseAnsi(text)) {
    const safe = escapeHtml(seg.t);
    if (!seg.style && !seg.cls) html += safe;
    else html += `<span${seg.cls ? ` class="${seg.cls}"` : ''}${seg.style ? ` style="${seg.style}"` : ''}>${safe}</span>`;
  }
  return html;
}

export function toSpans(segs: AnsiSeg[], re: RegExp | null): AnsiSpan[] {
  if (!re) return segs.map((s) => ({ ...s, hit: false }));
  let plain = '';
  for (const s of segs) plain += s.t;
  const ranges: number[] = [];
  re.lastIndex = 0;
  let m: RegExpExecArray | null;
  let guard = 0;
  while ((m = re.exec(plain)) && guard++ < 200) {
    if (m[0].length === 0) {
      re.lastIndex++;
      continue;
    }
    ranges.push(m.index, m.index + m[0].length);
  }
  re.lastIndex = 0;
  if (ranges.length === 0) return segs.map((s) => ({ ...s, hit: false }));
  const out: AnsiSpan[] = [];
  let pos = 0;
  let ri = 0;
  for (const seg of segs) {
    const end = pos + seg.t.length;
    let cur = pos;
    while (cur < end) {
      while (ri * 2 < ranges.length && (ranges[ri * 2 + 1] as number) <= cur) ri++;
      const rs = ranges[ri * 2];
      if (rs === undefined || rs >= end) {
        out.push({ t: seg.t.slice(cur - pos), style: seg.style, cls: seg.cls, hit: false });
        cur = end;
      } else if (rs > cur) {
        out.push({ t: seg.t.slice(cur - pos, rs - pos), style: seg.style, cls: seg.cls, hit: false });
        cur = rs;
      } else {
        const re2 = Math.min(ranges[ri * 2 + 1] as number, end);
        out.push({ t: seg.t.slice(cur - pos, re2 - pos), style: seg.style, cls: seg.cls, hit: true });
        cur = re2;
      }
    }
    pos = end;
  }
  return out;
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

const NONE: Matcher = { active: false, error: '', re: null, test: () => true };

export function buildMatcher(query: string, regex: boolean, caseSensitive: boolean): Matcher {
  if (query === '') return NONE;
  const flags = caseSensitive ? '' : 'i';
  const source = regex ? query : escapeRegex(query);
  try {
    const testRe = new RegExp(source, flags);
    const hiRe = new RegExp(source, flags + 'g');
    return { active: true, error: '', re: hiRe, test: (text) => testRe.test(text) };
  } catch (e) {
    return { active: false, error: e instanceof Error ? e.message.replace(/^Invalid regular expression: /, '') : 'invalid pattern', re: null, test: () => true };
  }
}
