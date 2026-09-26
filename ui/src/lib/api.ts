// API client for the daemon Connect-compatible endpoints. In Wails the
// page is served from the Go asset server and /api is proxied to
// agentd.sock; in web builds /api is the daemon TCP listener with a
// bearer token. Streams are newline-delimited JSON (snapshot, deltas,
// cursor, heartbeat, gap) per the API contract.
export interface Cursor {
  value?: string;
}

export interface ApiOptions {
  token?: string;
  client?: string;
}

const base = '';

function headers(opts: ApiOptions): HeadersInit {
  const h: Record<string, string> = {
    'Content-Type': 'application/json',
    'X-Agent-Runtime-Client': opts.client ?? 'gui/3.0.0'
  };
  if (opts.token) h['Authorization'] = `Bearer ${opts.token}`;
  return h;
}

export async function call<T>(service: string, method: string, body: unknown, opts: ApiOptions = {}): Promise<T> {
  const res = await fetch(`${base}/api/agentruntime.v1.${service}/${method}`, {
    method: 'POST',
    headers: headers(opts),
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${service}/${method}: ${res.status} ${text}`);
  }
  return (await res.json()) as T;
}

export interface StreamMessage {
  kind?: string;
  cursor?: string | number;
  [key: string]: unknown;
}

export type StreamHandler = (msg: StreamMessage) => void;

/** Open an NDJSON stream with auto-resume on cursor. Returns a closer. */
export function stream(
  service: string,
  method: string,
  body: Record<string, unknown>,
  onMessage: StreamHandler,
  opts: ApiOptions = {}
): () => void {
  let closed = false;
  let resume: string | number | undefined;
  let retryMs = 1000;

  const connect = async () => {
    if (closed) return;
    try {
      const res = await fetch(`${base}/api/agentruntime.v1.${service}/${method}`, {
        method: 'POST',
        headers: headers(opts),
        body: JSON.stringify({ ...body, resume })
      });
      if (!res.ok || !res.body) throw new Error(`stream ${res.status}`);
      retryMs = 1000;
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = '';
      for (;;) {
        const { done, value } = await reader.read();
        if (done || closed) break;
        buf += decoder.decode(value, { stream: true });
        const lines = buf.split('\n');
        buf = lines.pop() ?? '';
        for (const line of lines) {
          if (!line.trim()) continue;
          try {
            const msg = JSON.parse(line) as StreamMessage;
            if (msg.cursor !== undefined) resume = msg.cursor;
            if (msg.kind === 'gap') console.warn('stream gap', msg);
            else if (msg.kind !== 'heartbeat') onMessage(msg);
          } catch {
            /* partial frame; wait for more */
          }
        }
      }
    } catch (err) {
      if (!closed) {
        console.warn('stream reconnect in', retryMs, err);
        setTimeout(connect, retryMs);
        retryMs = Math.min(retryMs * 2, 15000);
      }
    }
  };
  void connect();
  return () => {
    closed = true;
  };
}

export const SystemService = {
  version: (o?: ApiOptions) => call('SystemService', 'GetVersion', {}, o),
  health: (o?: ApiOptions) => call('SystemService', 'Health', {}, o),
  stats: (o?: ApiOptions) => call('SystemService', 'GetStats', {}, o)
};

export const ProcessService = {
  list: (workspaceId: string, all = false, o?: ApiOptions) =>
    call('ProcessService', 'List', { workspaceId, allWorkspaces: all }, o),
  get: (processId: string, o?: ApiOptions) => call('ProcessService', 'Get', { processId }, o),
  start: (req: Record<string, unknown>, o?: ApiOptions) => call('ProcessService', 'Start', req, o),
  stop: (processId: string, o?: ApiOptions) => call('ProcessService', 'Stop', { processId }, o),
  restart: (processId: string, o?: ApiOptions) => call('ProcessService', 'Restart', { processId }, o)
};

export const LogService = {
  get: (processId: string, lines = 500, o?: ApiOptions) =>
    call('LogService', 'GetLogs', { process_id: processId, lines }, o),
  search: (req: Record<string, unknown>, o?: ApiOptions) => call('LogService', 'SearchLogs', req, o)
};

export const ConfigService = {
  get: (workspaceId: string, o?: ApiOptions) => call('ConfigService', 'GetConfig', { workspaceId }, o),
  validate: (yaml: string, o?: ApiOptions) => call('ConfigService', 'Validate', { yaml }, o),
  plan: (workspaceId: string, yaml: string, o?: ApiOptions) =>
    call('ConfigService', 'Plan', { workspaceId, yaml }, o),
  apply: (req: Record<string, unknown>, o?: ApiOptions) => call('ConfigService', 'Apply', req, o)
};
