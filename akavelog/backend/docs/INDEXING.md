# How indexing works (Progress index)

## No database schema

**The index does not use the database.** There are no index tables, no migrations, and no database schema for indexing. Everything is stored in **O3 (object storage)** only.

- **Chunks** → O3 (gzip JSON objects under `chunks/`)
- **Index** → O3 (NDJSON files under `index/`)

So the “index schema” is just the **key layout and file format in O3**, not a DB schema.

---

## Where the index is stored

**Storage:** Akave O3 (S3-compatible), same bucket as chunks.

**Location in the bucket:**

```
index/<tenant>/<date>/<batchID>.ndjson
```

| Part      | Example        | Meaning                                      |
|-----------|----------------|----------------------------------------------|
| `index/`  | prefix         | All index objects live under this prefix.   |
| `<tenant>`| `default`      | Tenant ID (e.g. `default`).                 |
| `<date>`  | `2026-02-26`   | UTC date when the batch was written (YYYY-MM-DD). |
| `<batchID>.ndjson` | `a1b2c3d4-....ndjson` | Unique batch ID (UUID). One NDJSON file per batch. |

**Example full key:**

```
index/default/2026-02-26/a1b2c3d4-e5f6-7890-abcd-ef1234567890.ndjson
```

You can list index objects with the same O3/S3 tools you use for chunks, using the prefix `index/` (or `index/default/` for one tenant).

---

## Index “schema” (structure of index data)

There is no database schema. The structure is:

1. **Object key** in O3: `index/<tenant>/<date>/<batchID>.ndjson` (see above).
2. **File content:** NDJSON = one JSON object per line, no outer array.
3. **One line (one index entry)** has this shape:

```json
{"tenant":"default","stream_id":"3f3b0b5ec25edcbc","from_ns":1772125352475005440,"to_ns":1772125352475015439,"chunk_key":"chunks/default/3f3b0b5ec25edcbc/1772125352475005440_1772125352475015439.json.gz"}
```

| Field       | Type   | Meaning |
|------------|--------|--------|
| `tenant`   | string | Tenant ID. |
| `stream_id`| string | 16-char hex FNV hash of stream labels (same as in chunk path). |
| `from_ns`  | number | Chunk time range start (Unix nanoseconds). |
| `to_ns`    | number | Chunk time range end (Unix nanoseconds). |
| `chunk_key`| string | Full O3 key of the chunk object (under `chunks/`). |

So the “index schema” is: **one NDJSON file per batch, each line = one chunk reference** with those five fields. No DB columns or tables.

---

## How indexing works (flow)

1. **Ingester flushes a chunk**  
   When a chunk is full (or idle/old), the ingester writes it to O3 via the chunk store and gets the chunk key (e.g. `chunks/default/<streamID>/<from>_<to>.json.gz`).

2. **Index write**  
   The ingester then calls the index writer:  
   `IndexChunk(ctx, tenant, streamID, fromNs, toNs, chunkKey)`.  
   So **every chunk that is written to O3 gets one index entry**.

3. **Index writer (in memory → O3)**  
   - The index writer **buffers** these entries in memory (no database).
   - When the buffer reaches **100 entries** (or on a **30s** timer, or on shutdown), it writes **one NDJSON file** to O3: key `index/<tenant>/<date>/<uuid>.ndjson`, body = one JSON line per buffered entry.
   - So the index is **only** stored in O3; nothing is written to the DB.

4. **Query path (read)**  
   To find chunks for a stream and time range:
   - List O3 objects under `index/<tenant>/` for the relevant dates (from query time range).
   - Download each NDJSON file, parse line by line.
   - Keep entries where `stream_id` matches and `[from_ns, to_ns]` overlaps the query range.
   - Collect the `chunk_key` values; those are the chunk objects to read from O3.

So: **index storage = O3 only. Index “schema” = key layout + NDJSON line format above. No indexing schema or tables in the database.**

---

## Code references

| What            | Where |
|-----------------|--------|
| Index entry shape | `internal/index/types.go` – `Entry` struct |
| O3 key layout   | `internal/index/keys.go` – `KeyForIndexBatch`, `PrefixForTenantDate` |
| Write (buffer + flush to O3) | `internal/index/writer.go` – `O3Writer` |
| Read (list + parse NDJSON) | `internal/index/reader.go` – `Reader.ListChunkKeys` |
| Ingester calls writer after chunk flush | `internal/ingester/ingester.go` – after `store.Put`, `indexWriter.IndexChunk(...)` |

---

## Summary

- **No database schema for indexing** – no new tables, no migrations.
- **Index is stored only in O3** under the prefix `index/<tenant>/<date>/<batchID>.ndjson`.
- **Index “schema”** = that key convention + one NDJSON line per chunk with `tenant`, `stream_id`, `from_ns`, `to_ns`, `chunk_key`.
