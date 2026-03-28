'use client';

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { getUploadContent, getUploadRaw, getUploads, type O3ObjectInfo, type StoredLogEntry } from '@/lib/api';

export default function UploadsPage() {
  const [objects, setObjects] = useState<O3ObjectInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [prefix, setPrefix] = useState('chunks/');
  const [storedLogs, setStoredLogs] = useState<StoredLogEntry[]>([]);
  const [logsLoading, setLogsLoading] = useState(false);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [loadAllLogs, setLoadAllLogs] = useState(false);
  const [logSearch, setLogSearch] = useState('');
  const [levelFilter, setLevelFilter] = useState<'all' | 'info' | 'warn' | 'error' | 'debug'>('all');
  const [expandedLogIndex, setExpandedLogIndex] = useState<number | null>(null);
  const [rawExpandedLogIndex, setRawExpandedLogIndex] = useState<number | null>(null);

  // Raw view state
  const [expandedKey, setExpandedKey] = useState<string | null>(null);
  const [rawContent, setRawContent] = useState<string | null>(null);
  const [rawLoading, setRawLoading] = useState(false);

  const loadUploads = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const { objects: list } = await getUploads(prefix || undefined);
      setObjects(list.reverse());
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load uploads');
      setObjects([]);
    } finally {
      setLoading(false);
    }
  }, [prefix]);

  useEffect(() => {
    loadUploads();
  }, [loadUploads]);

  const loadLogsForKey = useCallback(async (key: string) => {
    setLogsLoading(true);
    setError(null);
    try {
      const { logs } = await getUploadContent(key);
      setStoredLogs(logs);
      setSelectedKey(key);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load logs');
      setStoredLogs([]);
    } finally {
      setLogsLoading(false);
    }
  }, []);

  const loadAllStoredLogs = useCallback(async () => {
    if (objects.length === 0) return;
    setLoadAllLogs(true);
    setLogsLoading(true);
    setError(null);
    try {
      const all: StoredLogEntry[] = [];
      const toFetch = objects.slice(0, 20);
      for (const obj of toFetch) {
        const { logs } = await getUploadContent(obj.key);
        all.push(...logs);
      }
      setStoredLogs(all);
      setSelectedKey(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load logs');
      setStoredLogs([]);
    } finally {
      setLogsLoading(false);
      setLoadAllLogs(false);
    }
  }, [objects]);

  const toggleExpand = useCallback(async (key: string) => {
    if (expandedKey === key) {
      setExpandedKey(null);
      setRawContent(null);
      return;
    }
    setExpandedKey(key);
    setRawContent(null);
    setRawLoading(true);
    try {
      const res = await getUploadRaw(key);
      setRawContent(res.content);
    } catch (e) {
      setRawContent((e instanceof Error ? e.message : 'Failed to load raw content') + '\n');
    } finally {
      setRawLoading(false);
    }
  }, [expandedKey]);

  const formatSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  const filteredLogs = useMemo(() => {
    const q = logSearch.trim().toLowerCase();
    return storedLogs.filter((log) => {
      const levelOk = levelFilter === 'all' || (log.level || '').toLowerCase() === levelFilter;
      if (!levelOk) return false;
      if (!q) return true;
      const haystack = [
        log.timestamp || '',
        log.service || '',
        log.level || '',
        log.message || '',
        log.raw_request?.method || '',
        log.raw_request?.path || '',
        log.raw_request?.query || '',
        JSON.stringify(log.tags || {}),
      ]
        .join(' ')
        .toLowerCase();
      return haystack.includes(q);
    });
  }, [storedLogs, logSearch, levelFilter]);

  const copyLogJson = async (log: StoredLogEntry) => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(log, null, 2));
    } catch {
      setError('Failed to copy JSON');
    }
  };

  return (
    <div className="min-h-screen flex flex-col p-4 bg-[var(--bg)]">
      <header className="border-b border-[var(--border)] pb-4 mb-4">
        <div className="flex items-center gap-4 flex-wrap">
          <Link
            href="/"
            className="text-sm text-[var(--muted)] hover:text-[var(--accent)]"
          >
            ← Inserter
          </Link>
          <h1 className="text-xl font-semibold text-[var(--accent)]">
            Uploads to O3
          </h1>
        </div>
        <p className="text-sm text-[var(--muted)] mt-1">
          Log batches in O3. View logs from a batch, see raw O3 data, or load all stored logs.
        </p>
      </header>

      {error && (
        <div className="rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 px-3 py-2 text-sm mb-4">
          {error}
        </div>
      )}

      <div className="flex gap-2 items-center mb-4 flex-wrap">
        <label className="text-sm text-[var(--muted)]">Prefix</label>
        <input
          type="text"
          value={prefix}
          onChange={(e) => setPrefix(e.target.value)}
          placeholder="chunks/"
          className="rounded-lg bg-[var(--card)] border border-[var(--border)] px-3 py-2 text-sm w-48 font-mono"
        />
        <button
          type="button"
          onClick={loadUploads}
          disabled={loading}
          className="rounded-lg bg-[var(--accent)] text-[var(--bg)] px-4 py-2 text-sm font-medium disabled:opacity-50"
        >
          {loading ? 'Loading…' : 'Refresh'}
        </button>
        {objects.length > 0 && (
          <button
            type="button"
            onClick={loadAllStoredLogs}
            disabled={logsLoading || loadAllLogs}
            className="rounded-lg bg-[var(--border)] hover:bg-[var(--muted)] px-4 py-2 text-sm disabled:opacity-50"
          >
            {loadAllLogs ? 'Loading…' : 'Load all stored logs (last 20 batches)'}
          </button>
        )}
      </div>

      <section className="rounded-xl bg-[var(--card)] border border-[var(--border)] flex-1 min-h-0 flex flex-col overflow-hidden mb-4">
        <h2 className="text-sm font-medium text-[var(--muted)] p-4 pb-0">
          Batch files in O3
        </h2>
        {loading && objects.length === 0 ? (
          <p className="p-4 text-sm text-[var(--muted)]">Loading…</p>
        ) : objects.length === 0 ? (
          <p className="p-4 text-sm text-[var(--muted)]">
            No objects found. Ensure O3 is configured and the batcher has flushed
            some logs.
          </p>
        ) : (
          <div className="overflow-auto p-4">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-[var(--muted)] border-b border-[var(--border)]">
                  <th className="w-8 pb-2 pr-2" aria-label="Expand" />
                  <th className="pb-2 pr-4 font-medium">Key</th>
                  <th className="pb-2 pr-4 font-medium">Size</th>
                  <th className="pb-2 pr-4 font-medium">Last modified</th>
                  <th className="pb-2 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {objects.map((obj) => {
                  const isExpanded = expandedKey === obj.key;
                  return (
                    <React.Fragment key={obj.key}>
                      <tr className="border-b border-[var(--border)]/50 hover:bg-[var(--border)]/20">
                        <td className="py-2 pr-2">
                          <button
                            type="button"
                            onClick={() => toggleExpand(obj.key)}
                            disabled={rawLoading && expandedKey !== obj.key}
                            className="p-1 rounded text-[var(--muted)] hover:bg-[var(--border)] disabled:opacity-50"
                            aria-expanded={isExpanded}
                            title={isExpanded ? 'Collapse' : 'View raw object'}
                          >
                            <span className="inline-block transition-transform" style={{ transform: isExpanded ? 'rotate(90deg)' : 'none' }}>
                              ▶
                            </span>
                          </button>
                        </td>
                        <td className="py-2 pr-4 font-mono text-[var(--accent)] break-all">
                          {obj.key}
                        </td>
                        <td className="py-2 pr-4 text-[var(--muted)]">
                          {formatSize(obj.size)}
                        </td>
                        <td className="py-2 pr-4 text-[var(--muted)]">
                          {obj.last_modified
                            ? new Date(obj.last_modified).toLocaleString()
                            : '—'}
                        </td>
                        <td className="py-2">
                          <button
                            type="button"
                            onClick={() => loadLogsForKey(obj.key)}
                            disabled={logsLoading}
                            className="text-xs text-[var(--accent)] hover:underline disabled:opacity-50"
                          >
                            View logs
                          </button>
                        </td>
                      </tr>
                      {isExpanded && (
                        <tr className="border-b border-[var(--border)]/50 bg-[var(--bg)]">
                          <td colSpan={5} className="p-0">
                            <div className="px-12 pb-4 pt-1">
                              {rawLoading ? (
                                <p className="text-sm text-[var(--muted)]">Loading raw content…</p>
                              ) : (
                                <pre className="text-xs font-mono overflow-auto max-h-[400px] p-4 rounded-lg bg-[#0e0e0e] border border-[var(--border)] whitespace-pre-wrap break-words text-[var(--text)]">
                                  {rawContent ?? ''}
                                </pre>
                              )}
                            </div>
                          </td>
                        </tr>
                      )}
                    </React.Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="rounded-xl bg-[var(--card)] border border-[var(--border)] flex-1 min-h-[200px] flex flex-col overflow-hidden">
        <h2 className="text-sm font-medium text-[var(--muted)] p-4 pb-0">
          Stored logs
          {selectedKey && (
            <span className="ml-2 font-mono text-xs opacity-80">
              from {selectedKey.split('/').pop()}
            </span>
          )}
        </h2>
        <div className="flex items-center gap-2 px-4 py-3 border-b border-[var(--border)] flex-wrap">
          <input
            type="text"
            value={logSearch}
            onChange={(e) => setLogSearch(e.target.value)}
            placeholder="Search logs..."
            className="rounded-lg bg-[var(--bg)] border border-[var(--border)] px-3 py-2 text-sm w-56"
          />
          <select
            value={levelFilter}
            onChange={(e) => setLevelFilter(e.target.value as 'all' | 'info' | 'warn' | 'error' | 'debug')}
            className="rounded-lg bg-[var(--bg)] border border-[var(--border)] px-3 py-2 text-sm"
          >
            <option value="all">All levels</option>
            <option value="debug">debug</option>
            <option value="info">info</option>
            <option value="warn">warn</option>
            <option value="error">error</option>
          </select>
          <span className="text-xs text-[var(--muted)]">
            {filteredLogs.length} / {storedLogs.length}
          </span>
        </div>
        {logsLoading && storedLogs.length === 0 ? (
          <p className="p-4 text-sm text-[var(--muted)]">Loading logs…</p>
        ) : storedLogs.length === 0 ? (
          <p className="p-4 text-sm text-[var(--muted)]">
            Click &quot;View logs&quot; on a batch or &quot;Load all stored logs&quot; to see log entries.
          </p>
        ) : filteredLogs.length === 0 ? (
          <p className="p-4 text-sm text-[var(--muted)]">
            No logs match current filters.
          </p>
        ) : (
          <div className="overflow-auto p-4 font-mono text-xs">
            <ul className="space-y-1">
              {filteredLogs.map((log, i) => (
                <li
                  key={i}
                  className="border-b border-[var(--border)]/50 pb-1 break-words"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <span className="text-[var(--muted)]">
                        {log.timestamp ? new Date(log.timestamp).toLocaleString() : '—'}
                      </span>
                      {' '}
                      <span className="text-[var(--warn)]">{log.service ?? '—'}</span>
                      {' '}
                      <span className="text-[var(--accent)]">{log.level ?? '—'}</span>
                      {' '}
                      {log.message}
                      {log.raw_request && (
                        <span className="text-[var(--muted)]">
                          {' '}
                          [{log.raw_request.method} {log.raw_request.path}]
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <button
                        type="button"
                        onClick={() => setExpandedLogIndex(expandedLogIndex === i ? null : i)}
                        className="text-[var(--muted)] hover:text-[var(--accent)]"
                      >
                        {expandedLogIndex === i ? 'Collapse' : 'Expand'}
                      </button>
                      {log.raw_request && (
                        <button
                          type="button"
                          onClick={() =>
                            setRawExpandedLogIndex(rawExpandedLogIndex === i ? null : i)
                          }
                          className="text-[var(--muted)] hover:text-[var(--accent)]"
                        >
                          {rawExpandedLogIndex === i ? 'Hide raw log' : 'Show raw log'}
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => copyLogJson(log)}
                        className="text-[var(--muted)] hover:text-[var(--accent)]"
                      >
                        Copy JSON
                      </button>
                    </div>
                  </div>
                  {expandedLogIndex === i && (
                    <pre className="mt-2 p-3 rounded-lg bg-[#0e0e0e] border border-[var(--border)] text-[var(--text)] overflow-auto whitespace-pre-wrap">
                      {JSON.stringify(log, null, 2)}
                    </pre>
                  )}
                  {rawExpandedLogIndex === i && log.raw_request && (
                    <pre className="mt-2 p-3 rounded-lg bg-[#0e0e0e] border border-[var(--border)] text-[var(--text)] overflow-auto whitespace-pre-wrap">
                      {JSON.stringify(log.raw_request, null, 2)}
                    </pre>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>
    </div>
  );
}
