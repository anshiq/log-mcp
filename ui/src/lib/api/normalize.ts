import type { AuditEntry, DaemonEvent, Harness, LogLine, Process, ResourceSample, Revision, Session, Skill } from './types';

function num(v: unknown): number | null {
  if (typeof v === 'number') return v;
  if (typeof v === 'string' && v !== '') {
    const n = Date.parse(v);
    if (!Number.isNaN(n)) return n;
    const i = Number(v);
    if (!Number.isNaN(i)) return i;
  }
  return null;
}

function str(v: unknown, fallback = ''): string {
  return typeof v === 'string' ? v : fallback;
}

function arr(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
}

function numArr(v: unknown): number[] {
  if (!Array.isArray(v)) return [];
  return v.map((x) => (typeof x === 'number' ? x : Number(x))).filter((x) => Number.isFinite(x));
}

export function toProcess(raw: Record<string, unknown>): Process {
  const id = str(raw['id'] || raw['processId'] || raw['process_id']);
  const started = num(raw['startedAt'] ?? raw['started_at']) ?? num(raw['startedat']);
  const exited = num(raw['exitedAt'] ?? raw['exited_at']);
  const exitRaw = raw['exitCode'] ?? raw['exit_code'];
  return {
    id,
    instanceId: str(raw['instanceId'] ?? raw['instance_id']),
    app: str(raw['app']),
    workspaceId: str(raw['workspaceId'] ?? raw['workspace_id']),
    projectId: str(raw['projectId'] ?? raw['project_id']),
    command: str(raw['command']),
    args: arr(raw['args']),
    workdir: str(raw['workdir']),
    profile: str(raw['profile']),
    status: (str(raw['status'], 'unknown') as Process['status']),
    pid: typeof raw['pid'] === 'number' ? raw['pid'] : 0,
    restarts: typeof raw['restarts'] === 'number' ? raw['restarts'] : 0,
    health: str(raw['health'], 'unknown'),
    startedAt: started,
    exitedAt: exited,
    exitCode: typeof exitRaw === 'number' ? exitRaw : null,
    exitSignal: str(raw['exitSignal'] ?? raw['exit_signal']),
    restartPolicy: str(raw['restartPolicy'] ?? raw['restart_policy']),
    stdoutLines: typeof raw['stdoutLines'] === 'number' ? raw['stdoutLines'] : (typeof raw['stdout_lines'] === 'number' ? (raw['stdout_lines'] as number) : 0),
    stderrLines: typeof raw['stderrLines'] === 'number' ? raw['stderrLines'] : (typeof raw['stderr_lines'] === 'number' ? (raw['stderr_lines'] as number) : 0),
    ports: numArr(raw['ports']),
    stale: raw['stale'] === true,
    loaded: raw['loaded'] !== false,
    lifetime: str(raw['lifetime'], 'persistent'),
    sessionId: str(raw['sessionId'] ?? raw['session_id'] ?? raw['startedBy'])
  };
}

export function toLogLine(raw: Record<string, unknown>, fallbackProc = ''): LogLine {
  const tsRaw = raw['timestamp'] ?? raw['ts'];
  let ts = 0;
  if (typeof tsRaw === 'number') ts = tsRaw > 1e12 ? tsRaw : tsRaw / 1e6;
  else if (typeof tsRaw === 'string') ts = Date.parse(tsRaw) || 0;
  return {
    id: typeof raw['id'] === 'number' ? raw['id'] : 0,
    ts,
    stream: str(raw['stream'], 'stdout'),
    level: str(raw['level'], 'info'),
    text: str(raw['line'] ?? raw['data'] ?? raw['text']),
    proc: str(raw['processId'] ?? raw['process_id'], fallbackProc),
    instanceId: str(raw['instanceId'] ?? raw['instance_id']),
    cursor: str(raw['cursor'])
  };
}

