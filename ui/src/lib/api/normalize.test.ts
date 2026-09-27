import { describe, expect, it } from 'vitest';
import { toAuditEntry, toEvent, toProcess, toResource } from './normalize';

describe('toProcess', () => {
  it('normalises snake_case list rows', () => {
    const p = toProcess({ process_id: 'proc_1', status: 'running', command: 'npm run', pid: 12, ports: null } as unknown as Record<string, unknown>);
    expect(p.id).toBe('proc_1');
    expect(p.ports).toEqual([]);
    expect(p.status).toBe('running');
  });
  it('normalises camelCase watch rows', () => {
    const p = toProcess({ id: 'proc_2', processId: 'proc_2', status: 'failed', command: 'api', ports: [3000] } as unknown as Record<string, unknown>);
    expect(p.id).toBe('proc_2');
    expect(p.ports).toEqual([3000]);
  });
});

describe('toEvent', () => {
  it('parses payload JSON', () => {
    const e = toEvent({ id: 3, ts: 1000, type: 'process.exited', payload: '{"exitCode":1}' } as unknown as Record<string, unknown>);
    expect(e.payload).toEqual({ exitCode: 1 });
    expect(e.cursor).toBe('3');
  });
});

describe('toAuditEntry', () => {
  it('maps duration', () => {
    const a = toAuditEntry({ id: 1, ts: 100, action: 'ProcessService.Start', result: 'ok', durationMs: 5 } as unknown as Record<string, unknown>);
    expect(a.duration).toBe(5);
  });
});

describe('toResource', () => {
  it('keeps cpuNanos and percent', () => {
    const r = toResource({ processId: 'p', cpuNanos: 10, cpuPercent: 2.5 } as unknown as Record<string, unknown>);
    expect(r.cpuNanos).toBe(10);
    expect(r.cpuPercent).toBe(2.5);
  });
});
