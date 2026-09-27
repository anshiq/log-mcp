import { describe, expect, it } from 'vitest';
import { bytes, duration, relativeTime } from './format';

describe('format', () => {
  it('bytes', () => {
    expect(bytes(512)).toBe('512 B');
    expect(bytes(2048)).toContain('KB');
  });
  it('duration', () => {
    expect(duration(500)).toBe('500ms');
    expect(duration(65000)).toContain('m');
  });
  it('relative', () => {
    expect(relativeTime(Date.now())).toBe('just now');
  });
});
