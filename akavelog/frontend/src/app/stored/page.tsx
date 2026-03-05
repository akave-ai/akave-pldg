'use client';

import React, { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { getUploadRaw, getUploads, type O3ObjectInfo } from '@/lib/api';

type Section = 'chunks' | 'index';

export default function StoredDataPage() {
  const [chunks, setChunks] = useState<O3ObjectInfo[]>([]);
  const [indexObjects, setIndexObjects] = useState<O3ObjectInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expandedKey, setExpandedKey] = useState<string | null>(null);
  const [rawContent, setRawContent] = useState<string | null>(null);
  const [rawLoading, setRawLoading] = useState(false);

  const loadAll = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [chunkRes, indexRes] = await Promise.all([
        getUploads('chunks/'),
        getUploads('index/'),
      ]);
      setChunks(chunkRes.objects);
      setIndexObjects(indexRes.objects);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load stored data');
      setChunks([]);
      setIndexObjects([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadAll();
  }, [loadAll]);

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

  const renderTable = (objects: O3ObjectInfo[], section: Section) => (
    <div className="overflow-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-[var(--muted)] border-b border-[var(--border)]">
            <th className="w-8 pb-2 pr-2" aria-label="Expand" />
            <th className="pb-2 pr-4 font-medium">Key</th>
            <th className="pb-2 pr-4 font-medium">Size</th>
            <th className="pb-2 font-medium">Last modified</th>
          </tr>
        </thead>
        <tbody>
          {objects.map((obj) => {
            const isExpanded = expandedKey === obj.key;
            return (
              <React.Fragment key={obj.key}>
                <tr
                  className="border-b border-[var(--border)]/50 hover:bg-[var(--border)]/20"
                >
                  <td className="py-2 pr-2">
                    <button
                      type="button"
                      onClick={() => toggleExpand(obj.key)}
                      disabled={rawLoading && expandedKey !== obj.key}
                      className="p-1 rounded text-[var(--muted)] hover:bg-[var(--border)] disabled:opacity-50"
                      aria-expanded={isExpanded}
                      title={isExpanded ? 'Collapse' : 'Expand raw data'}
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
                  <td className="py-2 text-[var(--muted)]">
                    {obj.last_modified
                      ? new Date(obj.last_modified).toLocaleString()
                      : '—'}
                  </td>
                </tr>
                {isExpanded && (
                  <tr className="border-b border-[var(--border)]/50 bg-[var(--bg)]">
                    <td colSpan={4} className="p-0">
                      <div className="px-4 pb-4 pt-1">
                        {rawLoading ? (
                          <p className="text-sm text-[var(--muted)]">Loading raw content…</p>
                        ) : (
                          <pre className="text-xs font-mono overflow-auto max-h-[400px] p-3 rounded-lg bg-[var(--card)] border border-[var(--border)] whitespace-pre-wrap break-words">
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
  );

  return (
    <div className="min-h-screen flex flex-col p-4 bg-[var(--bg)]">
      <header className="border-b border-[var(--border)] pb-4 mb-4">
        <div className="flex items-center gap-4 flex-wrap">
          <Link
            href="/"
            className="text-sm text-[var(--muted)] hover:text-[var(--accent)]"
          >
            ← Demo
          </Link>
          <h1 className="text-xl font-semibold text-[var(--accent)]">
            Stored data
          </h1>
        </div>
        <p className="text-sm text-[var(--muted)] mt-1">
          All data in O3 (chunks and index). Expand a row to see raw content. Read-only.
        </p>
      </header>

      {error && (
        <div className="rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 px-3 py-2 text-sm mb-4">
          {error}
        </div>
      )}

      <div className="flex gap-2 items-center mb-4">
        <button
          type="button"
          onClick={loadAll}
          disabled={loading}
          className="rounded-lg bg-[var(--accent)] text-[var(--bg)] px-4 py-2 text-sm font-medium disabled:opacity-50"
        >
          {loading ? 'Loading…' : 'Refresh'}
        </button>
      </div>

      <section className="rounded-xl bg-[var(--card)] border border-[var(--border)] flex-1 min-h-0 flex flex-col overflow-hidden mb-6">
        <h2 className="text-sm font-medium text-[var(--muted)] p-4 pb-2">
          Chunks
        </h2>
        {loading && chunks.length === 0 ? (
          <p className="p-4 text-sm text-[var(--muted)]">Loading…</p>
        ) : chunks.length === 0 ? (
          <p className="p-4 text-sm text-[var(--muted)]">No chunks found.</p>
        ) : (
          renderTable(chunks, 'chunks')
        )}
      </section>

      <section className="rounded-xl bg-[var(--card)] border border-[var(--border)] flex-1 min-h-0 flex flex-col overflow-hidden">
        <h2 className="text-sm font-medium text-[var(--muted)] p-4 pb-2">
          Index
        </h2>
        {loading && indexObjects.length === 0 ? (
          <p className="p-4 text-sm text-[var(--muted)]">Loading…</p>
        ) : indexObjects.length === 0 ? (
          <p className="p-4 text-sm text-[var(--muted)]">No index files found.</p>
        ) : (
          renderTable(indexObjects, 'index')
        )}
      </section>
    </div>
  );
}
