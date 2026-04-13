# py-akavelink

Async job gateway for Akave decentralized storage. Fire-and-forget API — submit heavy operations (bucket create/delete, file upload/delete), get a `job_id` back instantly, poll for results.

---

## 1. Setup

```bash
# Add your Akave private key
echo "AKAVE_PRIVATE_KEY=your_key_here" > .env

# Build and start all services
docker-compose up --build
```

Four containers start: **postgres**, **redis**, **api** (port 8000), **worker** (Celery).

---

## 2. Verify everything is up

```bash
curl http://localhost:8000/health
# {"status":"healthy","database":"connected"}
```

---

## 3. Make requests

### Option A — FastAPI interactive docs (easiest to explore)
Open **http://localhost:8000/docs** in your browser. Every endpoint is listed with a "Try it out" button. (Recommended!)

### Option B — curl (shown below for all endpoints)


## 4. Endpoints

### Bucket operations

```bash
# Create bucket (async — returns job_id)
curl -X POST http://localhost:8000/buckets \
  -H "Content-Type: application/json" \
  -d '{"bucket_name": "my-bucket"}'

# Delete bucket (async — returns job_id)
curl -X POST http://localhost:8000/buckets/delete \
  -H "Content-Type: application/json" \
  -d '{"bucket_name": "my-bucket"}'

# Poll bucket job status
curl http://localhost:8000/buckets/jobs/<job_id>

# List all completed buckets
curl http://localhost:8000/buckets
```

### File operations

```bash
# Upload file (async — returns job_id)
curl -X POST http://localhost:8000/files/upload \
  -F "bucket_name=my-bucket" \
  -F "file=@/path/to/your/file.txt"

# Delete file (async — returns job_id)
curl -X POST http://localhost:8000/files/delete \
  -H "Content-Type: application/json" \
  -d '{"bucket_name": "my-bucket", "file_name": "file.txt"}'

# Poll file job status
curl http://localhost:8000/files/jobs/<job_id>

# Download file (direct stream — no job_id needed)
curl -O http://localhost:8000/files/my-bucket/file.txt/download
```

### Monitoring

```bash
# Count dashboard — how many jobs in each state
curl http://localhost:8000/jobs/summary

# Active jobs — see what's currently running with full details
curl http://localhost:8000/jobs/active
```

`/jobs/summary` example:
```json
{
  "bucket_jobs": { "queued": 0, "processing": 1, "completed": 14, "failed": 0 },
  "file_jobs":   { "queued": 2, "processing": 0, "completed": 8,  "failed": 1 },
  "active_jobs": 3,
  "total_completed": 22,
  "total_failed": 1
}
```

`/jobs/active` example:
```json
{
  "active_count": 2,
  "bucket_jobs": [
    {
      "job_id": "f47ac10b-...",
      "operation": "create",
      "bucket_name": "my-bucket",
      "status": "processing",
      "queued_at": "2026-04-11T17:00:01"
    }
  ],
  "file_jobs": [
    {
      "job_id": "a1b2c3d4-...",
      "operation": "upload",
      "bucket_name": "my-bucket",
      "file_name": "data.csv",
      "status": "queued",
      "queued_at": "2026-04-11T17:00:03"
    }
  ]
}
```

---

## 5. Typical workflow

```bash
# 1. Create a bucket
RESP=$(curl -s -X POST http://localhost:8000/buckets \
  -H "Content-Type: application/json" \
  -d '{"bucket_name": "demo-bucket"}')
JOB=$(echo $RESP | python3 -c "import sys,json; print(json.load(sys.stdin)['job_id'])")

# 2. Poll until completed
watch -n 3 "curl -s http://localhost:8000/buckets/jobs/$JOB | python3 -m json.tool"

# 3. Upload a file once bucket is ready
curl -X POST http://localhost:8000/files/upload \
  -F "bucket_name=demo-bucket" \
  -F "file=@./mydata.csv"
```

---

## 6. Logs & debugging

```bash
docker-compose logs -f api       # API startup errors, request logs
docker-compose logs -f worker    # Celery task processing, SDK calls
docker-compose logs -f           # All services together

docker-compose restart worker    # If worker gets stuck
docker-compose down -v           # Nuclear reset (wipes DB volume too)
```

---

## Architecture

```
Client
  │
  ▼
FastAPI (api.py)          — validates request, inserts job row, returns job_id
  │
  ├── PostgreSQL           — stores job state (queued → processing → completed/failed)
  │
  └── Redis                — message broker (task queue)
        │
        ▼
      Celery Worker (worker.py)
        │
        └── Akave Python SDK → Akave blockchain / storage network
              (writes result back to PostgreSQL on completion)
```

**Why async jobs?** Akave operations involve blockchain transactions — they take seconds to tens of seconds. The job queue pattern means your HTTP response is always instant (<200ms) and clients poll for results.

---