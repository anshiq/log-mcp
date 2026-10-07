import { call, openStream, type StreamMessage, type StreamState } from './transport';
import { toAuditEntry, toEvent, toHarness, toProcess, toResource, toRevision, toSession, toSkill } from './normalize';
import type { AuditEntry, DaemonEvent, Harness, PlanResult, Process, ResourceSample, Revision, Session, Skill } from './types';

export type { StreamMessage, StreamState };

type CB = (s: StreamState) => void;

export const SystemService = {
  version: () => call<Record<string, never>, { daemonVersion: string; apiVersion: string; minClientVersion: string }>('SystemService', 'GetVersion', {}),
  health: () => call<Record<string, never>, { healthy: boolean; status: string; uptimeMs: number; uptimeSeconds: number; version: string; socketPath: string; dataDir: string; tcpAddr: string }>('SystemService', 'Health', {}),
  stats: () => call<Record<string, never>, { projects: number; processes: number; sessions: number; processesRunning: number; processesFailed: number; logDiskBytes: number; indexLagSeconds: number; uptimeSeconds: number }>('SystemService', 'GetStats', {}),
  getSettings: () => call<Record<string, never>, { settings: Record<string, unknown>; schema: Record<string, { type: string; enum?: string[]; description: string }> }>('SystemService', 'GetSettings', {}),
  updateSettings: (req: Record<string, unknown>) => call<Record<string, unknown>, { settings: Record<string, unknown> }>('SystemService', 'UpdateSettings', req),
  shutdown: (keepProcesses = true) => call<{ keepProcesses: boolean }, { draining: boolean }>('SystemService', 'Shutdown', { keepProcesses })
};

export const ProjectService = {
  resolve: (path: string) => call<{ path: string }, { projectId: string; workspaceId: string; projectName: string }>('ProjectService', 'Resolve', { path }),
  list: async () => {
    const r = await call<Record<string, never>, { projects: Record<string, unknown>[] }>('ProjectService', 'ListProjects', {});
    return r.projects ?? [];
  },
  get: (projectId: string) => call<{ projectId: string }, { project: Record<string, unknown> }>('ProjectService', 'GetProject', { projectId }),
  update: (projectId: string, name: string) => call<{ projectId: string; name: string }, unknown>('ProjectService', 'UpdateProject', { projectId, name }),
  workspaces: async (projectId: string) => {
    const r = await call<{ projectId: string }, { workspaces: Record<string, unknown>[] }>('ProjectService', 'ListWorkspaces', { projectId });
    return r.workspaces ?? [];
  },
  linkWorkspace: (projectId: string, path: string) => call<{ projectId: string; path: string }, unknown>('ProjectService', 'LinkWorkspace', { projectId, path }),
  forget: (projectId: string) => call<{ projectId: string }, unknown>('ProjectService', 'ForgetProject', { projectId }),
  gc: () => call<Record<string, never>, { removedWorkspaces: string[] }>('ProjectService', 'GC', {}),
};

