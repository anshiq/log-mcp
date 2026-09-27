import { describe, expect, it } from 'vitest';
import { LogBuffer } from './logs.svelte';

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
});
