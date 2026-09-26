export class ApiError extends Error {
  status: number;
  code: string;
  constructor(message: string, status: number, code: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

const TOKEN_KEY = 'ar.token';
let authToken: string | null = null;

function loadToken(): string | null {
  if (typeof window === 'undefined') return null;
  const fromHash = window.location.hash.match(/#token=([^&]+)/);
  if (fromHash) {
    const t = decodeURIComponent(fromHash[1]);
    try {
      sessionStorage.setItem(TOKEN_KEY, t);
    } catch {
    }
    const url = new URL(window.location.href);
    url.hash = '';
    window.history.replaceState(null, '', url.toString());
    return t;
  }
  try {
    return sessionStorage.getItem(TOKEN_KEY) ?? localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function setAuthToken(token: string, remember = false): void {
  authToken = token;
  try {
    if (remember) localStorage.setItem(TOKEN_KEY, token);
    else sessionStorage.setItem(TOKEN_KEY, token);
  } catch {
  }
}

export function clearAuthToken(): void {
  authToken = null;
  try {
    sessionStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(TOKEN_KEY);
  } catch {
  }
}

export function getAuthToken(): string | null {
  if (authToken === null) authToken = loadToken();
  return authToken;
}

export function hasAuthToken(): boolean {
  return getAuthToken() !== null;
}

function headers(): HeadersInit {
  const h: Record<string, string> = {
    'Content-Type': 'application/json',
    'X-Agent-Runtime-Client': `${__APP_TARGET__}/${__APP_VERSION__}`
  };
  const token = getAuthToken();
  if (token) h['Authorization'] = `Bearer ${token}`;
  return h;
}

let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn;
}

export async function call<T>(
  service: string,
  method: string,
  body: unknown,
  opts: { timeoutMs?: number; signal?: AbortSignal } = {}
): Promise<T> {
  const controller = new AbortController();
  const timeout = opts.timeoutMs ?? 15000;
  const timer = setTimeout(() => controller.abort(), timeout);
  if (opts.signal) {
    opts.signal.addEventListener('abort', () => controller.abort());
  }
  try {
    const res = await fetch(`/api/agentruntime.v1.${service}/${method}`, {
      method: 'POST',
      headers: headers(),
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: controller.signal
    });
    if (res.status === 401) {
      onUnauthorized?.();
      throw new ApiError('unauthorized', 401, 'unauthorized');
    }
    const text = await res.text();
    let parsed: unknown = undefined;
    if (text) {
      try {
        parsed = JSON.parse(text);
      } catch {
        parsed = undefined;
      }
    }
    if (!res.ok) {
      const p = (parsed ?? {}) as { error?: string; code?: string };
      throw new ApiError(p.error ?? text ?? `${res.status}`, res.status, p.code ?? 'internal');
    }
    return (parsed ?? {}) as T;
  } catch (err) {
    if (err instanceof ApiError) throw err;
    if (controller.signal.aborted) throw new ApiError('request timed out', 0, 'timeout');
    throw new ApiError(err instanceof Error ? err.message : String(err), 0, 'unavailable');
  } finally {
    clearTimeout(timer);
  }
}

export interface StreamMessage {
  kind?: string;
  cursor?: string | number;
  [key: string]: unknown;
}

export type StreamHandler = (msg: StreamMessage) => void;
export type StreamStateHandler = (state: 'connecting' | 'open' | 'reconnecting' | 'closed') => void;

export interface StreamHandle {
  close(): void;
}

export function stream(
  service: string,
  method: string,
  body: Record<string, unknown>,
  onMessage: StreamHandler,
  onState?: StreamStateHandler
): StreamHandle {
  let closed = false;
  let resume: string | number | undefined;
  let retryMs = 1000;
  let controller: AbortController | null = null;

  const connect = async () => {
    if (closed) return;
    controller = new AbortController();
    onState?.('connecting');
    try {
      const res = await fetch(`/api/agentruntime.v1.${service}/${method}`, {
        method: 'POST',
        headers: headers(),
        body: JSON.stringify({ ...body, resume }),
        signal: controller.signal
      });
      if (res.status === 401) {
        onUnauthorized?.();
        return;
      }
      if (!res.ok || !res.body) throw new Error(`stream ${res.status}`);
      retryMs = 1000;
      onState?.('open');
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = '';
      for (;;) {
        const { done, value } = await reader.read();
        if (done || closed) break;
        buf += decoder.decode(value, { stream: true });
        const parts = buf.split('\n');
        buf = parts.pop() ?? '';
        for (const line of parts) {
          if (!line.trim()) continue;
          try {
            const msg = JSON.parse(line) as StreamMessage;
            if (msg.cursor !== undefined) resume = msg.cursor;
            if (msg.kind === 'heartbeat') continue;
            onMessage(msg);
          } catch {
          }
        }
      }
    } catch (err) {
      if (controller?.signal.aborted) return;
      void err;
    }
    if (!closed) {
      onState?.('reconnecting');
      setTimeout(connect, retryMs);
      retryMs = Math.min(retryMs * 2, 15000);
    }
  };

  void connect();
  return {
    close() {
      closed = true;
      onState?.('closed');
      controller?.abort();
    }
  };
}

export const SystemService = {
  version: () => call<{ daemonVersion: string; apiVersion: string; minClientVersion: string }>('SystemService', 'GetVersion', {}),
  health: () => call<{ healthy: boolean; status: string; uptimeMs: number }>('SystemService', 'Health', {}),
  stats: () => call<Record<string, unknown>>('SystemService', 'GetStats', {}),
  getSettings: () => call<{ settings: Record<string, unknown> }>('SystemService', 'GetSettings', {}),
  updateSettings: (req: Record<string, unknown>) => call('SystemService', 'UpdateSettings', req),
  shutdown: (keepProcesses = true) => call('SystemService', 'Shutdown', { keepProcesses })
};

export const ProjectService = {
  resolve: (path: string) =>
    call<{ projectId: string; workspaceId: string; projectName: string }>('ProjectService', 'Resolve', { path }),
  list: () => call<{ projects: Record<string, unknown>[] }>('ProjectService', 'ListProjects', {}),
  get: (projectId: string) => call<{ project: Record<string, unknown> }>('ProjectService', 'GetProject', { projectId }),
  update: (projectId: string, name: string) => call('ProjectService', 'UpdateProject', { projectId, name }),
  workspaces: (projectId: string) =>
    call<{ workspaces: Record<string, unknown>[] }>('ProjectService', 'ListWorkspaces', { projectId }),
  linkWorkspace: (projectId: string, path: string) => call('ProjectService', 'LinkWorkspace', { projectId, path }),
  forget: (projectId: string) => call('ProjectService', 'ForgetProject', { projectId }),
  gc: () => call<{ removedWorkspaces: string[] }>('ProjectService', 'GC', {}),
  trustRepoConfig: (workspaceId: string, path: string, sha256: string) =>
    call('ProjectService', 'TrustRepoConfig', { workspaceId, path, sha256 })
};

export const ProcessService = {
  list: (workspaceId: string, allWorkspaces = false) =>
    call<{ processes: Record<string, unknown>[] }>('ProcessService', 'List', { workspaceId, allWorkspaces }),
  get: (processId: string) => call<Record<string, unknown>>('ProcessService', 'Get', { processId }),
  start: (req: Record<string, unknown>) => call<Record<string, unknown>>('ProcessService', 'Start', req),
  stop: (processId: string, signal?: string) => call('ProcessService', 'Stop', { processId, signal }),
  restart: (processId: string) => call('ProcessService', 'Restart', { processId }),
  signal: (processId: string, signal: string) => call('ProcessService', 'Signal', { processId, signal }),
  sendStdin: (processId: string, data: string) => call('ProcessService', 'SendStdin', { processId, data }),
  remove: (processId: string, force = false) => call('ProcessService', 'Remove', { processId, force }),
  setRestartPolicy: (processId: string, policy: string) =>
    call('ProcessService', 'SetRestartPolicy', { processId, policy }),
  getEnv: (processId: string, reveal = false, live = false) =>
    call<Record<string, unknown>>('ProcessService', 'GetEnv', { processId, reveal, live }),
  openShell: (req: Record<string, unknown>) => call<Record<string, unknown>>('ProcessService', 'OpenShell', req),
  getResourceUsage: (processId: string) =>
    call<{ cpuNanos: number; memoryBytes: number; pid: number; ports: number[] }>('ProcessService', 'GetResourceUsage', {
      processId
    }),
  startStack: (workspaceId: string, apps: string[]) =>
    call<Record<string, unknown>>('ProcessService', 'StartStack', { workspaceId, apps }),
  watch: (workspaceId: string, allWorkspaces: boolean, onMessage: StreamHandler, onState?: StreamStateHandler) =>
    stream('ProcessService', 'WatchProcesses', { workspaceId, allWorkspaces }, onMessage, onState),
  watchResourceUsage: (processId: string, onMessage: StreamHandler) =>
    stream('ProcessService', 'WatchResourceUsage', { processId }, onMessage),
  attach: (processId: string, backlog: number, onMessage: StreamHandler, onState?: StreamStateHandler) =>
    stream('ProcessService', 'Attach', { processId, backlog }, onMessage, onState)
};

export const LogService = {
  get: (processId: string, lines = 500) =>
    call<{ entries: { line: string; stream: string; timestamp?: string }[] }>('LogService', 'GetLogs', {
      processId,
      lines
    }),
  search: (req: Record<string, unknown>) => call<Record<string, unknown>>('LogService', 'SearchLogs', req),
  clear: (processId: string, stream = 'all') => call('LogService', 'ClearLogs', { processId, stream }),
  stats: (processId: string) => call<Record<string, unknown>>('LogService', 'GetLogStats', { processId }),
  tail: (
    workspaceId: string,
    processIds: string[],
    backlog: number,
    onMessage: StreamHandler,
    onState?: StreamStateHandler
  ) => stream('LogService', 'TailLogs', { workspaceId, processIds, backlog }, onMessage, onState)
};

export const ConfigService = {
  get: (workspaceId: string) => call<Record<string, unknown>>('ConfigService', 'GetConfig', { workspaceId }),
  schema: () => call<{ schema: unknown }>('ConfigService', 'GetSchema', {}),
  validate: (yaml: string) =>
    call<{ valid: boolean; errors: { line: number; column: number; path: string; message: string }[] }>(
      'ConfigService',
      'Validate',
      { yaml }
    ),
  plan: (workspaceId: string, yaml: string) =>
    call<{ changes?: { app: string; kind: string; fields: string[] }[]; errors?: unknown[] }>(
      'ConfigService',
      'Plan',
      { workspaceId, yaml }
    ),
  apply: (req: Record<string, unknown>) =>
    call<{ applied: boolean; revision?: number; errors?: unknown[] }>('ConfigService', 'Apply', req),
  revisions: (projectId: string, limit = 20) =>
    call<{ revisions: Record<string, unknown>[] }>('ConfigService', 'ListRevisions', { projectId, limit }),
  revision: (projectId: string, id: number) =>
    call<{ revision: Record<string, unknown>; content: string }>('ConfigService', 'GetRevision', {
      projectId,
      id: id
    }),
  rollback: (projectId: string, revision: number) => call('ConfigService', 'Rollback', { projectId, revision })
};

export const EventService = {
  list: (workspaceId: string, limit = 100) =>
    call<{ events: Record<string, unknown>[] }>('EventService', 'ListEvents', { workspaceId, limit }),
  watch: (workspaceId: string, onMessage: StreamHandler) =>
    stream('EventService', 'WatchEvents', { workspaceId }, onMessage)
};

export const SessionService = {
  list: () => call<{ sessions: Record<string, unknown>[] }>('SessionService', 'ListSessions', {}),
  close: (sessionId: string) => call('SessionService', 'Close', { sessionId })
};

export const IntegrationService = {
  listHarnesses: () => call<{ harnesses: Record<string, unknown>[] }>('IntegrationService', 'ListHarnesses', {}),
  previewInstall: (harness: string, scope: string) =>
    call<{ diff: string }>('IntegrationService', 'PreviewInstall', { harness, scope }),
  installMCP: (harness: string, scope: string) =>
    call<{ path: string; diff: string; changed?: boolean }>('IntegrationService', 'InstallMCP', { harness, scope }),
  removeMCP: (harness: string, scope: string) =>
    call<{ path: string; changed?: boolean }>('IntegrationService', 'RemoveMCP', { harness, scope }),
  listSkills: () => call<{ skills: Record<string, unknown>[] }>('IntegrationService', 'ListSkills', {}),
  installSkills: (skillNames: string[], scope: string) =>
    call('IntegrationService', 'InstallSkills', { skillNames, scope }),
  removeSkills: (skillNames: string[]) => call('IntegrationService', 'RemoveSkills', { skillNames }),
  checkUpdates: () => call<{ updates: Record<string, unknown>[] }>('IntegrationService', 'CheckUpdates', {})
};

export const AuditService = {
  list: (workspaceId: string, limit = 100) =>
    call<{ entries: Record<string, unknown>[] }>('AuditService', 'ListAudit', { workspaceId, limit })
};
