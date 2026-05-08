const API = process.env.NEXT_PUBLIC_API_URL || '/api';

// Standard API response shape (success)
export type APIResponse<T = unknown> = {
  data: T;
  status: number;
  message?: string;
  path: string;
};

// Standard API error shape
export type APIError = {
  message: string;
  error: string;
  path: string;
  status: number;
};

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, init);
  const body = await r.json().catch(() => ({}));
  if (!r.ok) {
    const err = body as Partial<APIError>;
    const msg = err?.message || err?.error || r.statusText || 'Request failed';
    throw new Error(msg);
  }
  const res = body as APIResponse<T>;
  return res.data !== undefined ? res.data : (body as T);
}

export type RawRequestData = {
  method: string;
  path: string;
  query?: string;
  headers?: Record<string, string>;
  body?: string;
};

export type LogEntry = {
  entry: {
    timestamp: string;
    service: string;
    level: string;
    message: string;
    tags?: Record<string, string>;
    raw_request?: RawRequestData;
  };
  received_at: string;
};

export type UploadStatus = {
  batcher_enabled: boolean;
  last_upload_at: string;
  last_upload_key: string;
  last_upload_count: number;
  pending_count: number;
};

export async function getRecentLogs(): Promise<{ logs: LogEntry[] }> {
  return request<{ logs: LogEntry[] }>(`${API}/logs/recent`);
}

/** Akavelog push API: POST /akavelog/api/v1/push (streams/values). */
export type AkavelogStream = {
  stream: Record<string, string>;
  values: [string, string][]; // [timestamp_ns, log_line]
};

const PUSH_PATH = '/akavelog/api/v1/push';

export function getPushUrl(): string {
  const base = typeof window !== 'undefined' && API.startsWith('/') ? window.location.origin : '';
  return `${base}${API}${PUSH_PATH}`;
}

export async function sendAkavelogPush(streams: AkavelogStream[]): Promise<void> {
  const r = await fetch(`${API}${PUSH_PATH}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ streams }),
  });
  if (r.status === 204) return;
  const body = await r.json().catch(() => ({}));
  const err = body as Partial<APIError>;
  throw new Error(err?.message || err?.error || r.statusText || 'Push failed');
}

export async function getUploadStatus(): Promise<UploadStatus> {
  return request<UploadStatus>(`${API}/logs/status`);
}

export type O3ObjectInfo = {
  key: string;
  size: number;
  last_modified: string;
};

export async function getUploads(prefix?: string): Promise<{ objects: O3ObjectInfo[] }> {
  const url = prefix ? `${API}/uploads?prefix=${encodeURIComponent(prefix)}` : `${API}/uploads`;
  return request<{ objects: O3ObjectInfo[] }>(url);
}

export type StoredLogEntry = {
  timestamp?: string;
  service?: string;
  level?: string;
  message?: string;
  tags?: Record<string, string>;
  raw_request?: RawRequestData;
};

export async function getUploadContent(key: string): Promise<{ logs: StoredLogEntry[]; key: string }> {
  return request<{ logs: StoredLogEntry[]; key: string }>(
    `${API}/uploads/content?key=${encodeURIComponent(key)}`
  );
}

export type RawObjectResponse = {
  key: string;
  content: string;
  encoding: string;
};

export async function getUploadRaw(key: string): Promise<RawObjectResponse> {
  return request<RawObjectResponse>(
    `${API}/uploads/raw?key=${encodeURIComponent(key)}`
  );
}

// ── Phase 5: Query Engine ──────────────────────────────────────────────────

export type QueryResultEntry = {
  ts_ns: number;
  timestamp: string; // RFC3339Nano
  service: string;
  level: string;
  line: string;
  labels: Record<string, string>;
  o3_object_key: string;
};

export type QueryResponse = {
  results: QueryResultEntry[];
  count: number;
  truncated: boolean;
};

export type QueryRequest = {
  tenant?: string;
  service?: string;
  levels?: string[];
  keyword?: string;
  time_start?: string; // RFC3339
  time_end?: string;   // RFC3339
  limit?: number;
};

/** POST /query — full buffered result set */
export async function queryLogs(req: QueryRequest): Promise<QueryResponse> {
  const r = await fetch(`${API}/query`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  if (!r.ok) {
    const body = await r.json().catch(() => ({}));
    const err = body as Partial<APIError>;
    throw new Error(err?.message || err?.error || r.statusText || 'Query failed');
  }
  return r.json() as Promise<QueryResponse>;
}

export type SSEDoneEvent = { count: number; truncated: boolean };
export type SSEErrorEvent = { error: string };

/**
 * GET /query/stream — Server-Sent Events streaming query.
 * Calls onEntry for each log entry as it arrives.
 * Calls onDone when the stream ends.
 * Returns an AbortController so the caller can cancel.
 */
export function streamLogs(
  req: QueryRequest,
  onEntry: (entry: QueryResultEntry) => void,
  onDone: (summary: SSEDoneEvent) => void,
  onError: (err: string) => void,
): AbortController {
  const ctrl = new AbortController();

  const params = new URLSearchParams();
  if (req.tenant) params.set('tenant', req.tenant);
  if (req.service) params.set('service', req.service);
  if (req.levels?.length) params.set('levels', req.levels.join(','));
  if (req.keyword) params.set('keyword', req.keyword);
  if (req.time_start) params.set('from', req.time_start);
  if (req.time_end) params.set('to', req.time_end);
  if (req.limit) params.set('limit', String(req.limit));

  const url = `${API}/query/stream?${params.toString()}`;

  (async () => {
    try {
      const resp = await fetch(url, { signal: ctrl.signal });
      if (!resp.ok || !resp.body) {
        onError(`HTTP ${resp.status}: ${resp.statusText}`);
        return;
      }

      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const parts = buffer.split('\n\n');
        buffer = parts.pop() ?? '';

        for (const block of parts) {
          const lines = block.split('\n');
          let eventType = '';
          let data = '';

          for (const line of lines) {
            if (line.startsWith('event: ')) eventType = line.slice(7).trim();
            if (line.startsWith('data: ')) data = line.slice(6).trim();
          }

          if (!data) continue;

          try {
            const parsed = JSON.parse(data);
            if (eventType === 'log') onEntry(parsed as QueryResultEntry);
            else if (eventType === 'done') onDone(parsed as SSEDoneEvent);
            else if (eventType === 'error') onError((parsed as SSEErrorEvent).error);
          } catch {
            // ignore malformed SSE
          }
        }
      }
    } catch (e) {
      if ((e as Error).name !== 'AbortError') {
        onError((e as Error).message || 'Stream failed');
      }
    }
  })();

  return ctrl;
}