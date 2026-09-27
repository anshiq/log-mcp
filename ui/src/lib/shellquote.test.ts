import { describe, expect, it } from 'vitest';
import { splitCommand } from './shellquote';

describe('splitCommand', () => {
  it('splits simple', () => {
    expect(splitCommand('npm run dev')).toEqual(['npm', 'run', 'dev']);
  });
  it('handles quotes', () => {
    expect(splitCommand('echo "a b" c')).toEqual(['echo', 'a b', 'c']);
  });
});
