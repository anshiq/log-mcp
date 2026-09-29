import { openStream, type StreamMessage, type StreamState } from '../api/transport';
import { toLogLine } from '../api/normalize';
import { plainLength, type Matcher } from '../ansi';

export interface BufferedLine {
  id: number;
  ts: number;
  stream: string;
  level: string;
  text: string;
  proc: string;
}

export type ConnState = 'idle' | 'connecting' | 'live' | 'reconnecting' | 'closed';

export interface LogCriteria {
  procs: Set<string> | null;
  streams: Set<string> | null;
  minRank: number;
  matcher: Matcher | null;
}

export const NO_CRITERIA: LogCriteria = { procs: null, streams: null, minRank: 0, matcher: null };

export function levelRank(level: string): number {
  switch (level.toLowerCase()) {
    case 'trace':
    case 'debug':
    case 'dbg':
      return 0;
    case 'warn':
    case 'warning':
      return 2;
    case 'error':
    case 'err':
      return 3;
    case 'fatal':
    case 'panic':
    case 'critical':
    case 'crit':
      return 4;
    default:
      return 1;
  }
}

export class LogBuffer {
  ids: number[] = [];
  ts: number[] = [];
  stream: string[] = [];
  level: string[] = [];
  text: string[] = [];
  proc: string[] = [];
  rank: number[] = [];
  plain: number[] = [];
  max: number;
  view: Uint32Array = new Uint32Array(0);
  maxPlain = 0;
  private head = 0;
  private abs0 = 0;

  constructor(max = 200000) {
    this.max = max;
  }

  get length(): number {
    return this.ids.length - this.head;
  }

  get firstSeq(): number {
    return this.abs0 + this.head;
  }

  get endSeq(): number {
    return this.abs0 + this.ids.length;
  }

  append(l: BufferedLine): number {
    const seq = this.abs0 + this.ids.length;
    const plain = plainLength(l.text);
    this.ids.push(l.id);
    this.ts.push(l.ts);
    this.stream.push(l.stream);
    this.level.push(l.level);
    this.text.push(l.text);
    this.proc.push(l.proc);
    this.rank.push(levelRank(l.level));
    this.plain.push(plain);
    if (plain > this.maxPlain) this.maxPlain = plain;
    if (this.ids.length - this.head > this.max) this.head = this.ids.length - this.max;
    if (this.head >= Math.max(2048, this.max >> 2)) this.compact();
    return seq;
  }

  private compact() {
    const n = this.head;
    this.ids.splice(0, n);
    this.ts.splice(0, n);
    this.stream.splice(0, n);
    this.level.splice(0, n);
    this.text.splice(0, n);
    this.proc.splice(0, n);
    this.rank.splice(0, n);
    this.plain.splice(0, n);
    this.abs0 += n;
    this.head = 0;
  }

  at(i: number): BufferedLine {
    return this.atSeq(this.firstSeq + i);
  }

  plainAtSeq(seq: number): number {
    return this.plain[seq - this.abs0] ?? 0;
  }

  atSeq(seq: number): BufferedLine {
    const i = seq - this.abs0;
    return {
      id: this.ids[i] ?? 0,
      ts: this.ts[i] ?? 0,
      stream: this.stream[i] ?? 'stdout',
      level: this.level[i] ?? 'info',
      text: this.text[i] ?? '',
      proc: this.proc[i] ?? ''
    };
  }

  matches(seq: number, c: LogCriteria): boolean {
    const i = seq - this.abs0;
    if (c.procs && !c.procs.has(this.proc[i] as string)) return false;
    if (c.minRank > 0 && (this.rank[i] as number) < c.minRank) return false;
    if (c.streams) {
      const s = this.stream[i] as string;
      if ((s === 'stdout' || s === 'stderr') && !c.streams.has(s)) return false;
    }
    if (c.matcher && c.matcher.active && !c.matcher.test(this.text[i] as string)) return false;
    return true;
  }

  filter(pred: (l: BufferedLine, i: number) => boolean): Uint32Array {
    const idx: number[] = [];
    const n = this.length;
    for (let i = 0; i < n; i++) {
      if (pred(this.at(i), i)) idx.push(i);
    }
    this.view = Uint32Array.from(idx);
    return this.view;
  }

  clear() {
    this.abs0 = this.abs0 + this.ids.length;
    this.head = 0;
    this.ids = [];
    this.ts = [];
    this.stream = [];
    this.level = [];
    this.text = [];
    this.proc = [];
    this.rank = [];
    this.plain = [];
    this.maxPlain = 0;
    this.view = new Uint32Array(0);
  }
}

