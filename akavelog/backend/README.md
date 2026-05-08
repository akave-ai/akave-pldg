# Akavelog Backend

Go backend for the Akavelog log-ingestion service. It exposes an HTTP API to manage **inputs** (ingest sources) and to **ingest** log payloads by path.

---

## File structure

```
akavelog/backend/
├── cmd/
│   └── akavelog/
│       └── main.go              # Entrypoint: load config, run migrations, connect DB, start server
├── internal/
│   ├── config/
│   │   ├── config.go            # Config struct, LoadConfig (koanf + .env), Server/Database/Primary types
│   │   └── observability.go     # ObservabilityConfig, New Relic, logging, health checks
│   ├── database/
│   │   ├── database.go          # pgx pool, New(), optional New Relic + zerolog tracing
│   │   ├── migrator.go         # Migrate() – tern migrations via config DSN
│   │   └── migrations/         # Tern SQL: 001_setup.sql, 002_projects.sql, 003_inputs.sql
│   ├── logger/
│   │   └── logger.go           # zerolog + New Relic LoggerService, PgxLogger
│   ├── batcher/
│   │   ├── batcher.go          # Batcher: implements InputBuffer, validates logs, flushes to O3
│   │   └── validator.go        # ValidateLog(): JSON → LogEntry (service, message required)
│   ├── storage/
│   │   └── o3.go               # O3Client: S3-compatible PutObject for Akave O3
│   ├── server/
│   │   ├── server.go           # Echo server, routes, InputHandler, IngestDispatcher, batcher
│   │   └── ingest.go           # IngestDispatcher – routes /ingest/<path> to registered handlers
│   ├── handler/
│   │   └── inputs.go           # InputHandler – CRUD for inputs, list types, mount ingest by path
│   ├── repository/
│   │   └── input.go            # InputRepository – persist inputs (id, type, title, configuration, etc.)
│   ├── model/
│   │   ├── project.go          # Input, InputState (used by inputs API)
│   │   ├── logentry.go         # LogEntry (timestamp, service, level, message, tags)
│   │   └── ...                 # Other domain models (projects, batches, alerts, etc.)
│   ├── infrastructure/
│   │   └── inputs/             # Pluggable input types
│   │       ├── registry.go     # GlobalRegistry, Factory, Create, ListRegistered
│   │       ├── interfaces.go   # MessageInput, InputBuffer, Config, Factory
│   │       ├── buffer.go       # InputBuffer interface
│   │       ├── typeinfo.go     # TypeInfo for API (config spec, etc.)
│   │       ├── global.go       # GlobalRegistry singleton
│   │       └── httpinput/      # Built-in "http" input type
│   │           ├── init.go     # Registers factory in init()
│   │           ├── factory.go  # Factory implementation
│   │           └── input.go    # HTTP ingest handler
│   ├── middleware/             # Auth, recovery, rate limit (for future use)
│   └── pkg/                    # Shared helpers (ids, validator, compression)
├── go.mod
├── go.sum
├── .env                        # Local env (loaded by config; see .env.example)
└── .env.example                # Template for AKAVELOG_* variables
```

---

## Current behaviour

### Startup (main.go)

1. **Config** – `config.LoadConfig()` loads `.env` (if present) then reads `AKAVELOG_*` env vars via koanf into `Config` (Primary, Server, Database, Observability).
2. **Logger** – Zerolog + optional New Relic (`logger.NewLoggerService`, `NewLoggerWithService`).
3. **Migrations** – `database.Migrate(ctx, &log, cfg)` runs tern migrations from `internal/database/migrations/` (001_setup, 002_projects, 003_inputs).
4. **Database** – `database.New(cfg, &log, loggerService)` builds a pgx pool with optional New Relic and pgx-zerolog tracing in local env.
5. **Server** – `server.New(cfg, db.Pool)` creates the Echo app, registers routes, then `srv.Start(ctx)` listens on `Config.Server.Port`.

### HTTP API

