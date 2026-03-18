'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import {
  streamLogs,
  type QueryRequest,
  type QueryResultEntry,
  type SSEDoneEvent,
} from '@/lib/api';

const LEVELS = ['error', 'warn', 'info', 'debug', 'fatal', 'trace'];

const LEVEL_COLORS: Record<string, string> = {
  error: 'text-red-400 bg-red-400/10 border-red-400/30',
  fatal: 'text-red-300 bg-red-300/10 border-red-300/30',
  warn:  'text-yellow-400 bg-yellow-400/10 border-yellow-400/30',
  info:  'text-cyan-400 bg-cyan-400/10 border-cyan-400/30',
  debug: 'text-zinc-400 bg-zinc-400/10 border-zinc-400/30',
  trace: 'text-purple-400 bg-purple-400/10 border-purple-400/30',
};

const LEVEL_DOT: Record<string, string> = {
  error: 'bg-red-400',
  fatal: 'bg-red-300',
  warn:  'bg-yellow-400',
  info:  'bg-cyan-400',
  debug: 'bg-zinc-400',
  trace: 'bg-purple-400',
};

/** Rows per page (initial load AND each scroll page). */
const PAGE_SIZE = 50;

function levelStyle(level: string) {
  return LEVEL_COLORS[level.toLowerCase()] ?? 'text-zinc-400 bg-zinc-400/10 border-zinc-400/30';
}
function levelDot(level: string) {
  return LEVEL_DOT[level.toLowerCase()] ?? 'bg-zinc-400';
}

function fmtTimestamp(ts: string): string {
  try {
    const d = new Date(ts);
    const date = d.toLocaleDateString('en-GB', { month: 'short', day: '2-digit' });
    const time = d.toLocaleTimeString('en-GB', { hour12: false });
    const ms = String(d.getMilliseconds()).padStart(3, '0');
    return `${date} ${time}.${ms}`;
  } catch { return ts; }
}

function toRFC3339(localValue: string): string {
  if (!localValue) return '';
  return new Date(localValue).toISOString();
}

