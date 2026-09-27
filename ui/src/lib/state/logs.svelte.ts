export interface BufferedLine {
  id: number;
  ts: number;
  stream: string;
  level: string;
  text: string;
  proc: string;
}

export class LogBuffer {
  ids: number[] = [];
  ts: number[] = [];
  stream: string[] = [];
  level: string[] = [];
  text: string[] = [];
  proc: string[] = [];
  max: number;
  view: Uint32Array = new Uint32Array(0);

  constructor(max = 200000) {
    this.max = max;
  }

  get length(): number {
    return this.ids.length;
  }

  append(l: BufferedLine) {
    this.ids.push(l.id);
    this.ts.push(l.ts);
    this.stream.push(l.stream);
    this.level.push(l.level);
    this.text.push(l.text);
    this.proc.push(l.proc);
    if (this.ids.length > this.max) {
      const drop = this.ids.length - this.max;
      this.ids.splice(0, drop);
      this.ts.splice(0, drop);
      this.stream.splice(0, drop);
      this.level.splice(0, drop);
      this.text.splice(0, drop);
      this.proc.splice(0, drop);
    }
  }

  at(i: number): BufferedLine {
    return {
      id: this.ids[i] ?? 0,
      ts: this.ts[i] ?? 0,
      stream: this.stream[i] ?? 'stdout',
      level: this.level[i] ?? 'info',
      text: this.text[i] ?? '',
      proc: this.proc[i] ?? ''
    };
  }

  filter(pred: (l: BufferedLine, i: number) => boolean): Uint32Array {
    const idx: number[] = [];
    for (let i = 0; i < this.ids.length; i++) {
      if (pred(this.at(i), i)) idx.push(i);
    }
    this.view = Uint32Array.from(idx);
    return this.view;
  }

  clear() {
    this.ids = [];
    this.ts = [];
    this.stream = [];
    this.level = [];
    this.text = [];
    this.proc = [];
    this.view = new Uint32Array(0);
  }
}
