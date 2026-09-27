import { clearAuthToken, getAuthToken } from './auth';

export class ApiError extends Error {
  status: number;
  code: string;
  details: unknown;
  service: string;
  method: string;
  constructor(message: string, status: number, code: string, service = '', method = '', details: unknown = undefined) {
    super(message);
    this.status = status;
    this.code = code;
    this.service = service;
    this.method = method;
    this.details = details;
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError;
}

export function codeOf(e: unknown): string {
  return e instanceof ApiError ? e.code : 'internal';
}

let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn;
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

export async function call<Req, Res>(
  service: string,
  method: string,
  body: Req,
  opts: { signal?: AbortSignal; timeoutMs?: number } = {}
): Promise<Res> {
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
      if (__APP_TARGET__ === 'web') {
        clearAuthToken();
        onUnauthorized?.();
      }
      throw new ApiError('unauthorized', 401, 'unauthorized', service, method);
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
      const p = (parsed ?? {}) as { error?: string; code?: string; details?: unknown };
      throw new ApiError(p.error ?? text ?? `${res.status}`, res.status, p.code ?? 'internal', service, method, p.details);
    }
    return (parsed ?? {}) as Res;
  } catch (err) {
    if (err instanceof ApiError) throw err;
    if (controller.signal.aborted) throw new ApiError('request timed out', 0, 'timeout', service, method);
    if (err instanceof TypeError) throw new ApiError(err.message, 0, 'unavailable', service, method);
    throw new ApiError(err instanceof Error ? err.message : String(err), 0, 'unavailable', service, method);
  } finally {
    clearTimeout(timer);
  }
}

export interface StreamMessage {
  kind?: string;
  cursor?: string | number;
  [key: string]: unknown;
}

export type StreamState = 'connecting' | 'open' | 'reconnecting' | 'closed';
export type StreamStateHandler = (s: StreamState) => void;
export interface StreamHandle {
  close: () => void;
}
export type StreamHandler = (msg: StreamMessage) => void;

export function openStream(
  service: string,
  method: string,
  body: Record<string, unknown>,
  handlers: {
    onMessage: (msg: StreamMessage) => void;
    onState?: (s: StreamState) => void;
    onGap?: (dropped: number) => void;
    onSnapshot?: (msg: StreamMessage) => void;
    resume?: () => Record<string, unknown>;
  },
  opts: { heartbeatTimeoutMs?: number } = {}
): { close: () => void } {
  let closed = false;
  let retryMs = 500;
  let controller: AbortController | null = null;
  let healthySince = Date.now();
  let lastFrame = Date.now();
  let watchdog: ReturnType<typeof setInterval> | null = null;
  const hbTimeout = opts.heartbeatTimeoutMs ?? 40000;

  const onlineHandler = () => {
    if (!closed && navigator.onLine) {
      retryMs = 500;
      void connect();
    }
  };
  const visHandler = () => {
    if (!closed && document.visibilityState === 'visible') {
      retryMs = 500;
      void connect();
    }
  };
  if (typeof window !== 'undefined') {
    window.addEventListener('online', onlineHandler);
    document.addEventListener('visibilitychange', visHandler);
    watchdog = setInterval(() => {
      if (!closed && Date.now() - lastFrame > hbTimeout) {
        controller?.abort();
      }
    }, 5000);
  }

  const cleanup = () => {
    if (typeof window !== 'undefined') {
      window.removeEventListener('online', onlineHandler);
      document.removeEventListener('visibilitychange', visHandler);
      if (watchdog) clearInterval(watchdog);
    }
  };

  const connect = async () => {
    if (closed) return;
    if (typeof navigator !== 'undefined' && navigator.onLine === false) {
      handlers.onState?.('reconnecting');
      setTimeout(() => {
        if (!closed) void connect();
      }, 2000);
      return;
    }
    controller = new AbortController();
    handlers.onState?.('connecting');
    try {
      const patch = handlers.resume?.() ?? {};
      const res = await fetch(`/api/agentruntime.v1.${service}/${method}`, {
        method: 'POST',
        headers: headers(),
        body: JSON.stringify({ ...body, ...patch }),
        signal: controller.signal
      });
      if (res.status === 401) {
        if (__APP_TARGET__ === 'web') {
          clearAuthToken();
          onUnauthorized?.();
        }
        return;
      }
      if (!res.ok || !res.body) throw new Error(`stream ${res.status}`);
      handlers.onState?.('open');
      healthySince = Date.now();
      lastFrame = Date.now();
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
          lastFrame = Date.now();
          try {
            const msg = JSON.parse(line) as StreamMessage;
            if (msg.kind === 'heartbeat') continue;
            if (msg.kind === 'gap') {
              handlers.onGap?.(typeof msg['dropped'] === 'number' ? (msg['dropped'] as number) : 0);
              continue;
            }
            if (msg.kind === 'snapshot') {
              handlers.onSnapshot?.(msg);
              continue;
            }
            handlers.onMessage(msg);
          } catch {
          }
        }
        if (Date.now() - healthySince > 10000) {
          retryMs = 500;
          healthySince = Date.now();
        }
      }
    } catch {
      if (controller?.signal.aborted && closed) return;
    }
    if (!closed) {
      handlers.onState?.('reconnecting');
      const jitter = Math.random() * retryMs;
      const delay = Math.min(retryMs / 2 + jitter, 15000);
      setTimeout(() => {
        if (!closed) void connect();
      }, Math.max(500, delay));
      retryMs = Math.min(retryMs * 2, 15000);
    }
  };

  void connect();
  return {
    close() {
      closed = true;
      handlers.onState?.('closed');
      controller?.abort();
      cleanup();
    }
  };
}
