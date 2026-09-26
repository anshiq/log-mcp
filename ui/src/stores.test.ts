import { describe, it, expect } from 'vitest';
import { createProcessStore } from '../src/lib/stores';

describe('process store', () => {
  it('starts empty', () => {
    const s = createProcessStore();
    let size = -1;
    const unsub = s.processes.subscribe((m) => {
      size = m.size;
    });
    expect(size).toBe(0);
    unsub();
  });
});
