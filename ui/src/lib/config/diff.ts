export type DiffKind = 'add' | 'del' | 'ctx' | 'hunk' | 'meta';

export interface DiffRow {
  kind: DiffKind;
  oldNo: number | null;
  newNo: number | null;
  text: string;
}

export interface DiffStats {
  added: number;
  removed: number;
}

function splitLines(v: string): string[] {
  if (v === '') return [];
  return v.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n');
}

export function diffLines(oldText: string, newText: string): DiffRow[] {
  const a = splitLines(oldText);
  const b = splitLines(newText);
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let endA = a.length;
  let endB = b.length;
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--;
    endB--;
  }
  const midA = a.slice(start, endA);
  const midB = b.slice(start, endB);
  const n = midA.length;
  const m = midB.length;
  const rows: DiffRow[] = [];
  for (let i = 0; i < start; i++) rows.push({ kind: 'ctx', oldNo: i + 1, newNo: i + 1, text: a[i] ?? '' });

  if (n * m > 4_000_000) {
    midA.forEach((t, i) => rows.push({ kind: 'del', oldNo: start + i + 1, newNo: null, text: t }));
    midB.forEach((t, i) => rows.push({ kind: 'add', oldNo: null, newNo: start + i + 1, text: t }));
  } else {
    const w = m + 1;
    const table = new Uint32Array((n + 1) * w);
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        table[i * w + j] =
          midA[i] === midB[j] ? (table[(i + 1) * w + j + 1] ?? 0) + 1 : Math.max(table[(i + 1) * w + j] ?? 0, table[i * w + j + 1] ?? 0);
      }
    }
    let i = 0;
    let j = 0;
    while (i < n && j < m) {
      if (midA[i] === midB[j]) {
        rows.push({ kind: 'ctx', oldNo: start + i + 1, newNo: start + j + 1, text: midA[i] ?? '' });
        i++;
        j++;
      } else if ((table[(i + 1) * w + j] ?? 0) >= (table[i * w + j + 1] ?? 0)) {
        rows.push({ kind: 'del', oldNo: start + i + 1, newNo: null, text: midA[i] ?? '' });
        i++;
      } else {
        rows.push({ kind: 'add', oldNo: null, newNo: start + j + 1, text: midB[j] ?? '' });
        j++;
      }
    }
    while (i < n) {
      rows.push({ kind: 'del', oldNo: start + i + 1, newNo: null, text: midA[i] ?? '' });
      i++;
    }
    while (j < m) {
      rows.push({ kind: 'add', oldNo: null, newNo: start + j + 1, text: midB[j] ?? '' });
      j++;
    }
  }

  for (let k = 0; k < a.length - endA; k++) {
    rows.push({ kind: 'ctx', oldNo: endA + k + 1, newNo: endB + k + 1, text: a[endA + k] ?? '' });
  }
  return rows;
}

export function parseUnified(diff: string): DiffRow[] {
  const rows: DiffRow[] = [];
  let oldNo = 1;
  let newNo = 1;
  for (const line of diff.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n')) {
    if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff ') || line.startsWith('index ')) {
      rows.push({ kind: 'meta', oldNo: null, newNo: null, text: line });
    } else if (line.startsWith('@@')) {
      const m = /@@ -(\d+)(?:,\d+)? \+(\d+)/.exec(line);
      if (m) {
        oldNo = Number(m[1]);
        newNo = Number(m[2]);
      }
      rows.push({ kind: 'hunk', oldNo: null, newNo: null, text: line });
    } else if (line.startsWith('+')) {
      rows.push({ kind: 'add', oldNo: null, newNo: newNo++, text: line.slice(line.startsWith('+ ') ? 2 : 1) });
    } else if (line.startsWith('-')) {
      rows.push({ kind: 'del', oldNo: oldNo++, newNo: null, text: line.slice(line.startsWith('- ') ? 2 : 1) });
    } else {
      rows.push({ kind: 'ctx', oldNo: oldNo++, newNo: newNo++, text: line.startsWith(' ') ? line.slice(1) : line });
    }
  }
  return rows;
}

export function collapseContext(rows: DiffRow[], context = 3): DiffRow[] {
  const keep = new Array<boolean>(rows.length).fill(false);
  rows.forEach((r, i) => {
    if (r.kind !== 'ctx') {
      for (let k = Math.max(0, i - context); k <= Math.min(rows.length - 1, i + context); k++) keep[k] = true;
    }
  });
  const out: DiffRow[] = [];
  let skipped = 0;
  rows.forEach((r, i) => {
    if (keep[i] || r.kind === 'hunk' || r.kind === 'meta') {
      if (skipped > 0) {
        out.push({ kind: 'hunk', oldNo: null, newNo: null, text: `${skipped} unchanged line${skipped === 1 ? '' : 's'}` });
        skipped = 0;
      }
      out.push(r);
    } else {
      skipped++;
    }
  });
  if (skipped > 0) out.push({ kind: 'hunk', oldNo: null, newNo: null, text: `${skipped} unchanged line${skipped === 1 ? '' : 's'}` });
  return out;
}

export function diffStats(rows: DiffRow[]): DiffStats {
  let added = 0;
  let removed = 0;
  for (const r of rows) {
    if (r.kind === 'add') added++;
    else if (r.kind === 'del') removed++;
  }
  return { added, removed };
}
