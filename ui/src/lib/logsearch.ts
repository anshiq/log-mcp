import { LogService } from './api/services';
import { levelRank } from './state/logs.svelte';

export interface SearchHit {
  key: string;
  proc: string;
  ts: number;
  stream: string;
  level: string;
  text: string;
  before: string[];
  after: string[];
}

export interface SearchOptions {
  groups: Map<string, string[]>;
  query: string;
  regex: boolean;
  caseSensitive: boolean;
  minLevel: string;
  streams: string[] | null;
  maxRows: number;
}

export interface SearchOutcome {
  hits: SearchHit[];
  truncated: boolean;
}

export function epochMs(v: unknown): number {
  if (typeof v === 'number') {
    if (v > 1e17) return v / 1e6;
    if (v > 1e14) return v / 1e3;
    if (v > 1e11) return v;
    return v * 1000;
  }
  if (typeof v === 'string' && v !== '') {
    const d = Date.parse(v);
    if (!Number.isNaN(d)) return d;
    const n = Number(v);
    if (!Number.isNaN(n)) return epochMs(n);
  }
  return 0;
}

function ctxLines(v: unknown): string[] {
  if (!Array.isArray(v)) return [];
  return v.map((x) => String((x as Record<string, unknown>)['line'] ?? ''));
}

export function toSearchHit(raw: Record<string, unknown>): SearchHit {
  const proc = String(raw['processId'] ?? '');
  const ts = epochMs(raw['timestamp'] ?? raw['ts']);
  const text = String(raw['line'] ?? '');
  const ctx = (raw['context'] ?? {}) as Record<string, unknown>;
  return {
    key: `${proc}|${Math.floor(ts / 1000)}|${text}`,
    proc,
    ts,
    stream: String(raw['stream'] ?? 'stdout'),
    level: String(raw['level'] ?? ''),
    text,
    before: ctxLines(ctx['before']),
    after: ctxLines(ctx['after'])
  };
}

export async function searchLogs(o: SearchOptions): Promise<SearchOutcome> {
  const jobs = [...o.groups].map(([ws, ids]) =>
    LogService.search({
      workspaceId: ws,
      processIds: ids,
      query: o.query,
      regex: o.regex,
      caseSensitive: o.caseSensitive,
      minLevel: o.minLevel === 'all' ? '' : o.minLevel,
      streams: o.streams ?? [],
      maxRows: o.maxRows,
      contextLines: 2
    })
  );
  const results = await Promise.all(jobs);
  const seen = new Map<string, SearchHit>();
  let truncated = false;
  for (const r of results) {
    if (r.truncatedScan) truncated = true;
    for (const raw of r.matches ?? []) {
      const h = toSearchHit(raw);
      const prev = seen.get(h.key);
      if (!prev || (prev.before.length === 0 && prev.after.length === 0 && (h.before.length > 0 || h.after.length > 0))) seen.set(h.key, h);
    }
  }
  const min = o.minLevel === 'all' ? 0 : levelRank(o.minLevel);
  const hits = [...seen.values()].filter((h) => (min === 0 || levelRank(h.level) >= min) && (!o.streams || h.stream === 'system' || o.streams.includes(h.stream)));
  hits.sort((a, b) => b.ts - a.ts);
  return { hits, truncated };
}