export const ProcessService = {
  list: async (workspaceId: string, allWorkspaces = false): Promise<Process[]> => {
    const r = await call<{ workspaceId: string; allWorkspaces: boolean }, { processes: Record<string, unknown>[] }>('ProcessService', 'List', { workspaceId, allWorkspaces });
    return (r.processes ?? []).map(toProcess);
  },
  get: async (processId: string): Promise<Process> => {
    const r = await call<{ processId: string }, Record<string, unknown>>('ProcessService', 'Get', { processId });
    return toProcess(r);
  },
  start: (req: Record<string, unknown>) => call<Record<string, unknown>, { processId: string; instanceId: string }>('ProcessService', 'Start', req),
  stop: (processId: string, signal?: string) => call<Record<string, unknown>, unknown>('ProcessService', 'Stop', { processId, signal }),
  restart: (processId: string) => call<{ processId: string }, { newInstanceId: string }>('ProcessService', 'Restart', { processId }),
  signal: (processId: string, signal: string) => call<{ processId: string; signal: string }, unknown>('ProcessService', 'Signal', { processId, signal }),
  sendStdin: (processId: string, data: string) => call<{ processId: string; data: string }, unknown>('ProcessService', 'SendStdin', { processId, data }),
  remove: (processId: string, force = false) => call<{ processId: string; force: boolean }, unknown>('ProcessService', 'Remove', { processId, force }),
  setRestartPolicy: (processId: string, policy: string) => call<{ processId: string; policy: string }, unknown>('ProcessService', 'SetRestartPolicy', { processId, policy }),
  getEnv: (processId: string, reveal = false, live = false) => call<{ processId: string; reveal: boolean; live: boolean }, { env: string[]; source?: Record<string, string>; redacted?: number }>('ProcessService', 'GetEnv', { processId, reveal, live }),
  openShell: (req: Record<string, unknown>) => call<Record<string, unknown>, { processId: string; instanceId: string }>('ProcessService', 'OpenShell', req),
  getResourceUsage: async (processId: string): Promise<ResourceSample> => {
    const r = await call<{ processId: string }, Record<string, unknown>>('ProcessService', 'GetResourceUsage', { processId });
    return toResource(r);
  },
  waitForExit: (processId: string, timeoutMs?: number) => call<{ processId: string; timeoutMs?: number }, { exited: boolean; exitCode?: number }>('ProcessService', 'WaitForExit', { processId, timeoutMs }),
  startStack: (workspaceId: string, apps: string[], timeoutMs?: number) => call<{ workspaceId: string; apps: string[]; timeoutMs?: number }, { started: { app: string; processId: string }[]; order: string[]; failed?: string; error?: string }>('ProcessService', 'StartStack', { workspaceId, apps, timeoutMs }),
  watch: (workspaceId: string, allWorkspaces: boolean, onMessage: (m: StreamMessage) => void, onState?: CB, extra?: { onGap?: (n: number) => void; onSnapshot?: (m: StreamMessage) => void }) =>
    openStream('ProcessService', 'WatchProcesses', { workspaceId, allWorkspaces }, { onMessage, onState, onGap: extra?.onGap, onSnapshot: extra?.onSnapshot }),
  watchResourceUsage: (processId: string, onMessage: (m: StreamMessage) => void, onState?: CB) =>
    openStream('ProcessService', 'WatchResourceUsage', { processId }, { onMessage, onState }),
  attach: (processId: string, backlog: number, onMessage: (m: StreamMessage) => void, onState?: CB) =>
    openStream('ProcessService', 'Attach', { processId, backlog }, { onMessage, onState })
};

export const LogService = {
  get: (processId: string, lines = 500) => call<{ processId: string; lines: number }, { entries: { line: string; stream: string; timestamp?: string; id?: number }[] }>('LogService', 'GetLogs', { processId, lines }),
  search: (req: Record<string, unknown>) => call<Record<string, unknown>, { matches: Record<string, unknown>[]; truncatedScan: boolean }>('LogService', 'SearchLogs', req),
  clear: (processId: string, stream = 'all') => call<{ processId: string; stream: string }, unknown>('LogService', 'ClearLogs', { processId, stream }),
  stats: (processId: string) => call<{ processId: string }, { stdoutLines: number; stderrLines: number; totalLines: number; diskBytes: number; segmentCount: number; firstTimestamp: string; lastTimestamp: string }>('LogService', 'GetLogStats', { processId }),
  waitForLog: (processId: string, contains?: string, pattern?: string, timeoutMs?: number) => call<Record<string, unknown>, { matched: boolean; entry?: { line: string } }>('LogService', 'WaitForLog', { processId, contains, pattern, timeoutMs }),
  exportLogs: (processId: string, format = 'ndjson', onMessage: (m: StreamMessage) => void, onState?: CB) =>
    openStream('LogService', 'ExportLogs', { processId, format }, { onMessage, onState }),
  tail: (workspaceId: string, processIds: string[], backlog: number, onMessage: (m: StreamMessage) => void, onState?: CB, resume?: () => Record<string, unknown>) =>
    openStream('LogService', 'TailLogs', { workspaceId, processIds, backlog }, { onMessage, onState, resume })
};

