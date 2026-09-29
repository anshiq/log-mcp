import { describe, expect, it } from 'vitest';
import type { Process } from '../api/types';
import { commandLine, matchProcesses, primaryProcess, toConfigApp, toConfigApps } from './apps';

function proc(over: Partial<Process>): Process {
  return {
    id: 'p1',
    instanceId: 'i1',
    app: '',
    workspaceId: 'ws',
    projectId: 'pr',
    command: 'sh',
    args: [],
    workdir: '',
    profile: '',
    status: 'running',
    pid: 1,
    restarts: 0,
    health: '',
    startedAt: null,
    exitedAt: null,
    exitCode: null,
    exitSignal: '',
    restartPolicy: '',
    stdoutLines: 0,
    stderrLines: 0,
    ports: [],
    stale: false,
    loaded: true,
    lifetime: '',
    sessionId: '',
    ...over
  };
}

describe('toConfigApp', () => {
  it('reads Go-cased daemon output', () => {
    const app = toConfigApp('api', {
      Type: 'shell',
      Command: ['node', 'server.js'],
      DependsOn: ['db'],
      Autostart: true,
      Ports: { http: { Default: 8080 } },
      Restart: { Policy: 'always' },
      HealthCheck: { HTTP: 'http://x/health' },
      Readiness: ['ready']
    });
    expect(app.command).toEqual(['node', 'server.js']);
    expect(app.ports).toEqual([{ name: 'http', port: 8080 }]);
    expect(app.restartPolicy).toBe('always');
    expect(app.healthHttp).toBe('http://x/health');
    expect(app.dependsOn).toEqual(['db']);
  });

  it('reads snake-cased yaml output and tolerates nulls', () => {
    const app = toConfigApp('w', { command: ['a'], depends_on: null, ports: null });
    expect(app.command).toEqual(['a']);
    expect(app.dependsOn).toEqual([]);
    expect(app.ports).toEqual([]);
  });

  it('sorts apps by name', () => {
    expect(toConfigApps({ b: {}, a: {} }).map((a) => a.name)).toEqual(['a', 'b']);
  });
});

describe('commandLine', () => {
  it('quotes arguments with spaces', () => {
    expect(commandLine(['sh', '-c', 'echo "hi there"'])).toBe(`sh -c 'echo "hi there"'`);
  });
});

describe('matchProcesses', () => {
  const app = toConfigApp('api', { Command: ['sh', '-c', 'loop'] });
  it('matches by app name first', () => {
    const matches = matchProcesses(app, [proc({ id: 'a', app: 'api' }), proc({ id: 'b', app: 'other' })]);
    expect(matches.map((p) => p.id)).toEqual(['a']);
  });
  it('falls back to argv equality for unnamed processes', () => {
    const matches = matchProcesses(app, [proc({ id: 'a', command: 'sh', args: ['-c', 'loop'] }), proc({ id: 'b', command: 'sh', args: ['-c', 'x'] })]);
    expect(matches.map((p) => p.id)).toEqual(['a']);
  });
  it('prefers a live process as primary', () => {
    const primary = primaryProcess([proc({ id: 'x', status: 'exited' }), proc({ id: 'y', status: 'running' })]);
    expect(primary?.id).toBe('y');
  });
});
