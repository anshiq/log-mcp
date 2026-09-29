import { describe, expect, it } from 'vitest';
import type { Process } from '../lib/api/types';
import {
  agoShort,
  commandLine,
  compactDuration,
  displayName,
  exitInfo,
  matchesQuery,
  matchesStatus,
  sortRows,
  splitEnvLine,
  uptimeText
} from './processView';

function proc(over: Partial<Process> = {}): Process {
  return {
    id: 'proc_1',
    instanceId: 'run_1',
    app: '',
    workspaceId: 'ws_1',
    projectId: 'p_1',
    command: 'node',
    args: ['server.js', '--port', '3000'],
    workdir: '/tmp',
    profile: 'node',
    status: 'running',
    pid: 42,
    restarts: 0,
    health: 'unknown',
    startedAt: 1000,
    exitedAt: null,
    exitCode: null,
    exitSignal: '',
    restartPolicy: '',
    stdoutLines: 0,
    stderrLines: 0,
    ports: [3000],
    stale: false,
    loaded: true,
    lifetime: 'persistent',
    sessionId: '',
    ...over
  };
}

describe('processView', () => {
  it('builds command lines and names', () => {
    expect(commandLine(proc())).toBe('node server.js --port 3000');
    expect(displayName(proc())).toBe('node');
    expect(displayName(proc({ app: 'api' }))).toBe('api');
    expect(displayName(proc({ command: 'sh', args: ['-c', 'while true; do x; done'] }))).toBe('sh · x');
    expect(displayName(proc({ command: 'sh', args: ['-c', 'i=0; while true; do echo "job $i"; done'] }))).toBe('sh · echo');
    expect(displayName(proc({ command: '/usr/bin/python3', args: [] }))).toBe('python3');
  });

  it('formats durations', () => {
    expect(compactDuration(4000)).toBe('4s');
    expect(compactDuration(125000)).toBe('2m 05s');
    expect(compactDuration(3 * 3600000 + 60000)).toBe('3h 01m');
    expect(compactDuration(2 * 86400000 + 5 * 3600000)).toBe('2d 5h');
    expect(agoShort(0, 3 * 60000)).toBe('3m ago');
  });

  it('describes uptime and exit state', () => {
    expect(uptimeText(proc(), 61000)).toBe('1m 00s');
    expect(uptimeText(proc({ status: 'exited', exitedAt: 1000 }), 181000)).toBe('exited 3m ago');
    expect(exitInfo(proc({ status: 'exited', exitCode: 3 }))).toBe('code 3');
    expect(exitInfo(proc({ status: 'failed', exitSignal: 'SIGKILL' }))).toBe('SIGKILL');
    expect(exitInfo(proc())).toBe('');
  });

  it('filters by status and query', () => {
    expect(matchesStatus(proc(), 'running')).toBe(true);
    expect(matchesStatus(proc({ status: 'crashed' }), 'failed')).toBe(true);
    expect(matchesStatus(proc({ status: 'stopped' }), 'exited')).toBe(true);
    expect(matchesQuery(proc(), 'server 3000', 'ws')).toBe(true);
    expect(matchesQuery(proc(), ':3000', 'ws')).toBe(true);
    expect(matchesQuery(proc(), 'nope', 'ws')).toBe(false);
  });

  it('sorts rows', () => {
    const a = proc({ id: 'a', pid: 5, command: 'zeta' });
    const b = proc({ id: 'b', pid: 9, command: 'alpha' });
    expect(sortRows([a, b], 'name', 'asc').map((p) => p.id)).toEqual(['b', 'a']);
    expect(sortRows([a, b], 'pid', 'desc').map((p) => p.id)).toEqual(['b', 'a']);
    expect(sortRows([a, b], null, 'asc').map((p) => p.id)).toEqual(['a', 'b']);
    const old = proc({ id: 'old', startedAt: 100 });
    const young = proc({ id: 'young', startedAt: 900 });
    const dead = proc({ id: 'dead', status: 'exited' });
    expect(sortRows([young, dead, old], 'uptime', 'desc').map((p) => p.id)).toEqual(['old', 'young', 'dead']);
    expect(sortRows([old, dead, young], 'uptime', 'asc').map((p) => p.id)).toEqual(['young', 'old', 'dead']);
  });

  it('splits env lines', () => {
    expect(splitEnvLine('A=b=c')).toEqual({ key: 'A', value: 'b=c' });
    expect(splitEnvLine('NOEQ')).toEqual({ key: 'NOEQ', value: '' });
  });
});
