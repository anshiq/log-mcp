import { beforeEach, describe, expect, it } from 'vitest';
import { buildLabels, commandPreview, processHue, resetProcessHues } from './logproc';

describe('logproc', () => {
  beforeEach(resetProcessHues);

  it('gives a stable hue per process', () => {
    expect(processHue('proc_a')).toBe(processHue('proc_a'));
  });

  it('avoids hue collisions for a handful of processes', () => {
    const ids = Array.from({ length: 12 }, (_, i) => `proc_${i}x${i * 7}`);
    const hues = new Set(ids.map(processHue));
    expect(hues.size).toBe(12);
  });

  it('disambiguates identical names with an id suffix', () => {
    const labels = buildLabels([
      { id: 'proc_aaaa1111', app: '', command: '/bin/sh' },
      { id: 'proc_bbbb2222', app: '', command: 'sh' },
      { id: 'proc_cccc3333', app: 'api', command: 'node' }
    ]);
    expect(labels.get('proc_aaaa1111')).toBe('sh·1111');
    expect(labels.get('proc_bbbb2222')).toBe('sh·2222');
    expect(labels.get('proc_cccc3333')).toBe('api');
  });

  it('quotes arguments with spaces in the command preview', () => {
    expect(commandPreview({ command: 'sh', args: ['-c', 'echo hi'] })).toBe('sh -c "echo hi"');
  });
});