interface TailEntry {
  key: string;
  ids: string[];
  state: StreamState;
  handle: { close: () => void };
}

export interface LogSessionOptions {
  max?: number;
  schedule?: (fn: () => void) => void;
}

function toBuffered(raw: Record<string, unknown>): BufferedLine & { inst: string } {
  const l = toLogLine(raw);
  return { id: l.id, ts: l.ts || Date.now(), stream: l.stream, level: l.level, text: l.text, proc: l.proc, inst: l.instanceId };
}

export class LogSession {
  buffer: LogBuffer;
  conn = $state<ConnState>('idle');
  version = $state(0);
  paused = $state(false);
  gaps = $state(0);
  downSince = $state(0);

  private idx: number[] = [];
  private ihead = 0;
  private trimmed = 0;
  numBase = 0;
  private epochN = 0;
  private crit: LogCriteria = NO_CRITERIA;
  private queue: BufferedLine[] = [];
  private scheduled = false;
  private frozenSeq = 0;
  private tails = new Map<string, TailEntry>();
  private nextId = new Map<string, number>();
  private instOf = new Map<string, string>();
  private lastGroups = new Map<string, string[]>();
  private lastBacklog = 500;
  private schedule: (fn: () => void) => void;

  constructor(opts: LogSessionOptions = {}) {
    this.buffer = new LogBuffer(opts.max ?? 200000);
    this.schedule =
      opts.schedule ??
      ((fn) => {
        let done = false;
        const run = () => {
          if (done) return;
          done = true;
          fn();
        };
        if (typeof requestAnimationFrame === 'function') requestAnimationFrame(run);
        setTimeout(run, 250);
      });
  }

  get total(): number {
    void this.version;
    return this.buffer.length;
  }

  get viewCount(): number {
    void this.version;
    const end = this.paused ? this.lowerBound(this.frozenSeq) : this.idx.length;
    return Math.max(0, end - this.ihead);
  }

  get liveCount(): number {
    void this.version;
    return this.idx.length - this.ihead;
  }

  get ordBase(): number {
    void this.version;
    return this.trimmed;
  }

  get epoch(): number {
    void this.version;
    return this.epochN;
  }

  get maxPlain(): number {
    void this.version;
    return this.buffer.maxPlain;
  }

