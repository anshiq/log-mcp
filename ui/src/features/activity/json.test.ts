import { describe, expect, it } from 'vitest';
import { prettyJson, tokenizeJson } from './json';

describe('tokenizeJson', () => {
  it('separates keys, strings, numbers and literals', () => {
    const segs = tokenizeJson(prettyJson({ exit_code: 3, ok: false, msg: 'a "b"' }));
    expect(segs.filter((s) => s.kind === 'key').map((s) => s.text)).toEqual(['"exit_code"', '"ok"', '"msg"']);
    expect(segs.find((s) => s.kind === 'number')?.text).toBe('3');
    expect(segs.find((s) => s.kind === 'literal')?.text).toBe('false');
    expect(segs.find((s) => s.kind === 'string')?.text).toBe('"a \\"b\\""');
    expect(segs.map((s) => s.text).join('')).toBe(prettyJson({ exit_code: 3, ok: false, msg: 'a "b"' }));
  });
});
