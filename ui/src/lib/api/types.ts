export type ProcessStatus = 'running' | 'starting' | 'ready' | 'exited' | 'stopped' | 'failed' | 'crashed' | 'unknown';

export interface Process {
  id: string;
  instanceId: string;
  app: string;
  workspaceId: string;
  projectId: string;
  command: string;
  args: string[];
  workdir: string;
  profile: string;
  status: ProcessStatus;
  pid: number;
  restarts: number;
  health: string;
  startedAt: number | null;
  exitedAt: number | null;
  exitCode: number | null;
  exitSignal: string;
  restartPolicy: string;
  stdoutLines: number;
  stderrLines: number;
  ports: number[];
  stale: boolean;
  loaded: boolean;
  lifetime: string;
  sessionId: string;
}

export interface LogLine {
  id: number;
  ts: number;
  stream: string;
  level: string;
  text: string;
  proc: string;
  instanceId: string;
  cursor: string;
}

export interface LogFrame {
  kind: string;
  processId?: string;
  lines?: LogLine[];
  line?: LogLine;
  data?: string;
  cursor?: string;
}

export interface DaemonEvent {
  id: number;
  ts: number;
  type: string;
  processId: string;
  instanceId: string;
  workspaceId: string;
  projectId: string;
  sessionId: string;
  payload: Record<string, unknown>;
  cursor: string;
}

export interface Project {
  id: string;
  name: string;
  configMode: string;
  workspaceCount: number;
  lastUsed: number;
}

export interface Workspace {
  id: string;
  projectId: string;
  path: string;
  confirmed: boolean;
  lastSeen: number;
  missing: boolean;
}

export interface ConfigBundle {
  projectId: string;
  workspaceId: string;
  revision: number;
  raw: Record<string, string>;
  layers: { name: string; path: string; exists: boolean; writable: boolean }[];
  apps: Record<string, unknown>;
}

export interface PlanResult {
  changes: { app: string; kind: string; fields: string[]; affectedProcIds?: string[] }[];
  stale: boolean;
  latestRevision: number;
  diff: string;
}

export interface Revision {
  id: number;
  layer: string;
  time: number;
  source: string;
  session: string;
  message: string;
  valid: boolean;
  sha: string;
}

export interface Harness {
  id: string;
  name: string;
  displayName: string;
  detected: boolean;
  version: string;
  mcpConfigured: boolean;
  mcpGlobal: boolean;
  skillsGlobal: string[];
}

export interface Skill {
  name: string;
  version: string;
  description: string;
  outdated: boolean;
}

export interface Session {
  id: string;
  kind: string;
  harness: string;
  harnessVersion: string;
  clientPid: number;
  workspaceId: string;
  started: number;
  lastSeen: number;
  closedAt: number;
  closed: boolean;
}

export interface AuditEntry {
  id: number;
  time: number;
  action: string;
  result: string;
  duration: number;
  session: string;
  harness: string;
  workspaceId: string;
  projectId: string;
}

export interface Settings {
  values: Record<string, unknown>;
  schema: Record<string, { type: string; enum?: string[]; description: string }>;
}

export interface ResourceSample {
  processId: string;
  pid: number;
  memoryBytes: number;
  cpuNanos: number;
  cpuPercent: number;
  rssBytes: number;
  threads: number;
  openFds: number;
  children: number;
  ports: number[];
  at: number;
}