- **Input management**
  - `GET /inputs/types` – list registered input type names (e.g. `http`).
  - `GET /inputs/types/:type` – config spec for one type.
  - `GET /inputs/info` – config spec for all types.
  - `GET /inputs` – list saved inputs from DB.
  - `POST /inputs` – create an input (type, title, config, etc.); can mount an ingest path.

- **Ingest**
  - `ANY /ingest/*` – dispatched by path. Each input type can register a handler for a path (e.g. `/ingest/raw`). The **IngestDispatcher** strips `/ingest` and routes the rest to the handler registered for that path.

### Input types (pluggable)

- **Registry** – `inputs.GlobalRegistry` holds factories per type name. Packages like `httpinput` register in `init()`.
- **http** – Built-in type registered in `internal/infrastructure/inputs/httpinput`. Provides an HTTP ingest endpoint; creating an input of type `http` with a `listen` path mounts that path under `/ingest/*`.

### Batcher, validator, and Akave O3

- **Log format** – Ingested payloads should be JSON with required fields `service` and `message`, and optional `timestamp`, `level`, `tags`, `project_id`. See `model.LogEntry`.
- **Validator** – `batcher.ValidateLog(raw)` parses JSON and validates; invalid logs are logged and dropped.
- **Batcher** – When `AKAVELOG_STORAGE.O3` is set, the server uses a **Batcher** as the ingest buffer instead of in-memory only. The batcher:
  - Accepts raw bytes via `Insert([]byte)` (same as `InputBuffer`).
  - Validates each payload; on success appends to the current batch.
  - Flushes when batch size reaches **1000** entries or every **30s** (configurable via `BatcherConfig`).
  - On flush: serializes batch to JSON, gzips it, uploads to Akave O3 with key `logs/<project>/YYYY/MM/DD/<uuid>.json.gz`.
- **O3** – S3-compatible client in `internal/storage/o3.go`. Configure with `AKAVELOG_STORAGE.O3.ENDPOINT`, `BUCKET`, `REGION`, `ACCESS_KEY`, `SECRET_KEY`. If O3 is not configured, the server falls back to an in-memory buffer (no upload). To **verify uploads** (list/download batches), use the [AWS CLI with O3](docs/O3_VERIFY.md); the Akave web UI shows buckets only.

### Config and env

- **.env** – Optional. Loaded at startup by `config.LoadConfig()` (godotenv). Use `.env.example` as a template.
- **Variables** – All config keys are under the `AKAVELOG_` prefix and use dots for nesting, e.g. `AKAVELOG_SERVER.PORT`, `AKAVELOG_DATABASE.HOST`, `AKAVELOG_OBSERVABILITY.NEW_RELIC.LICENSE_KEY` (empty = disabled). Optional: `AKAVELOG_STORAGE.O3.*` for Akave O3 (endpoint, bucket, region, access_key, secret_key).

---

## Getting started

1. **Copy env**  
   `cp .env.example .env` and set at least Database (and optionally Server.Port, Observability).

2. **Run Postgres**  
   Use the connection settings from `.env` (e.g. local Postgres or Docker).

3. **Run the server** (from `akavelog/backend/`)  
   ```bash
   go run ./cmd/akavelog
   ```  
   Server listens on the port in `AKAVELOG_SERVER.PORT` (e.g. `8080`).

4. **Try the API**  
   - `curl http://localhost:8080/inputs/types`  
   - `curl http://localhost:8080/inputs/info`

5. **Demo UI** (optional)  
   From `akavelog/frontend`: `npm install && npm run dev`, then open http://localhost:3000. You can create an HTTP input, send test logs, see incoming logs, and monitor upload status to O3 in the side panel.

---

## Step-By-Step Implementation Plan

> Note: GPT-5.2-Codex was used to generate the initial implementation plan below, which is manually verified by me!!!!

### Phase 1: Foundation

