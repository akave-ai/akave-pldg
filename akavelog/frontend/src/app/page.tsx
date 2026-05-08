'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import {
  getUploadStatus,
  getRecentLogs,
  sendAkavelogPush,
  type AkavelogStream,
  type LogEntry,
  type UploadStatus,
} from '@/lib/api';

const LEVELS = ['error', 'warn', 'info', 'debug', 'fatal', 'trace'] as const;

const LEVEL_COLORS: Record<string, string> = {
  error: '#f87171',
  fatal: '#fca5a5',
  warn:  '#facc15',
  info:  '#22d3ee',
  debug: '#a1a1aa',
  trace: '#c084fc',
};

export default function LogInserterPage() {
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  const [serviceName, setServiceName] = useState('akavelog');
  const [level, setLevel] = useState<(typeof LEVELS)[number]>('info');
  const [logMessage, setLogMessage] = useState('');

  const [uploadStatus, setUploadStatus] = useState<UploadStatus | null>(null);
  const [recentLogs, setRecentLogs] = useState<LogEntry[]>([]);

  const loadStatus = useCallback(async () => {
    try {
      const st = await getUploadStatus();
      setUploadStatus(st);
    } catch {
      setUploadStatus(null);
    }
  }, []);

  useEffect(() => {
    loadStatus();
    const t = setInterval(loadStatus, 2000);
    return () => clearInterval(t);
  }, [loadStatus]);

  const loadRecent = useCallback(async () => {
    try {
      const { logs } = await getRecentLogs();
      setRecentLogs(logs);
    } catch { /* ignore */ }
  }, []);

  useEffect(() => {
    loadRecent();
    const t = setInterval(loadRecent, 2000);
    return () => clearInterval(t);
  }, [loadRecent]);

  // ── Metrics ─────────────────────────────────────────────────────────────────
  const nowMs = Date.now();
  const windowMs = 10 * 60 * 1000;
  const windowStartMs = nowMs - windowMs;

  const logsInWindow = recentLogs.filter(l => {
    const t = Date.parse(l.received_at);
    return !Number.isNaN(t) && t >= windowStartMs && t <= nowMs;
  });

  const pushRatePerMin = logsInWindow.length / 10;

  const BUCKETS = 12;
  const bucketMs = windowMs / BUCKETS;
  const buckets = Array.from({ length: BUCKETS }, () => 0);
  for (const l of logsInWindow) {
    const t = Date.parse(l.received_at);
    const idx = Math.floor((t - windowStartMs) / bucketMs);
    if (idx >= 0 && idx < BUCKETS) buckets[idx] += 1;
  }
  const maxBucket = Math.max(...buckets, 1);

  const levelOrder = ['error', 'fatal', 'warn', 'info', 'debug', 'trace'] as const;
  const levelCounts = Object.fromEntries(levelOrder.map(lv => [lv, 0])) as Record<(typeof levelOrder)[number], number>;
  for (const l of logsInWindow) {
    const lvl = String(l.entry.level || '').toLowerCase() as (typeof levelOrder)[number];
    if (lvl in levelCounts) levelCounts[lvl] += 1;
  }

  const buildPushPayload = (message: string): AkavelogStream[] => [
    {
      stream: { app: 'akavelog', job: serviceName.trim() || 'akavelog', level },
      values: [[String(Date.now() * 1_000_000), message || `log at ${new Date().toISOString()}`]],
    },
  ];

  const handleSend = async () => {
    setSending(true);
    setError(null);
    setSuccess(false);
    try {
      await sendAkavelogPush(buildPushPayload(logMessage));
      await loadStatus();
      setSuccess(true);
      setTimeout(() => setSuccess(false), 2000);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Send failed');
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="min-h-screen flex flex-col bg-[var(--bg)] text-[var(--text)]">

      {/* ── Header ── */}
      <header className="border-b border-[var(--border)] px-6 py-4 flex items-center gap-6">
        <h1 className="text-base font-semibold text-[var(--accent)] tracking-wide uppercase">
          Akavelog
        </h1>
        <nav className="flex items-center gap-4 ml-2">
          <Link href="/logs"    className="text-xs text-[var(--muted)] hover:text-[var(--accent)] transition-colors">Log Explorer →</Link>
          <Link href="/uploads" className="text-xs text-[var(--muted)] hover:text-[var(--accent)] transition-colors">O3 Uploads →</Link>
        </nav>
        <span className="ml-auto text-[10px] text-[var(--muted)] font-mono">
          POST /akavelog/api/v1/push
        </span>
      </header>

      <main className="flex-1 px-6 py-6 grid grid-cols-1 lg:grid-cols-3 gap-6 items-start max-w-6xl mx-auto w-full">

        {/* ── Left column: Push form + Rate chart ── */}
        <div className="lg:col-span-2 flex flex-col gap-5">

          {/* Push form */}
          <section className="rounded-xl bg-[var(--card)] border border-[var(--border)] p-5">
            <h2 className="text-sm font-semibold text-[var(--text)] mb-4">Push log</h2>

            {error && (
              <div className="rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 px-3 py-2 text-xs mb-4">
                {error}
              </div>
            )}

            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-3">
              <div className="sm:col-span-2">
                <label className="block text-xs text-[var(--muted)] mb-1">Service (job label)</label>
                <input
                  type="text"
                  value={serviceName}
                  onChange={e => setServiceName(e.target.value)}
                  placeholder="e.g. payments-api"
                  className="w-full rounded-lg bg-[var(--bg)] border border-[var(--border)] px-3 py-2 text-sm font-mono placeholder:text-[var(--muted)] focus:outline-none focus:border-[var(--accent)] transition-colors"
                />
              </div>
              <div>
                <label className="block text-xs text-[var(--muted)] mb-1">Level</label>
                <select
                  value={level}
                  onChange={e => setLevel(e.target.value as (typeof LEVELS)[number])}
                  className="w-full rounded-lg bg-[var(--bg)] border border-[var(--border)] px-3 py-2 text-sm font-mono text-[var(--text)] focus:outline-none focus:border-[var(--accent)] transition-colors"
                >
                  {LEVELS.map(l => <option key={l} value={l}>{l}</option>)}
                </select>
              </div>
            </div>

            <div className="mb-4">
              <label className="block text-xs text-[var(--muted)] mb-1">Message</label>
              <input
                type="text"
                value={logMessage}
                onChange={e => setLogMessage(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') handleSend(); }}
                placeholder="your log message…"
                className="w-full rounded-lg bg-[var(--bg)] border border-[var(--border)] px-3 py-2 text-sm font-mono placeholder:text-[var(--muted)] focus:outline-none focus:border-[var(--accent)] transition-colors"
              />
            </div>

            <div className="flex items-center gap-3">
              <button
                type="button"
                onClick={handleSend}
                disabled={sending}
                className="rounded-lg bg-[var(--accent)] text-[var(--bg)] px-5 py-2 text-sm font-semibold disabled:opacity-50 hover:opacity-90 transition-opacity"
              >
                {sending ? 'Sending…' : 'Push log'}
              </button>
              {success && <span className="text-xs text-[var(--success)]">✓ Sent</span>}
            </div>
          </section>

          {/* Rate chart */}
          <section className="rounded-xl bg-[var(--card)] border border-[var(--border)] p-5">
            <div className="flex items-baseline justify-between mb-1">
              <h2 className="text-sm font-semibold text-[var(--text)]">Push rate</h2>
              <span className="text-xs text-[var(--muted)]">last 10 min</span>
            </div>

            <div className="flex items-baseline gap-2 mb-1">
              <span className="text-2xl font-bold text-[var(--accent)]">{pushRatePerMin.toFixed(1)}</span>
              <span className="text-xs text-[var(--muted)]">logs / min</span>
              <span className="text-xs text-[var(--muted)] ml-2">({logsInWindow.length} total)</span>
            </div>

            {/* Bar chart */}
            <div className="mt-4 flex items-end gap-[3px] h-24">
              {buckets.map((c, i) => {
                const heightPct = c === 0 ? 4 : Math.max(6, Math.round((c / maxBucket) * 100));
                const isLast = i === buckets.length - 1;
                return (
                  <div
                    key={i}
                    className="flex-1 rounded-sm transition-all"
                    style={{
                      height: `${heightPct}%`,
                      background: isLast
                        ? 'var(--accent)'
                        : c === 0 ? 'var(--border)' : `color-mix(in srgb, var(--accent) ${30 + Math.round((c / maxBucket) * 70)}%, var(--border))`,
                      opacity: isLast ? 1 : 0.6 + (i / buckets.length) * 0.4,
                    }}
                    title={`${c} logs`}
                  />
                );
              })}
            </div>
            <div className="flex justify-between mt-1 text-[9px] text-[var(--muted)]">
              <span>10m ago</span>
              <span>now</span>
            </div>

            {/* Level breakdown */}
            <div className="mt-4 pt-4 border-t border-[var(--border)]">
              <p className="text-[10px] uppercase tracking-wider text-[var(--muted)] mb-2">By level</p>
              <div className="grid grid-cols-3 gap-y-1.5 gap-x-4">
                {levelOrder.map(lv => (
                  <div key={lv} className="flex items-center justify-between gap-2">
                    <span className="text-xs font-mono" style={{ color: LEVEL_COLORS[lv] }}>
                      {lv}
                    </span>
                    <span className="text-xs font-mono text-[var(--text)]">{levelCounts[lv]}</span>
                  </div>
                ))}
              </div>
            </div>
          </section>
        </div>

        {/* ── Right column: O3 Upload status ── */}
        <aside className="rounded-xl bg-[var(--card)] border border-[var(--border)] p-5 h-fit lg:sticky lg:top-6">
          <h2 className="text-sm font-semibold text-[var(--text)] mb-4">O3 Upload Status</h2>

          {!uploadStatus ? (
            <div className="space-y-2">
              {[...Array(4)].map((_, i) => (
                <div key={i} className="h-4 rounded bg-[var(--border)]/40 animate-pulse" style={{ width: `${60 + i * 10}%` }} />
              ))}
            </div>
          ) : (
            <div className="space-y-3 text-sm">
              {/* Batcher status */}
              <div className="flex items-center justify-between">
                <span className="text-xs text-[var(--muted)]">Batcher</span>
                <span className={`text-xs font-semibold px-2 py-0.5 rounded-full border ${
                  uploadStatus.batcher_enabled
                    ? 'text-[var(--success)] bg-green-500/10 border-green-500/30'
                    : 'text-[var(--muted)] bg-zinc-500/10 border-zinc-500/30'
                }`}>
                  {uploadStatus.batcher_enabled ? '● On' : '○ Off'}
                </span>
              </div>

              {uploadStatus.batcher_enabled ? (
                <>
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-[var(--muted)]">Last batch</span>
                    <span className="text-xs font-mono text-[var(--text)]">{uploadStatus.last_upload_count} logs</span>
                  </div>

                  {uploadStatus.last_upload_at && (
                    <div className="flex items-center justify-between">
                      <span className="text-xs text-[var(--muted)]">At</span>
                      <span className="text-xs font-mono text-[var(--muted)]">
                        {new Date(uploadStatus.last_upload_at).toLocaleTimeString()}
                      </span>
                    </div>
                  )}

                  <div className="flex items-center justify-between">
                    <span className="text-xs text-[var(--muted)]">Pending chunks</span>
                    <span className="text-xs font-mono text-[var(--accent)]">{uploadStatus.pending_count}</span>
                  </div>

                  {uploadStatus.last_upload_key && (
                    <div className="mt-2 pt-3 border-t border-[var(--border)]">
                      <p className="text-[10px] uppercase tracking-wider text-[var(--muted)] mb-1">Last key</p>
                      <p className="font-mono text-[var(--accent)] break-all bg-[var(--bg)] rounded border border-[var(--border)] px-2 py-1.5 text-[10px]">
                        {uploadStatus.last_upload_key}
                      </p>
                    </div>
                  )}
                </>
              ) : (
                <p className="text-xs text-[var(--muted)] pt-1">
                  O3 not configured — logs won't be persisted as chunks.
                </p>
              )}
            </div>
          )}
        </aside>
      </main>
    </div>
  );
}