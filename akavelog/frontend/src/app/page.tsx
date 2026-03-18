'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import {
  getPushUrl,
  getRecentLogs,
  getUploadStatus,
  sendAkavelogPush,
  type AkavelogStream,
  type LogEntry,
  type UploadStatus as UploadStatusType,
} from '@/lib/api';

export default function DemoPage() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [uploadStatus, setUploadStatus] = useState<UploadStatusType | null>(null);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [logMessage, setLogMessage] = useState('');
  const [pushMs, setPushMs] = useState<number | null>(null);
  const [copyCurlDone, setCopyCurlDone] = useState(false);

  const loadLogs = useCallback(async () => {
    try {
      const { logs: list } = await getRecentLogs();
      setLogs(list);
    } catch {
      // ignore
    }
  }, []);

  const loadStatus = useCallback(async () => {
    try {
      const st = await getUploadStatus();
      setUploadStatus(st);
    } catch {
      setUploadStatus(null);
    }
  }, []);

  useEffect(() => {
    loadLogs();
    loadStatus();
  }, [loadLogs, loadStatus]);

  useEffect(() => {
    const t = setInterval(() => {
      loadLogs();
      loadStatus();
    }, 2000);
    return () => clearInterval(t);
  }, [loadLogs, loadStatus]);

  const buildPushPayload = (message: string): AkavelogStream[] => [
    {
      stream: { job: 'demo-ui', app: 'akavelog' },
      values: [[String(Date.now() * 1_000_000), message || `Test log at ${new Date().toISOString()}`]],
    },
  ];

  const handleSendTest = async () => {
    setSending(true);
    setError(null);
    setPushMs(null);
    const payload = buildPushPayload(logMessage);
    const t0 = performance.now();
    try {
      await sendAkavelogPush(payload);
      setPushMs(Math.round(performance.now() - t0));
      await loadLogs();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Send failed');
    } finally {
      setSending(false);
    }
  };

  const getCurlCommand = () => {
    const url = typeof window !== 'undefined' ? getPushUrl() : 'http://localhost:3000/api/akavelog/api/v1/push';
    const payload = buildPushPayload(logMessage);
    const body = JSON.stringify({ streams: payload });
    const escaped = body.replace(/'/g, "'\\''");
    return `curl -X POST '${url}' \\
  -H 'Content-Type: application/json' \\
  -d '${escaped}'`;
  };

  const handleCopyCurl = async () => {
    try {
      await navigator.clipboard.writeText(getCurlCommand());
      setCopyCurlDone(true);
      setTimeout(() => setCopyCurlDone(false), 2000);
    } catch {
      setError('Copy failed');
    }
  };

  return (
    <div className="min-h-screen flex flex-col md:flex-row gap-4 p-4 bg-[var(--bg)]">
      <div className="flex-1 flex flex-col gap-4 min-w-0">
        <header className="border-b border-[var(--border)] pb-2">
          <div className="flex items-center gap-4 flex-wrap">
            <h1 className="text-xl font-semibold text-[var(--accent)]">Akavelog Demo</h1>
            {/* ── Navigation ── */}
            <Link href="/logs" className="text-sm text-[var(--muted)] hover:text-[var(--accent)] transition-colors">
              Log Explorer →
            </Link>
            <Link href="/uploads" className="text-sm text-[var(--muted)] hover:text-[var(--accent)] transition-colors">
              View O3 uploads →
            </Link>
            <Link href="/stored" className="text-sm text-[var(--muted)] hover:text-[var(--accent)] transition-colors">
              Stored data (raw) →
            </Link>
          </div>
          <p className="text-sm text-[var(--muted)]">
            Push logs via POST /akavelog/api/v1/push. Watch uploads to Akave O3.
          </p>
        </header>

        {error && (
          <div className="rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 px-3 py-2 text-sm">
            {error}
          </div>
        )}

        <section className="rounded-xl bg-[var(--card)] border border-[var(--border)] p-4">
          <h2 className="text-sm font-medium text-[var(--muted)] mb-3">Push logs</h2>
          <p className="text-sm text-[var(--muted)] mb-3">
            POST /akavelog/api/v1/push with JSON: streams[].stream (labels), streams[].values ([ts_ns, line]). Chunks flush to O3 after 5s idle or 50 entries.
          </p>
          <div className="mb-3">
            <input
              type="text"
              value={logMessage}
              onChange={(e) => setLogMessage(e.target.value)}
              placeholder="the log"
              className="w-full rounded-lg bg-[var(--bg)] border border-[var(--border)] px-3 py-2 text-sm font-mono placeholder:text-[var(--muted)]"
            />
          </div>
          <div className="flex flex-wrap gap-2 items-center">
            <button
              type="button"
              onClick={handleSendTest}
              disabled={sending}
              className="rounded-lg bg-[var(--accent)] text-[var(--bg)] px-4 py-2 text-sm font-medium disabled:opacity-50"
            >
              {sending ? 'Sending…' : 'Send test log'}
            </button>
            {pushMs != null && (
              <span className="text-sm text-[var(--muted)]">Push took {pushMs} ms</span>
            )}
            <button
              type="button"
              onClick={handleCopyCurl}
              className="rounded-lg bg-[var(--border)] hover:bg-[var(--muted)] px-4 py-2 text-sm disabled:opacity-50"
            >
              {copyCurlDone ? 'Copied!' : 'Copy curl'}
            </button>
          </div>
          <p className="text-xs text-[var(--muted)] mt-2">
            Use &quot;Copy curl&quot; and run in terminal to test the same request. Logs flush to O3 within ~5s of last write.
          </p>
        </section>

        <section className="rounded-xl bg-[var(--card)] border border-[var(--border)] p-4 flex-1 min-h-[200px] flex flex-col">
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-medium text-[var(--muted)]">Incoming logs (last 200)</h2>
            <Link
              href="/logs"
              className="text-xs text-[var(--accent)] hover:underline transition-colors"
            >
              Open Log Explorer →
            </Link>
          </div>
          <div className="flex-1 overflow-auto rounded-lg bg-[var(--bg)] border border-[var(--border)] p-2 font-mono text-xs">
            {logs.length === 0 ? (
              <p className="text-[var(--muted)]">Logs appear here after you push. Polling every 2s.</p>
            ) : (
              <ul className="space-y-2">
                {[...logs].reverse().map((l, i) => (
                  <li key={i} className="border-b border-[var(--border)]/50 pb-2">
                    {l.entry.raw_request ? (
                      <pre className="text-xs whitespace-pre-wrap break-all overflow-x-auto bg-[var(--bg)]/80 p-2 rounded border border-[var(--border)]/50">
                        {l.entry.raw_request.method} {l.entry.raw_request.path}
                        {l.entry.raw_request.query ? `?${l.entry.raw_request.query}` : ''}
                        {l.entry.raw_request.headers && Object.keys(l.entry.raw_request.headers).length > 0 && (
                          <>
                            {'\n'}
                            {Object.entries(l.entry.raw_request.headers).map(([k, v]) => (
                              <span key={k}>{'\n'}{k}: {v}</span>
                            ))}
                          </>
                        )}
                        {l.entry.raw_request.body != null && l.entry.raw_request.body !== '' && (
                          <>{'\n\n'}{l.entry.raw_request.body}</>
                        )}
                      </pre>
                    ) : (
                      <>
                        <span className="text-[var(--muted)]">{new Date(l.received_at).toLocaleTimeString()}</span>
                        {' '}
                        <span className="text-[var(--warn)]">{l.entry.service}</span>
                        {' '}
                        <span className="text-[var(--accent)]">{l.entry.level}</span>
                        {' '}
                        {l.entry.message}
                        {l.entry.tags && Object.keys(l.entry.tags).length > 0 && (
                          <span className="text-[var(--muted)]"> {JSON.stringify(l.entry.tags)}</span>
                        )}
                      </>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </section>
      </div>

      <aside className="w-full md:w-80 shrink-0 rounded-xl bg-[var(--card)] border border-[var(--border)] p-4 h-fit">
        <h2 className="text-sm font-medium text-[var(--muted)] mb-3">Upload status (O3)</h2>
        {!uploadStatus ? (
          <p className="text-sm text-[var(--muted)]">Loading…</p>
        ) : (
          <div className="space-y-3 text-sm">
            <p>
              Batcher:{' '}
              <span className={uploadStatus.batcher_enabled ? 'text-[var(--success)]' : 'text-[var(--muted)]'}>
                {uploadStatus.batcher_enabled ? 'On' : 'Off'}
              </span>
            </p>
            {uploadStatus.batcher_enabled && (
              <>
                <p className="text-[var(--muted)]">
                  Last upload: {uploadStatus.last_upload_count} logs
                </p>
                {uploadStatus.last_upload_at && (
                  <p className="text-xs text-[var(--muted)]">
                    {new Date(uploadStatus.last_upload_at).toLocaleString()}
                  </p>
                )}
                {uploadStatus.last_upload_key && (
                  <p className="text-xs font-mono text-[var(--accent)] break-all">
                    {uploadStatus.last_upload_key}
                  </p>
                )}
              </>
            )}
          </div>
        )}
      </aside>
    </div>
  );
}