export const ConfigService = {
  get: (workspaceId: string) => call<{ workspaceId: string }, { projectId: string; workspaceId: string; revision: number; configRevision: number; configSource: string; pendingProposal: { id: string; yaml: string; summary: { app: string; action: string; reason: string }[]; createdAt: number } | null; raw: Record<string, string>; layers: { name: string; path: string; exists: boolean; writable: boolean }[]; apps: Record<string, unknown> }>('ConfigService', 'GetConfig', { workspaceId }),
  schema: () => call<Record<string, never>, { schema: unknown }>('ConfigService', 'GetSchema', {}),
  validate: (yaml: string) => call<{ yaml: string }, { valid: boolean; errors: { line: number; column: number; path: string; message: string }[]; warnings?: string[] }>('ConfigService', 'Validate', { yaml }),
  plan: (workspaceId: string, yaml: string, baseRevision?: number) => call<{ workspaceId: string; yaml: string; baseRevision?: number }, PlanResult>('ConfigService', 'Plan', { workspaceId, yaml, baseRevision }),
  apply: (req: { projectId?: string; workspaceId?: string; layer?: string; yaml: string; baseRevision?: number; message?: string; restartAffected?: boolean }) =>
    call<Record<string, unknown>, { applied: boolean; revision?: number; restarted?: string[]; errors?: unknown[] }>('ConfigService', 'Apply', req),
  revisions: async (projectId: string, limit = 20): Promise<Revision[]> => {
    const r = await call<{ projectId: string; limit: number }, { revisions: Record<string, unknown>[] }>('ConfigService', 'ListRevisions', { projectId, limit });
    return (r.revisions ?? []).map(toRevision);
  },
  revision: (projectId: string, id: number) => call<{ projectId: string; revision: number }, { revision: Record<string, unknown>; content: string; previousContent: string }>('ConfigService', 'GetRevision', { projectId, revision: id }),
  rollback: (projectId: string, revision: number) => call<{ projectId: string; revision: number }, { applied: boolean; revision: number }>('ConfigService', 'Rollback', { projectId, revision }),
  resolveProposal: (projectId: string, proposalId: string, action: 'approve' | 'dismiss') =>
    call<{ projectId: string; proposalId: string; action: string }, { applied?: boolean; dismissed?: boolean; revision?: number }>('ConfigService', 'ResolveProposal', { projectId, proposalId, action }),
  watch: (projectId: string, onMessage: (m: StreamMessage) => void, onState?: CB) =>
    openStream('ConfigService', 'WatchConfig', { projectId }, { onMessage, onState })
};

export const EventService = {
  list: async (workspaceId: string, limit = 100, extra?: { types?: string[]; before?: string; since?: string; processId?: string }): Promise<{ events: DaemonEvent[]; nextBefore: string }> => {
    const r = await call<Record<string, unknown>, { events: Record<string, unknown>[]; nextBefore: string }>('EventService', 'ListEvents', { workspaceId, limit, ...extra });
    return { events: (r.events ?? []).map(toEvent), nextBefore: r.nextBefore ?? '' };
  },
  latestId: async (): Promise<number> => {
    const has = async (since: number): Promise<boolean> => (await EventService.list('', 1, { since: String(since) })).events.length > 0;
    if (!(await has(0))) return 0;
    let lo = 0;
    let hi = 1;
    while (await has(hi)) {
      lo = hi;
      hi *= 2;
    }
    while (hi - lo > 1) {
      const mid = Math.floor((lo + hi) / 2);
      if (await has(mid)) lo = mid;
      else hi = mid;
    }
    return hi;
  },
  window: async (workspaceId: string, upTo: number, size = 400): Promise<{ events: DaemonEvent[]; floor: number }> => {
    const floor = Math.max(0, upTo - size);
    const r = await EventService.list(workspaceId, size, { since: String(floor) });
    return { events: r.events.filter((e) => e.id <= upTo), floor };
  },
  watch: (workspaceId: string, onMessage: (m: StreamMessage) => void, onState?: CB, extra?: { types?: string[]; processIds?: string[] }) =>
    openStream('EventService', 'WatchEvents', { workspaceId, ...extra }, { onMessage, onState }),
  subscribe: (sessionId: string, types: string[], processId?: string) => call<Record<string, unknown>, { subscriptionId: string }>('EventService', 'Subscribe', { sessionId, types, processId }),
  drain: (subscriptionId: string, limit?: number) => call<Record<string, unknown>, { events: unknown[]; dropped: number }>('EventService', 'Drain', { subscriptionId, limit }),
  unsubscribe: (subscriptionId: string) => call<{ subscriptionId: string }, unknown>('EventService', 'Unsubscribe', { subscriptionId })
};