  private lowerBound(seq: number): number {
    let lo = this.ihead;
    let hi = this.idx.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if ((this.idx[mid] as number) < seq) lo = mid + 1;
      else hi = mid;
    }
    return lo;
  }

  rowSeq(k: number): number {
    return this.idx[this.ihead + k] as number;
  }

  line(k: number): BufferedLine {
    return this.buffer.atSeq(this.rowSeq(k));
  }

  plainAt(k: number): number {
    return this.buffer.plainAtSeq(this.rowSeq(k));
  }

  push(l: BufferedLine) {
    this.queue.push(l);
    if (this.scheduled) return;
    this.scheduled = true;
    this.schedule(() => this.flushNow());
  }

  flushNow() {
    this.scheduled = false;
    if (this.drain()) this.version++;
  }

  private drain(): boolean {
    if (this.queue.length === 0) return false;
    const q = this.queue;
    this.queue = [];
    const b = this.buffer;
    for (const l of q) {
      const seq = b.append(l);
      if (b.matches(seq, this.crit)) this.idx.push(seq);
    }
    this.trim();
    return true;
  }

  private trim() {
    const first = this.buffer.firstSeq;
    while (this.ihead < this.idx.length && (this.idx[this.ihead] as number) < first) {
      this.ihead++;
      this.trimmed++;
    }
    if (this.ihead > 65536 && this.ihead * 2 > this.idx.length) {
      this.idx.splice(0, this.ihead);
      this.ihead = 0;
    }
  }

  setCriteria(c: LogCriteria) {
    this.crit = c;
    this.rebuild();
  }

  private rebuild() {
    this.drain();
    const b = this.buffer;
    const out: number[] = [];
    for (let s = b.firstSeq; s < b.endSeq; s++) {
      if (b.matches(s, this.crit)) out.push(s);
    }
    this.idx = out;
    this.ihead = 0;
    this.trimmed = 0;
    this.epochN++;
    this.version++;
  }

  pause() {
    if (this.paused) return;
    this.drain();
    this.frozenSeq = this.buffer.endSeq;
    this.paused = true;
    this.version++;
  }

  resume() {
    if (!this.paused) return;
    this.paused = false;
    this.version++;
  }

  clear() {
    this.queue = [];
    this.buffer.clear();
    this.numBase = this.buffer.endSeq;
    this.idx = [];
    this.ihead = 0;
    this.trimmed = 0;
    this.epochN++;
    this.frozenSeq = this.buffer.endSeq;
    this.version++;
  }

  reset() {
    this.closeAll();
    this.nextId.clear();
    this.instOf.clear();
    this.gaps = 0;
    this.clear();
    this.recomputeConn();
  }

  private accept(l: BufferedLine & { inst: string }) {
    const key = `${l.proc}:${l.inst}`;
    const n = this.nextId.get(key) ?? 0;
    if (l.id < n) return;
    this.nextId.set(key, l.id + 1);
    this.instOf.set(l.proc, l.inst);
    this.push(l);
  }

  private onMessage(msg: StreamMessage) {
    if (msg.kind === 'batch' && Array.isArray(msg['lines'])) {
      const proc = String(msg['processId'] ?? '');
      for (const raw of msg['lines'] as Record<string, unknown>[]) {
        const l = toBuffered(raw);
        if (!l.proc) l.proc = proc;
        this.accept(l);
      }
    } else if (msg.kind === 'line') {
      this.accept(toBuffered(msg as Record<string, unknown>));
    } else if (msg.kind === 'instance') {
      const proc = String(msg['processId'] ?? '');
      const inst = String(msg['instanceId'] ?? '');
      this.push({ id: -1, ts: Date.now(), stream: 'system', level: 'info', text: `process restarted${inst ? ` (${inst})` : ''}`, proc });
    }
  }

  private resumeFor(ids: string[]): Record<string, unknown> {
    const resume: Record<string, number> = {};
    for (const id of ids) {
      const inst = this.instOf.get(id);
      if (inst === undefined) continue;
      const n = this.nextId.get(`${id}:${inst}`);
      if (n !== undefined) resume[id] = n;
    }
    return Object.keys(resume).length > 0 ? { resume } : {};
  }

  setTargets(groups: Map<string, string[]>, backlog = 500) {
    this.lastGroups = groups;
    this.lastBacklog = backlog;
    for (const [ws, entry] of this.tails) {
      if (!groups.has(ws)) {
        entry.handle.close();
        this.tails.delete(ws);
      }
    }
    for (const [ws, ids] of groups) {
      const key = [...ids].sort().join(',');
      const cur = this.tails.get(ws);
      if (cur && cur.key === key) continue;
      cur?.handle.close();
      this.tails.delete(ws);
      this.open(ws, ids, key, backlog);
    }
    this.recomputeConn();
  }

  private open(ws: string, ids: string[], key: string, backlog: number) {
    const entry: TailEntry = { key, ids, state: 'connecting', handle: { close: () => {} } };
    this.tails.set(ws, entry);
    entry.handle = openStream(
      'LogService',
      'TailLogs',
      { workspaceId: ws, processIds: ids, backlog },
      {
        onMessage: (m) => this.onMessage(m),
        onState: (s) => {
          if (this.tails.get(ws) !== entry) return;
          entry.state = s;
          this.recomputeConn();
        },
        onGap: () => {
          this.gaps++;
        },
        resume: () => this.resumeFor(ids)
      }
    );
  }

  private recomputeConn() {
    const states = [...this.tails.values()].map((t) => t.state);
    let next: ConnState;
    if (states.length === 0) next = 'idle';
    else if (states.includes('reconnecting')) next = 'reconnecting';
    else if (states.includes('connecting')) next = 'connecting';
    else if (states.every((s) => s === 'open')) next = 'live';
    else next = 'closed';
    if (next === 'reconnecting' && this.conn !== 'reconnecting') this.downSince = Date.now();
    if (next !== 'reconnecting') this.downSince = 0;
    this.conn = next;
  }

  retry() {
    const groups = this.lastGroups;
    this.closeAll();
    this.setTargets(groups, this.lastBacklog);
  }

  private closeAll() {
    for (const entry of this.tails.values()) entry.handle.close();
    this.tails.clear();
  }

  dispose() {
    this.closeAll();
    this.queue = [];
    this.conn = 'idle';
  }

  viewLines(): BufferedLine[] {
    this.drain();
    const out: BufferedLine[] = [];
    const end = this.paused ? this.lowerBound(this.frozenSeq) : this.idx.length;
    for (let p = this.ihead; p < end; p++) out.push(this.buffer.atSeq(this.idx[p] as number));
    return out;
  }
}
