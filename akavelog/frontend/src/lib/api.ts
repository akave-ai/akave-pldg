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
