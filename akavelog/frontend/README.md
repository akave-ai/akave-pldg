# Akavelog Demo UI

Next.js demo UI to create HTTP inputs, view incoming logs, and monitor uploads to Akave O3.

## Run

1. **Start the backend** (from `akavelog/backend`):
   ```bash
   go run ./cmd/akavelog
   ```
   Backend must listen on `http://localhost:8080`.

2. **Start the frontend** (from `akavelog/frontend`):
   ```bash
   npm install
   npm run dev
   ```
   Open [http://localhost:3000](http://localhost:3000).

## What the UI does

1. **Create HTTP input** – Form to create an input of type `http` with a title and path (e.g. `raw` → `/ingest/raw`).
2. **Your inputs** – List of created inputs with a “Send test log” button to POST a sample log to that input’s path.
3. **Incoming logs** – Last 200 ingested log entries (polled every 2s from `GET /logs/recent`). Only populated when the backend uses the batcher (O3 configured).
4. **Upload status (side panel)** – Shows whether the batcher is on and the last upload time/key/count (from `GET /logs/status`).

## API proxy

The app uses Next.js rewrites so that `/api/*` is proxied to `http://localhost:8080/*`. To use another backend URL, set:

```bash
NEXT_PUBLIC_API_URL=http://localhost:8080
```

and run the dev server (rewrites still send requests to the Next server; for a different host you’d use that env when building or configure rewrites accordingly).