function fromRFC3339ToLocal(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

type SearchState = 'idle' | 'streaming' | 'done' | 'error';

interface Filters {
  service: string;
  keyword: string;
  selectedLevels: string[];
  fromTime: string;  // user-set lower bound (optional)
  toTime: string;    // user-set upper bound (optional)
  limit: number;
}

const DEFAULT_FILTERS: Filters = {
  service: '',
  keyword: '',
  selectedLevels: [],
  fromTime: '',
  toTime: '',
  limit: PAGE_SIZE,
};

export default function LogsPage() {
  // ── Filter form ─────────────────────────────────────────────────────────────
  const [filters, setFilters] = useState<Filters>(DEFAULT_FILTERS);

  // ── Results ─────────────────────────────────────────────────────────────────
  const [results, setResults]       = useState<QueryResultEntry[]>([]);
  const [searchState, setSearchState] = useState<SearchState>('idle');
  const [summary, setSummary]        = useState<SSEDoneEvent | null>(null);
  const [streamError, setStreamError] = useState<string | null>(null);
  const [expandedIdx, setExpandedIdx] = useState<number | null>(null);
  /** Whether there are more pages to load (backend said truncated on last page). */
  const [hasMore, setHasMore] = useState(false);

  // ── Refs ─────────────────────────────────────────────────────────────────────
  const abortRef   = useRef<AbortController | null>(null);
  const listRef    = useRef<HTMLDivElement>(null);
  const isFetching = useRef(false); // prevent concurrent fetches

  // ── Helpers ──────────────────────────────────────────────────────────────────
  const patchFilters = (patch: Partial<Filters>) =>
    setFilters(prev => ({ ...prev, ...patch }));

  const toggleLevel = (l: string) =>
    setFilters(prev => ({
      ...prev,
      selectedLevels: prev.selectedLevels.includes(l)
        ? prev.selectedLevels.filter(x => x !== l)
        : [...prev.selectedLevels, l],
    }));

  /**
   * Execute a query.
   *
   * @param filtersSnapshot  The filter values to use (snapshot avoids stale closures).
   * @param append           If true, append results to existing list (scroll page).
   *                         If false, clear and start fresh (new search).
   * @param timeEndOverride  When paginating: the oldest row's timestamp becomes the
   *                         upper bound so we fetch the next batch of older logs.
   */
  const runQuery = useCallback((
    filtersSnapshot: Filters,
    append: boolean,
    timeEndOverride?: string,
  ) => {
    abortRef.current?.abort();
    isFetching.current = true;

    if (!append) {
      setResults([]);
      setSummary(null);
      setStreamError(null);
      setExpandedIdx(null);
      setHasMore(false);
    }

    setSearchState('streaming');

    const req: QueryRequest = {
      service:    filtersSnapshot.service.trim() || undefined,
      keyword:    filtersSnapshot.keyword.trim() || undefined,
      levels:     filtersSnapshot.selectedLevels.length ? filtersSnapshot.selectedLevels : undefined,
      time_start: filtersSnapshot.fromTime ? toRFC3339(filtersSnapshot.fromTime) : undefined,
      // When paginating backward, override the upper time bound with the oldest already-shown ts
      time_end:   timeEndOverride ?? (filtersSnapshot.toTime ? toRFC3339(filtersSnapshot.toTime) : undefined),
      limit:      filtersSnapshot.limit,
    };

    const collected: QueryResultEntry[] = [];

    const ctrl = streamLogs(
      req,
      (entry) => {
        collected.push(entry);
        // Append in arrival order — backend now sends newest first, so this is correct
        setResults(prev => append ? [...prev, ...[] /* batch added below */] : collected.slice());
      },
      (done) => {
        setSummary(done);
        // hasMore: backend says truncated AND we collected a full page
        setHasMore(done.truncated && collected.length >= filtersSnapshot.limit);
        // Flush collected into state as one final update for append mode
        if (append) {
          setResults(prev => [...prev, ...collected]);
        } else {
          setResults(collected.slice());
        }
        setSearchState('done');
        isFetching.current = false;
      },
      (err) => {
        setStreamError(err);
        setSearchState('error');
        isFetching.current = false;
      },
    );

    abortRef.current = ctrl;
  }, []);

  // ── Scroll → load more ───────────────────────────────────────────────────────
  const handleScroll = useCallback(() => {
    const el = listRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 150;

    if (nearBottom && !isFetching.current && hasMore) {
      // Capture state values we need right now via a functional update
      setResults(prev => {
        if (prev.length === 0) return prev;
        // The last element is the oldest row (backend sends newest-first,
        // but we may reverse in UI — use the actual last item in the array
        // which is the oldest in the original stream order).
        const oldest = prev[prev.length - 1];
        // Subtract 1ns to exclude that exact timestamp and avoid duplicates
        const cursor = new Date(new Date(oldest.timestamp).getTime() - 1).toISOString();
        setFilters(currentFilters => {
          runQuery(currentFilters, true, cursor);
          return currentFilters; // no change to filters
        });
        return prev; // no immediate state change
      });
    }
  }, [hasMore, runQuery]);

  // ── Initial auto-load on mount ─────────────────────────────────────────────
  const didMount = useRef(false);
  useEffect(() => {
    if (didMount.current) return;
    didMount.current = true;
    runQuery(DEFAULT_FILTERS, false);
  }, [runQuery]);

  // ── Action handlers ─────────────────────────────────────────────────────────
  const handleSearch = () => runQuery(filters, false);

  const handleClear = () => {
    abortRef.current?.abort();
    isFetching.current = false;
    setResults([]);
    setSummary(null);
    setStreamError(null);
    setSearchState('idle');
    setExpandedIdx(null);
    setHasMore(false);
    setFilters(DEFAULT_FILTERS);
  };

  const handleCancel = () => {
    abortRef.current?.abort();
    isFetching.current = false;
    setSearchState('done');
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') handleSearch();
  };

  // ── Render ──────────────────────────────────────────────────────────────────
  return (
    <div className="h-screen flex flex-col bg-[var(--bg)] overflow-hidden">

      {/* Navigation */}
      <header className="shrink-0 border-b border-[var(--border)] px-4 py-2 flex items-center gap-4 flex-wrap">
        <Link href="/" className="text-[var(--muted)] hover:text-[var(--accent)] text-sm transition-colors">← Home</Link>
        <span className="text-[var(--accent)] font-semibold tracking-wide text-sm">LOG EXPLORER</span>
        <div className="ml-auto flex items-center gap-3 text-xs text-[var(--muted)]">
          <Link href="/uploads" className="hover:text-[var(--accent)] transition-colors">O3 Uploads</Link>
        </div>
      </header>

      {/* Filter bar */}
      <div className="shrink-0 border-b border-[var(--border)] bg-[var(--card)] px-4 py-3 space-y-3">

        {/* Row 1: time range */}
        <div className="flex flex-wrap gap-3 items-center">
          <div className="flex items-center gap-2">
            <label className="text-xs text-[var(--muted)] font-medium uppercase tracking-wider w-10">From</label>
            <input
              type="datetime-local"
              value={filters.fromTime}
              onChange={e => patchFilters({ fromTime: e.target.value })}
              className="rounded bg-[var(--bg)] border border-[var(--border)] px-2 py-1 text-xs font-mono text-[var(--text)] focus:outline-none focus:border-[var(--accent)] transition-colors"
            />
          </div>
          <div className="flex items-center gap-2">
            <label className="text-xs text-[var(--muted)] font-medium uppercase tracking-wider w-4">To</label>
            <input
              type="datetime-local"
              value={filters.toTime}
              onChange={e => patchFilters({ toTime: e.target.value })}
              className="rounded bg-[var(--bg)] border border-[var(--border)] px-2 py-1 text-xs font-mono text-[var(--text)] focus:outline-none focus:border-[var(--accent)] transition-colors"
            />
          </div>
          <div className="flex gap-1">
            {[
              { label: '15m', ms: 15 * 60 * 1000 },
              { label: '1h',  ms: 60 * 60 * 1000 },
              { label: '6h',  ms: 6 * 60 * 60 * 1000 },
              { label: '24h', ms: 24 * 60 * 60 * 1000 },
            ].map(({ label, ms }) => (
              <button
                key={label}
                type="button"
                onClick={() => {
                  const now = new Date();
                  patchFilters({
                    toTime:   fromRFC3339ToLocal(now.toISOString()),
                    fromTime: fromRFC3339ToLocal(new Date(Date.now() - ms).toISOString()),
                  });
                }}
                className="px-2 py-1 rounded text-xs text-[var(--muted)] border border-[var(--border)] hover:border-[var(--accent)] hover:text-[var(--accent)] transition-colors"
              >
                {label}
              </button>
            ))}
            {(filters.fromTime || filters.toTime) && (
              <button
                type="button"
                onClick={() => patchFilters({ fromTime: '', toTime: '' })}
                className="px-2 py-1 rounded text-xs text-[var(--muted)] border border-[var(--border)] hover:border-red-400 hover:text-red-400 transition-colors"
              >
                ✕ time
              </button>
            )}
          </div>
        </div>

        {/* Row 2: service / keyword / levels / limit / actions */}
        <div className="flex flex-wrap gap-3 items-center">
          <input
            type="text"
            value={filters.service}
            onChange={e => patchFilters({ service: e.target.value })}
            onKeyDown={handleKeyDown}
            placeholder="service name…"
            className="rounded bg-[var(--bg)] border border-[var(--border)] px-3 py-1.5 text-xs font-mono text-[var(--text)] placeholder:text-[var(--muted)] focus:outline-none focus:border-[var(--accent)] transition-colors w-44"
          />
          <input
            type="text"
            value={filters.keyword}
            onChange={e => patchFilters({ keyword: e.target.value })}
            onKeyDown={handleKeyDown}
            placeholder="keyword search…"
            className="rounded bg-[var(--bg)] border border-[var(--border)] px-3 py-1.5 text-xs font-mono text-[var(--text)] placeholder:text-[var(--muted)] focus:outline-none focus:border-[var(--accent)] transition-colors w-48"
          />

          {/* Level toggles */}
          <div className="flex gap-1 flex-wrap">
            {LEVELS.map(l => {
              const active = filters.selectedLevels.includes(l);
              return (
                <button
                  key={l}
                  type="button"
                  onClick={() => toggleLevel(l)}
                  className={`px-2 py-1 rounded text-xs font-medium border transition-all ${
                    active ? levelStyle(l) : 'text-[var(--muted)] border-[var(--border)] hover:border-[var(--muted)]'
                  }`}
                >
                  {l}
                </button>
              );
            })}
          </div>

          {/* Limit */}
          <div className="flex items-center gap-1.5">
            <label className="text-xs text-[var(--muted)]">limit</label>
            <input
              type="number"
              value={filters.limit}
              onChange={e => patchFilters({ limit: Math.max(1, Math.min(1000, parseInt(e.target.value) || PAGE_SIZE)) })}
              min={1}
              max={1000}
              className="rounded bg-[var(--bg)] border border-[var(--border)] px-2 py-1 text-xs font-mono text-[var(--text)] focus:outline-none focus:border-[var(--accent)] transition-colors w-20"
            />
          </div>

          {/* Buttons */}
          <div className="flex gap-2 ml-auto">
            <button
              type="button"
              onClick={handleClear}
              className="px-3 py-1.5 rounded text-xs border border-[var(--border)] text-[var(--muted)] hover:text-red-400 hover:border-red-400/50 transition-colors"
            >
              Clear
            </button>
            {searchState === 'streaming' ? (
              <button
                type="button"
                onClick={handleCancel}
                className="px-4 py-1.5 rounded text-xs bg-red-500/20 border border-red-500/40 text-red-400 hover:bg-red-500/30 transition-colors"
              >
                ■ Stop
              </button>
            ) : (
              <button
                type="button"
                onClick={handleSearch}
                className="px-4 py-1.5 rounded text-xs bg-[var(--accent)] text-[var(--bg)] font-semibold hover:opacity-90 transition-opacity"
              >
                Search
              </button>
            )}
          </div>
        </div>
      </div>

      {/* Status bar */}
      <div className="shrink-0 border-b border-[var(--border)] px-4 py-1.5 flex items-center gap-4 text-xs text-[var(--muted)]">
        {searchState === 'streaming' && !hasMore && (
          <span className="flex items-center gap-1.5">
            <span className="inline-block w-1.5 h-1.5 rounded-full bg-[var(--accent)] animate-pulse" />
            loading…
          </span>
        )}
        {searchState === 'streaming' && hasMore && (
          <span className="flex items-center gap-1.5">
            <span className="inline-block w-1.5 h-1.5 rounded-full bg-[var(--accent)] animate-pulse" />
            loading more…
          </span>
        )}
        {searchState === 'done' && summary && (
          <span className="text-[var(--success)]">
            ✓ {summary.count} result{summary.count !== 1 ? 's' : ''}
            {hasMore && <span className="text-[var(--warn)] ml-2">· scroll for more ↓</span>}
          </span>
        )}
        {searchState === 'error' && <span className="text-red-400">✗ {streamError}</span>}
        {searchState === 'idle' && results.length === 0 && (
          <span>Set filters and click Search, or results will load automatically.</span>
        )}
        {results.length > 0 && (
          <span className="ml-auto">{results.length} entries loaded</span>
        )}
      </div>

      {/* Log table */}
      <div
        ref={listRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto font-mono text-xs"
      >
        {results.length === 0 && searchState !== 'streaming' ? (
          <div className="flex flex-col items-center justify-center h-full text-[var(--muted)] gap-2">
            <span className="text-2xl opacity-30">⌕</span>
            <span>No results</span>
          </div>
        ) : (
          <table className="w-full border-collapse">
            <thead className="sticky top-0 bg-[var(--bg)] z-10">
              <tr className="text-[var(--muted)] uppercase text-[10px] tracking-wider border-b border-[var(--border)]">
                <th className="px-3 py-1.5 text-left font-medium w-44">Timestamp</th>
                <th className="px-3 py-1.5 text-left font-medium w-28">Service</th>
                <th className="px-3 py-1.5 text-left font-medium w-16">Level</th>
                <th className="px-3 py-1.5 text-left font-medium">Message</th>
              </tr>
            </thead>
            <tbody>
              {results.map((entry, idx) => {
                const isExpanded = expandedIdx === idx;
                return (
                  <>
                    <tr
                      key={`${entry.ts_ns}-${idx}`}
                      onClick={() => setExpandedIdx(isExpanded ? null : idx)}
                      className={`border-b border-[var(--border)]/40 cursor-pointer transition-colors ${
                        isExpanded ? 'bg-[var(--card)]' : 'hover:bg-[var(--card)]/60'
                      }`}
                    >
                      <td className="px-3 py-1.5 text-[var(--muted)] whitespace-nowrap">{fmtTimestamp(entry.timestamp)}</td>
                      <td className="px-3 py-1.5 text-[var(--warn)] truncate max-w-[7rem]" title={entry.service}>{entry.service}</td>
                      <td className="px-3 py-1.5">
                        <span className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] border font-medium ${levelStyle(entry.level)}`}>
                          <span className={`w-1 h-1 rounded-full ${levelDot(entry.level)}`} />
                          {entry.level.toUpperCase()}
                        </span>
                      </td>
                      <td className="px-3 py-1.5 text-[var(--text)] truncate max-w-0" style={{ maxWidth: 1 }}>
                        <span className="block truncate" title={entry.line}>{entry.line}</span>
                      </td>
                    </tr>

                    {isExpanded && (
                      <tr key={`detail-${idx}`} className="bg-[var(--card)] border-b border-[var(--border)]">
                        <td colSpan={4} className="px-4 py-3">
                          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div className="md:col-span-2">
                              <p className="text-[10px] uppercase tracking-wider text-[var(--muted)] mb-1">Message</p>
                              <pre className="text-xs text-[var(--text)] bg-[var(--bg)] rounded border border-[var(--border)] p-3 whitespace-pre-wrap break-words leading-relaxed">{entry.line}</pre>
                            </div>
                            <div className="space-y-2 text-xs">
                              <p className="text-[10px] uppercase tracking-wider text-[var(--muted)]">Metadata</p>
                              <div className="bg-[var(--bg)] rounded border border-[var(--border)] divide-y divide-[var(--border)]">
                                {[
                                  ['Timestamp', fmtTimestamp(entry.timestamp)],
                                  ['ts_ns',     String(entry.ts_ns)],
                                  ['Service',   entry.service],
                                  ['Level',     entry.level],
                                ].map(([k, v]) => (
                                  <div key={k} className="flex px-3 py-1.5 gap-3">
                                    <span className="text-[var(--muted)] w-20 shrink-0">{k}</span>
                                    <span className="text-[var(--text)] font-mono break-all">{v}</span>
                                  </div>
                                ))}
                              </div>
                            </div>
                            <div className="space-y-2 text-xs">
                              <p className="text-[10px] uppercase tracking-wider text-[var(--muted)]">Labels</p>
                              <div className="bg-[var(--bg)] rounded border border-[var(--border)] divide-y divide-[var(--border)]">
                                {Object.entries(entry.labels).map(([k, v]) => (
                                  <div key={k} className="flex px-3 py-1.5 gap-3">
                                    <span className="text-[var(--muted)] w-20 shrink-0 truncate" title={k}>{k}</span>
                                    <span className="text-[var(--accent)] font-mono break-all">{v}</span>
                                  </div>
                                ))}
                                {Object.keys(entry.labels).length === 0 && (
                                  <div className="px-3 py-1.5 text-[var(--muted)]">—</div>
                                )}
                              </div>
                              <p className="text-[10px] uppercase tracking-wider text-[var(--muted)] mt-2">O3 Object</p>
                              <p className="font-mono text-[var(--accent)] break-all bg-[var(--bg)] rounded border border-[var(--border)] px-3 py-1.5 text-[10px]">{entry.o3_object_key}</p>
                            </div>
                          </div>
                          <button
                            type="button"
                            onClick={e => { e.stopPropagation(); setExpandedIdx(null); }}
                            className="mt-3 text-[10px] text-[var(--muted)] hover:text-[var(--text)] transition-colors"
                          >
                            ▲ collapse
                          </button>
                        </td>
                      </tr>
                    )}
                  </>
                );
              })}
            </tbody>
          </table>
        )}

        {/* Streaming skeleton */}
        {searchState === 'streaming' && (
          <div className="px-3 py-2 space-y-1.5 animate-pulse">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="h-6 rounded bg-[var(--card)] opacity-50" style={{ width: `${55 + (i * 13) % 35}%` }} />
            ))}
          </div>
        )}

        {/* Load more footer */}
        {searchState === 'done' && hasMore && (
          <div className="px-4 py-4 text-center text-xs text-[var(--muted)] border-t border-[var(--border)]/40">
            <span className="opacity-50">↓ scroll to load older logs</span>
          </div>
        )}

        {/* End of results */}
        {searchState === 'done' && !hasMore && results.length > 0 && (
          <div className="px-4 py-3 text-center text-[10px] text-[var(--muted)]/40 border-t border-[var(--border)]/20">
            — end of results —
          </div>
        )}
      </div>
    </div>
  );
}