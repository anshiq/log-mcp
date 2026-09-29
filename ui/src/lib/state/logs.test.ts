import { describe, expect, it } from 'vitest';
import { LogBuffer, LogSession, levelRank, type BufferedLine } from './logs.svelte';
import { buildMatcher } from '../ansi';

function line(i: number, over: Partial<BufferedLine> = {}): BufferedLine {
  return { id: i, ts: i, stream: 'stdout', level: 'info', text: `l${i}`, proc: 'p', ...over };
}

function session(max = 100) {
  return new LogSession({ max, schedule: () => {} });
}

describe('LogBuffer', () => {
  it('appends and wraps', () => {
    const b = new LogBuffer(3);
    for (let i = 0; i < 5; i++) b.append({ id: i, ts: i, stream: 'stdout', level: 'info', text: `l${i}`, proc: 'p' });
    expect(b.length).toBe(3);
    expect(b.at(0).text).toBe('l2');
  });
  it('filters incrementally', () => {
    const b = new LogBuffer(10);
    b.append({ id: 0, ts: 0, stream: 'stdout', level: 'info', text: 'hello', proc: 'p' });
    b.append({ id: 1, ts: 0, stream: 'stderr', level: 'error', text: 'boom', proc: 'p' });
    const v = b.filter((l) => l.level === 'error');
    expect(v.length).toBe(1);
  });
  it('keeps sequence numbers stable across compaction', () => {
    const b = new LogBuffer(3000);
    for (let i = 0; i < 10000; i++) b.append(line(i));
    expect(b.length).toBe(3000);
    expect(b.firstSeq).toBe(7000);
    expect(b.endSeq).toBe(10000);
    expect(b.atSeq(9999).text).toBe('l9999');
    expect(b.at(0).text).toBe('l7000');
  });
  it('tracks plain length and rank', () => {
    const b = new LogBuffer(10);
    b.append(line(0, { text: '\u001b[31mred\u001b[0m', level: 'error' }));
    expect(b.plainAtSeq(0)).toBe(3);
    expect(b.maxPlain).toBe(3);
    expect(b.rank[0]).toBe(3);
  });
});

describe('levelRank', () => {
  it('orders levels', () => {
    expect(levelRank('debug')).toBeLessThan(levelRank(''));
    expect(levelRank('warn')).toBeGreaterThan(levelRank('info'));
    expect(levelRank('error')).toBeGreaterThan(levelRank('warning'));
    expect(levelRank('fatal')).toBeGreaterThan(levelRank('err'));
  });
});

describe('LogSession', () => {
  it('indexes appended lines incrementally', () => {
    const s = session();
    s.setCriteria({ procs: null, streams: null, minRank: 3, matcher: null });
    s.push(line(0));
    s.push(line(1, { level: 'error' }));
    s.push(line(2));
    s.flushNow();
    expect(s.total).toBe(3);
    expect(s.viewCount).toBe(1);
    expect(s.line(0).text).toBe('l1');
  });

  it('rebuilds when criteria change', () => {
    const s = session();
    for (let i = 0; i < 10; i++) s.push(line(i, { proc: i % 2 ? 'a' : 'b' }));
    s.flushNow();
    s.setCriteria({ procs: new Set(['a']), streams: null, minRank: 0, matcher: null });
    expect(s.viewCount).toBe(5);
    s.setCriteria({ procs: null, streams: null, minRank: 0, matcher: buildMatcher('l7', false, false) });
    expect(s.viewCount).toBe(1);
    expect(s.line(0).id).toBe(7);
  });

  it('filters by stream but always keeps system lines', () => {
    const s = session();
    s.push(line(0, { stream: 'stdout' }));
    s.push(line(1, { stream: 'stderr' }));
    s.push(line(2, { stream: 'system' }));
    s.flushNow();
    s.setCriteria({ procs: null, streams: new Set(['stderr']), minRank: 0, matcher: null });
    expect(s.viewCount).toBe(2);
  });

  it('freezes the visible count while paused and catches up on resume', () => {
    const s = session();
    for (let i = 0; i < 5; i++) s.push(line(i));
    s.flushNow();
    s.pause();
    for (let i = 5; i < 9; i++) s.push(line(i));
    s.flushNow();
    expect(s.viewCount).toBe(5);
    expect(s.liveCount).toBe(9);
    s.resume();
    expect(s.viewCount).toBe(9);
  });

  it('trims the index when the buffer drops old lines', () => {
    const s = session(5);
    for (let i = 0; i < 12; i++) s.push(line(i));
    s.flushNow();
    expect(s.total).toBe(5);
    expect(s.viewCount).toBe(5);
    expect(s.line(0).id).toBe(7);
    expect(s.ordBase).toBe(7);
  });

  it('clears the view and bumps the epoch', () => {
    const s = session();
    s.push(line(0));
    s.flushNow();
    const e = s.epoch;
    s.clear();
    expect(s.viewCount).toBe(0);
    expect(s.epoch).toBeGreaterThan(e);
    s.push(line(1));
    s.flushNow();
    expect(s.viewCount).toBe(1);
    expect(s.numBase).toBe(1);
  });

  it('exports the filtered view', () => {
    const s = session();
    for (let i = 0; i < 4; i++) s.push(line(i, { level: i === 2 ? 'error' : 'info' }));
    s.flushNow();
    s.setCriteria({ procs: null, streams: null, minRank: 3, matcher: null });
    expect(s.viewLines().map((l) => l.text)).toEqual(['l2']);
  });
});