**Step 1: Project Setup**
- Initialize Go module with Echo framework
- Set up folder structure (`cmd/`, `internal/`, `pkg/`)
- Configure environment loading (`koanf`)
- Add structured logging (`zerolog`)

**Step 2: Database Layer**
- Set up PostgreSQL (local Docker)
- Configure `pgx` connection pool
- Write migrations with `tern`:
  - `projects` table
  - `api_keys` table
  - `log_batches` table (metadata index)
  - `alert_rules` + `alert_events` tables



### Phase 2: Ingestion Pipeline

**Step 3: Ingestion Endpoint**
- Create `POST /ingest` handler
- Validate request body (`timestamp`, `service`, `level`, `message`, `tags`)
- API key validation middleware → extract `project_id`

**Step 4: Batcher**
- In-memory buffer per project
- Flush on:
  - Batch size threshold (e.g., `1000` logs)
  - Time threshold (e.g., `30 seconds`)
- Compress batch with `gzip` / `zstd`


### Phase 3: Storage Layer

**Step 5: Akave O3 Integration**
- Initialize AWS SDK v2 with custom endpoint (`https://o3-rc2.akave.xyz`)
- Implement storage adapter:
  - `PutObject` → upload compressed batch
  - `GetObject` → retrieve batch
  - `HeadObject` → verify upload
- Return `o3_object_key` after upload


### Phase 4: Indexing

**Step 6: Metadata Index Write**
- After O3 upload succeeds:
  - Extract metadata: `project_id`, `service`, `ts_start`, `ts_end`, `levels`, `tags`
  - Insert into `log_batches` table with `o3_object_key`
- Ensure atomicity (upload + index write)


### Phase 5: Query Engine

**Step 7: Query Endpoint**
- Create `POST /query` handler
- Parse filters: time range, service, level, keyword

**Step 8: Metadata Lookup**
- Query `log_batches` table for matching `o3_object_key` values
- Filter by project, time, service, level

**Step 9: Batch Fetch + Filter**
- Fetch matching batches from Akave O3 via `GetObject`
- Decompress in memory
- Apply keyword/field filters
- Stream results to client (SSE or chunked JSON)


### Phase 6: Frontend

**Step 10: Next.js Setup**
- Initialize Next.js + TypeScript + TailwindCSS
- Set up API client for backend

**Step 11: Log Explorer UI**
- Time-range picker
- Service/level/tag dropdowns
- Keyword search input
- Streaming log list with infinite scroll
- Log detail panel


### Phase 7: Alerting


**Step 12: Alert Rule CRUD**
- `POST /alerts` → create rule
- `GET /alerts` → list rules
- `DELETE /alerts/:id` → delete rule

**Step 13: Background Worker**
- Run every `60 seconds`
- Fetch enabled rules
- Execute query against metadata index
- Evaluate threshold conditions
- Record `alert_events` and trigger notifications


### Phase 8: Identity

**Step 14: Project + API Key Management**
- `POST /projects` → create project + generate API key
- Middleware validates `X-API-Key` on all requests
- Scope all queries to `project_id`

**Step 15: Production Hardening**
- Rate limiting
- Retry logic for O3 uploads
- Observability (Prometheus metrics, health check)
- Error handling + graceful shutdown




## Resources

**Project Layout** - https://github.com/golang-standards/project-layout

**echo framework** - "https://echo.labstack.com/docs/quick-start"

**pgx - SQL Driver** - https://github.com/jackc/pgx

**tern - SQL Migrator** - https://github.com/jackc/tern

**zerolog - JSON Logger** - https://github.com/rs/zerolog

**newrelic -Monitoring and Observability** - "https://pkg.go.dev/github.com/newrelic/go-agent/v3@v3.40.1/newrelic"

**validator** - https://github.com/go-playground/validator

**koanf - Configuration Management** - https://github.com/knadh/koanf

**testify - for testing** - https://github.com/stretchr/testify

**taskfile** - https://taskfile.dev/

**AsyncQ - queueing tasks and processing them asynchronously with workers** - https://github.com/hibiken/asynq
