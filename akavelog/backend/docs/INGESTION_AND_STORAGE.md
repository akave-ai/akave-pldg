# Akavelog: Ingestion flow and O3 storage

## 1. How ingestion works (high level)

- **Client** sends one HTTP `POST /akavelog/api/v1/push` with a body = one push request (many streams, each with many log entries).
- **Distributor** parses and validates the request (JSON, optional gzip/deflate), then forwards the whole request to the **ingester** (single-node; no ring).
- **Ingester** receives the push and appends entries to in-memory **streams** and **chunks**. When a chunk is closed (full, old, idle), it is flushed to the **store** (O3).

So: **one HTTP request → one logical push → append in memory → flush by chunk**.

## 2. Where batching happens

| Where | What happens | Batcher? |
|-------|--------------|----------|
| **Client** | Buffers lines and sends one HTTP request per batch (size/time). | Yes – main “ingestion batcher” for combining lines. |
| **Distributor** | One HTTP request → validate → forward to ingester. No merging of multiple HTTP requests. | No cross-request batching. |
| **Ingester** | Append entries to in-memory chunks per stream; flush **one chunk at a time** to O3. | Chunk = batch of entries written together to O3. |

There is no server-side component that merges different HTTP pushes; each push is handled on its own. The only batching on the server is **chunk batching**: many log lines accumulated in memory per stream, then written as one object per chunk to O3.

### Code references

- **Distributor:** `internal/distributor/distributor.go` – `Push(ctx, req)` forwards to ingester.
- **Distributor HTTP:** `internal/distributor/http.go` – parse body, validate, call `Push`, then optional `onLog` per entry.
- **Ingester:** `internal/ingester/ingester.go` – `Push`, `flushQueue`, `flushLoop`.
- **Stream / chunk:** `internal/ingester/stream.go` – stream has current chunk; `FlushCurrentIfNeeded` closes chunk and enqueues flush.
- **Flush:** `internal/ingester/ingester.go` – `flushLoop` dequeues one chunk, encodes it, calls `store.Put(ctx, []Chunk{ch})`.

## 3. O3 storage: chunk format and file names

Chunks are stored as below. Indexing uses **Progress**, not TSDB.

### Chunks (log data)

| Aspect | Akavelog |
|--------|----------|
| **Storage format** | **Gzip-compressed JSON** (not binary). |
| **Content** | One JSON object per chunk: `{"labels":{...},"entries":[{"ts_ns":...,"line":"..."},...]}`. Then gzip. |
| **Content-Type** | `application/gzip`. |

**Object key (file name) in O3:**

```
chunks/<tenant>/<streamID>/<fromNs>_<toNs>.json.gz
```

- **tenant** – tenant ID (e.g. `default`).
- **streamID** – 16-character hex FNV-64a hash of sorted stream labels (e.g. `job=demo,app=myapp` → `a1b2c3d4e5f60001`). From `chunk.StreamID(labels)` in `internal/chunk/o3.go`.
- **fromNs**, **toNs** – Unix nanoseconds of the chunk’s time range, as decimal strings.

**Example key:**

```
chunks/default/a1b2c3d4e5f60001/1739123456000000000_1739123460000000000.json.gz
```

**Code:**

- Key: `internal/chunk/store.go` – `KeyForChunk(tenant, streamID, fromNs, toNs)`.
- Stream ID: `internal/chunk/o3.go` – `StreamID(labels)` (FNV-64a of sorted `k=v` pairs).
- Encode + upload: `internal/chunk/o3.go` – `O3Store.Put` (JSON marshal → gzip → `PutObject`).

### Progress index (not TSDB)

We use a **Progress-style index** (append-only, period-based index in O3).

**Role:** Record which chunk keys exist for which stream and time range so the query path can resolve “stream X, time [A,B]” → list of chunk keys to read from O3.

**Storage format:** NDJSON (one JSON object per line). Each line is an index entry:

```json
{"tenant":"default","stream_id":"a1b2c3d4e5f60001","from_ns":1739123456000000000,"to_ns":1739123460000000000,"chunk_key":"chunks/default/a1b2c3d4e5f60001/1739123456000000000_1739123460000000000.json.gz"}
```

**Object key (file name) in O3:**

```
index/<tenant>/<date>/<batchID>.ndjson
```

- **tenant** – tenant ID (e.g. `default`).
- **date** – UTC date of the batch write, `2006-01-02`.
- **batchID** – UUID for this batch (one NDJSON file per batch; no append, so one write per file).

**Example key:**

```
index/default/2026-02-26/a1b2c3d4-e5f6-7890-abcd-ef1234567890.ndjson
```

**Write path:** When the ingester flushes a chunk to O3, it calls `index.Writer.IndexChunk(tenant, streamID, fromNs, toNs, chunkKey)`. The index writer buffers entries and periodically (or when the buffer is full) writes one NDJSON file per tenant to O3. So chunks and index are written in sync; index is **not** TSDB.

**Read path:** `index.Reader.ListChunkKeys(ctx, tenant, streamID, fromNs, toNs)` lists index objects under `index/<tenant>/<date>/` for the date range that covers `[fromNs, toNs]`, reads each NDJSON file, filters by `stream_id` and overlapping time range, and returns chunk keys. The query layer can then `GetObject` those chunk keys from O3.

**Code:**

- Types: `internal/index/types.go` – `Entry` (tenant, stream_id, from_ns, to_ns, chunk_key).
- Keys: `internal/index/keys.go` – `KeyForIndexBatch`, `PrefixForTenantDate`, `PrefixForTenant`.
- Writer: `internal/index/writer.go` – `O3Writer` (buffer, flush to NDJSON per tenant, optional background flush loop).
- Reader: `internal/index/reader.go` – `Reader.ListChunkKeys(tenant, streamID, fromNs, toNs)`.
- API: `GET /index/chunks?tenant=default&stream_id=...&from_ns=...&to_ns=...` returns `{ "chunks": [ { "chunk_key": "...", "from_ns": ..., "to_ns": ... } ] }`.

## 4. Short summary

- **Ingestion:** One HTTP push at a time; distributor does not batch across requests; ingester batches into in-memory chunks and flushes **one chunk at a time** to O3; each flushed chunk is also recorded in the Progress index.
- **Chunk format in O3:** Gzip-compressed JSON. Key: `chunks/<tenant>/<streamID>/<fromNs>_<toNs>.json.gz`.
- **Index:** Progress index (not TSDB): NDJSON files in O3 at `index/<tenant>/<date>/<batchID>.ndjson`; used to resolve (tenant, stream_id, time range) → chunk keys for querying.
