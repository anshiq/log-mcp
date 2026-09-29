import { describe, expect, it } from 'vitest';
import { collapseContext, diffLines, diffStats, parseUnified } from './diff';

describe('diffLines', () => {
  it('reports identical text as context only', () => {
    const rows = diffLines('a\nb\n', 'a\nb\n');
    expect(rows.every((r) => r.kind === 'ctx')).toBe(true);
    expect(rows).toHaveLength(2);
  });

  it('handles insertion without shifting later lines', () => {
    const rows = diffLines('a\nb\nc\n', 'a\nx\nb\nc\n');
    expect(diffStats(rows)).toEqual({ added: 1, removed: 0 });
    const added = rows.find((r) => r.kind === 'add');
    expect(added?.text).toBe('x');
    expect(added?.newNo).toBe(2);
  });

  it('handles replacement and empty sides', () => {
    expect(diffStats(diffLines('', 'a\nb'))).toEqual({ added: 2, removed: 0 });
    expect(diffStats(diffLines('a\nb', ''))).toEqual({ added: 0, removed: 2 });
    expect(diffStats(diffLines('a\nb\nc', 'a\nB\nc'))).toEqual({ added: 1, removed: 1 });
  });
});

describe('collapseContext', () => {
  it('folds long unchanged runs', () => {
    const old = Array.from({ length: 20 }, (_, i) => `l${i}`).join('\n');
    const next = old.replace('l10', 'changed');
    const rows = collapseContext(diffLines(old, next), 2);
    expect(rows.some((r) => r.kind === 'hunk')).toBe(true);
    expect(rows.length).toBeLessThan(12);
  });
});

describe('parseUnified', () => {
  it('classifies simple and git-style lines', () => {
    const rows = parseUnified('- old\n+ new\n');
    expect(rows.map((r) => r.kind)).toEqual(['del', 'add']);
    expect(rows[0]?.text).toBe('old');
    const git = parseUnified('--- a\n+++ b\n@@ -3,2 +3,2 @@\n ctx\n-x\n+y');
    expect(git.map((r) => r.kind)).toEqual(['meta', 'meta', 'hunk', 'ctx', 'del', 'add']);
    expect(git[3]?.oldNo).toBe(3);
    expect(git[5]?.newNo).toBe(4);
  });
});
