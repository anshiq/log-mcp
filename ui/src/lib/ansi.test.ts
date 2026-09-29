import { describe, expect, it } from 'vitest';
import { ansiToHtml, buildMatcher, parseAnsi, plainLength, stripAnsi, toSpans } from './ansi';

describe('ansiToHtml', () => {
  it('escapes html in plain text', () => {
    const out = ansiToHtml('<img onerror=1>');
    expect(out).toBe('&lt;img onerror=1&gt;');
    expect(out).not.toContain('<img');
  });

  it('escapes html inside colored segments', () => {
    const out = ansiToHtml('\u001b[31m<script>alert(1)</script>\u001b[0m');
    expect(out).not.toContain('<script');
    expect(out).toContain('&lt;script&gt;');
    expect(out).toContain('color:var(--ansi-1)');
  });

  it('escapes quotes and ampersands', () => {
    expect(ansiToHtml(`a & "b" 'c'`)).toBe('a &amp; &quot;b&quot; &#39;c&#39;');
  });

  it('never emits attacker controlled attribute values', () => {
    const out = ansiToHtml('\u001b[38;2;999;0;0m"><b>x\u001b[0m');
    expect(out).not.toContain('<b>');
  });
});

describe('parseAnsi', () => {
  it('returns a single segment for plain text', () => {
    expect(parseAnsi('hello')).toEqual([{ t: 'hello', style: '', cls: '' }]);
  });

  it('applies and resets colors', () => {
    const segs = parseAnsi('a\u001b[32mb\u001b[0mc');
    expect(segs.map((s) => s.t)).toEqual(['a', 'b', 'c']);
    expect(segs[1]?.style).toBe('color:var(--ansi-2);');
    expect(segs[2]?.style).toBe('');
  });

  it('handles bold, bright and 256 colors', () => {
    const segs = parseAnsi('\u001b[1;91mx\u001b[38;5;196my\u001b[38;2;1;2;3mz');
    expect(segs[0]?.cls).toBe('ab');
    expect(segs[0]?.style).toBe('color:var(--ansi-9);');
    expect(segs[1]?.style).toContain('rgb(255,0,0)');
    expect(segs[2]?.style).toContain('rgb(1,2,3)');
  });

  it('drops non sgr sequences', () => {
    expect(stripAnsi('a\u001b[2Kb\u001b]0;title\u0007c')).toBe('abc');
    expect(plainLength('\u001b[31mred\u001b[0m')).toBe(3);
  });
});

describe('toSpans and matcher', () => {
  it('marks matches across segments', () => {
    const segs = parseAnsi('ab\u001b[31mcd\u001b[0mef');
    const spans = toSpans(segs, /bcde/gi);
    expect(spans.filter((s) => s.hit).map((s) => s.t).join('')).toBe('bcde');
    expect(spans.map((s) => s.t).join('')).toBe('abcdef');
  });

  it('builds plain and regex matchers', () => {
    expect(buildMatcher('a.c', false, false).test('xa.cx')).toBe(true);
    expect(buildMatcher('a.c', false, false).test('abc')).toBe(false);
    expect(buildMatcher('a.c', true, false).test('ABC')).toBe(true);
    expect(buildMatcher('ABC', false, true).test('abc')).toBe(false);
  });

  it('reports invalid regex without throwing', () => {
    const m = buildMatcher('(', true, false);
    expect(m.error).not.toBe('');
    expect(m.active).toBe(false);
    expect(m.test('anything')).toBe(true);
  });
});
