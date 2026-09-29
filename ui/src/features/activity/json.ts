export interface JsonSegment {
  text: string;
  kind: 'key' | 'string' | 'number' | 'literal' | 'punct';
}

const TOKEN = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g;

export function tokenizeJson(source: string): JsonSegment[] {
  const out: JsonSegment[] = [];
  let last = 0;
  for (const m of source.matchAll(TOKEN)) {
    const at = m.index ?? 0;
    if (at > last) out.push({ text: source.slice(last, at), kind: 'punct' });
    if (m[1] !== undefined) {
      out.push({ text: m[1], kind: m[2] ? 'key' : 'string' });
      if (m[2]) out.push({ text: m[2], kind: 'punct' });
    } else if (m[3] !== undefined) {
      out.push({ text: m[3], kind: 'literal' });
    } else {
      out.push({ text: m[0], kind: 'number' });
    }
    last = at + m[0].length;
  }
  if (last < source.length) out.push({ text: source.slice(last), kind: 'punct' });
  return out;
}

export function prettyJson(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2) ?? '';
  } catch {
    return String(value);
  }
}