export const SessionService = {
  list: async (): Promise<Session[]> => {
    const r = await call<Record<string, never>, { sessions: Record<string, unknown>[] }>('SessionService', 'ListSessions', {});
    return (r.sessions ?? []).map(toSession);
  },
  get: (sessionId: string) => call<{ sessionId: string }, { session: Record<string, unknown> }>('SessionService', 'GetSession', { sessionId }),
  close: (sessionId: string) => call<{ sessionId: string }, unknown>('SessionService', 'Close', { sessionId }),
  register: (req: Record<string, unknown>) => call<Record<string, unknown>, { sessionId: string }>('SessionService', 'Register', req),
  ping: (sessionId: string) => call<{ sessionId: string }, { ok: boolean }>('SessionService', 'Ping', { sessionId }),
  heartbeat: (sessionId: string, onMessage: (m: StreamMessage) => void, onState?: CB) =>
    openStream('SessionService', 'Heartbeat', { sessionId }, { onMessage, onState })
};

export const IntegrationService = {
  listHarnesses: async (): Promise<Harness[]> => {
    const r = await call<Record<string, never>, { harnesses: Record<string, unknown>[] }>('IntegrationService', 'ListHarnesses', {});
    return (r.harnesses ?? []).map(toHarness);
  },
  previewInstall: (harness: string, scope: string) => call<{ harness: string; scope: string }, { diff: string }>('IntegrationService', 'PreviewInstall', { harness, scope }),
  installMCP: (harness: string, scope: string) => call<{ harness: string; scope: string }, { path: string; diff: string; changed?: boolean }>('IntegrationService', 'InstallMCP', { harness, scope }),
  removeMCP: (harness: string, scope: string) => call<{ harness: string; scope: string }, { path: string; changed?: boolean }>('IntegrationService', 'RemoveMCP', { harness, scope }),
  listSkills: async (): Promise<Skill[]> => {
    const r = await call<Record<string, never>, { skills: Record<string, unknown>[] }>('IntegrationService', 'ListSkills', {});
    return (r.skills ?? []).map(toSkill);
  },
  installSkills: (skillNames: string[], scope: string) => call<{ skillNames: string[]; scope: string }, unknown>('IntegrationService', 'InstallSkills', { skillNames, scope }),
  removeSkills: (skillNames: string[]) => call<{ skillNames: string[] }, unknown>('IntegrationService', 'RemoveSkills', { skillNames }),
  installSkillsFor: (harness: string, scope: string) => call<{ harness: string; scope: string }, { installed?: unknown; path?: string }>('IntegrationService', 'InstallSkills', { harness, scope }),
  removeSkillsFor: (harness: string, scope: string) => call<{ harness: string; scope: string }, { removed?: boolean }>('IntegrationService', 'RemoveSkills', { harness, scope }),
  checkUpdates: () => call<Record<string, never>, { updates: Record<string, unknown>[] }>('IntegrationService', 'CheckUpdates', {})
};

export const AuditService = {
  list: async (workspaceId: string, limit = 100): Promise<AuditEntry[]> => {
    const r = await call<{ workspaceId: string; limit: number }, { entries: Record<string, unknown>[] }>('AuditService', 'ListAudit', { workspaceId, limit });
    return (r.entries ?? []).map(toAuditEntry);
  }
};