export function toEvent(raw: Record<string, unknown>): DaemonEvent {
  let payload: Record<string, unknown> = {};
  const p = raw['payload'];
  if (p && typeof p === 'object') payload = p as Record<string, unknown>;
  else if (typeof p === 'string' && p !== '') {
    try {
      payload = JSON.parse(p) as Record<string, unknown>;
    } catch {
      payload = { message: p };
    }
  } else if (typeof raw['payload_json'] === 'string' && (raw['payload_json'] as string) !== '') {
    try {
      payload = JSON.parse(raw['payload_json'] as string) as Record<string, unknown>;
    } catch {
      payload = { message: raw['payload_json'] };
    }
  }
  const tsRaw = raw['ts'] ?? raw['timestamp'];
  let ts = 0;
  if (typeof tsRaw === 'number') ts = tsRaw > 1e12 ? Math.round(tsRaw / 1e6) : tsRaw;
  else if (typeof tsRaw === 'string') ts = Date.parse(tsRaw) || 0;
  const idNum = typeof raw['id'] === 'number' ? raw['id'] : Number(raw['cursor']) || 0;
  const curRaw = raw['cursor'] ?? raw['id'];
  return {
    id: idNum,
    ts,
    type: str(raw['type']),
    processId: str(raw['processId'] ?? raw['process_id']),
    instanceId: str(raw['instanceId'] ?? raw['instance_id']),
    workspaceId: str(raw['workspaceId'] ?? raw['workspace_id']),
    projectId: str(raw['projectId'] ?? raw['project_id']),
    sessionId: str(raw['sessionId'] ?? raw['session_id']),
    payload,
    cursor: typeof curRaw === 'string' ? curRaw : String(curRaw ?? idNum)
  };
}

export function toRevision(raw: Record<string, unknown>): Revision {
  return {
    id: typeof raw['id'] === 'number' ? raw['id'] : 0,
    layer: str(raw['layer']),
    time: typeof raw['createdAt'] === 'number' ? (raw['createdAt'] as number) * 1000 : (num(raw['createdAt']) ?? 0),
    source: str(raw['source']),
    session: str(raw['sessionId'] ?? raw['session_id']),
    message: str(raw['message']),
    valid: raw['valid'] !== false,
    sha: str(raw['sha256'] ?? raw['sha'])
  };
}

export function toHarness(raw: Record<string, unknown>): Harness {
  return {
    name: str(raw['name'] ?? raw['harness']),
    detected: raw['detected'] === true,
    version: str(raw['version']),
    mcpConfigured: raw['mcpConfigured'] === true || raw['mcp_configured'] === true
  };
}

export function toSession(raw: Record<string, unknown>): Session {
  return {
    id: str(raw['id'] ?? raw['sessionId'] ?? raw['session_id']),
    kind: str(raw['kind'], 'mcp'),
    harness: str(raw['harness']),
    clientPid: typeof raw['clientPid'] === 'number' ? (raw['clientPid'] as number) : 0,
    workspaceId: str(raw['workspaceId'] ?? raw['workspace_id']),
    started: num(raw['startedAt'] ?? raw['started_at']) ?? 0,
    lastSeen: num(raw['lastSeen'] ?? raw['last_seen']) ?? 0,
    closed: raw['closed'] === true
  };
}

export function toAuditEntry(raw: Record<string, unknown>): AuditEntry {
  return {
    id: typeof raw['id'] === 'number' ? raw['id'] : 0,
    time: typeof raw['ts'] === 'number' ? (raw['ts'] as number) * 1000 : (num(raw['ts']) ?? 0),
    action: str(raw['action']),
    result: str(raw['result']),
    duration: typeof raw['durationMs'] === 'number' ? (raw['durationMs'] as number) : (typeof raw['duration'] === 'number' ? (raw['duration'] as number) : 0),
    session: str(raw['sessionId'] ?? raw['session_id']),
    harness: str(raw['harness']),
    workspaceId: str(raw['workspaceId'] ?? raw['workspace_id'])
  };
}

export function toSkill(raw: Record<string, unknown>): Skill {
  return {
    name: str(raw['name']),
    version: str(raw['version']),
    description: str(raw['description']),
    outdated: raw['outdated'] === true
  };
}

export function toResource(raw: Record<string, unknown>): ResourceSample {
  return {
    processId: str(raw['processId'] ?? raw['process_id']),
    pid: typeof raw['pid'] === 'number' ? (raw['pid'] as number) : 0,
    memoryBytes: typeof raw['memoryBytes'] === 'number' ? (raw['memoryBytes'] as number) : 0,
    cpuNanos: typeof raw['cpuNanos'] === 'number' ? (raw['cpuNanos'] as number) : 0,
    cpuPercent: typeof raw['cpuPercent'] === 'number' ? (raw['cpuPercent'] as number) : 0,
    rssBytes: typeof raw['rssBytes'] === 'number' ? (raw['rssBytes'] as number) : 0,
    threads: typeof raw['threads'] === 'number' ? (raw['threads'] as number) : 0,
    openFds: typeof raw['openFds'] === 'number' ? (raw['openFds'] as number) : 0,
    children: typeof raw['children'] === 'number' ? (raw['children'] as number) : 0,
    ports: numArr(raw['ports']),
    at: typeof raw['at'] === 'number' ? (raw['at'] as number) : Date.now()
  };
}
