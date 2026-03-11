package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/akave-ai/akavelog/internal/chunk"
	"github.com/akave-ai/akavelog/internal/config"
	"github.com/akave-ai/akavelog/internal/distributor"
	"github.com/akave-ai/akavelog/internal/handler"
	"github.com/akave-ai/akavelog/internal/index"
	"github.com/akave-ai/akavelog/internal/ingester"
	"github.com/akave-ai/akavelog/internal/model"
	"github.com/akave-ai/akavelog/internal/query"
	"github.com/akave-ai/akavelog/internal/repository"
	"github.com/akave-ai/akavelog/internal/response"
	"github.com/akave-ai/akavelog/internal/storage"
)

// Server holds the Echo app and dependencies.
type Server struct {
	Echo         *echo.Echo
	Config       *config.Config
	ingester     *ingester.Ingester // optional; stopped on Shutdown
	indexWriter  *index.O3Writer    // optional; Progress index, stopped on Shutdown
	o3Client     *storage.O3Client  // optional; for listing uploads
	recentLogs   *RecentLogsStore
	uploadStatus *UploadStatusStore
}

// New builds the Echo server and registers routes.
// Caller must provide a non-nil pool (e.g. from database.Database.Pool).
func New(cfg *config.Config, pool *pgxpool.Pool) *Server {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover(), middleware.Logger())

	recentLogs := newRecentLogsStore()
	uploadStatus := &UploadStatusStore{}

	var ing *ingester.Ingester
	var idxWriter *index.O3Writer
	var o3Client *storage.O3Client

	if cfg.Storage != nil && cfg.Storage.O3 != nil {
		var err error
		o3Client, err = storage.NewO3Client(cfg.Storage.O3)
		if err != nil {
			log.Printf("[server] O3 client: %v (push will not persist)", err)
			o3Client = nil
		}
		if o3Client != nil {
			if err := o3Client.EnsureBucket(context.Background()); err != nil {
				log.Printf("[server] O3 ensure bucket: %v (upload may fail)", err)
			}
			chunkStore := chunk.NewO3Store(o3Client)
			idxWriter = index.NewO3Writer(o3Client, index.DefaultO3WriterConfig())
			go idxWriter.Run(context.Background())

			streamConfig := ingester.DefaultStreamConfig()
			if cfg.Ingester != nil {
				if cfg.Ingester.ChunkIdleSeconds > 0 {
					streamConfig.ChunkIdlePeriod = time.Duration(cfg.Ingester.ChunkIdleSeconds) * time.Second
				}
				if cfg.Ingester.ChunkMaxEntries > 0 {
					streamConfig.MaxChunkEntries = cfg.Ingester.ChunkMaxEntries
				}
			}
			// Demo-friendly defaults when O3 enabled: 5s idle, 50 entries.
			if streamConfig.ChunkIdlePeriod == ingester.DefaultStreamConfig().ChunkIdlePeriod {
				streamConfig.ChunkIdlePeriod = 5 * time.Second
			}
			if streamConfig.MaxChunkEntries == ingester.DefaultStreamConfig().MaxChunkEntries {
				streamConfig.MaxChunkEntries = 50
			}

			ing = ingester.NewIngester(chunkStore, streamConfig, idxWriter)

			// ── Phase 4: attach PostgreSQL metadata index ─────────────────────
			if pool != nil {
				logBatchRepo := repository.NewLogBatchRepository(pool)
				ing.WithDBIndexer(logBatchRepo)
				log.Printf("[server] PostgreSQL log_batches index enabled")
			}

			ing.OnFlush(func(count int, key string) { uploadStatus.SetLastFlush(count, key) })
			ing.Start(context.Background())

			uploadStatus.mu.Lock()
			uploadStatus.BatcherOn = true
			uploadStatus.mu.Unlock()

			log.Printf("[server] ingester + Progress index enabled: chunks to O3 (idle=%v, maxEntries=%d)",
				streamConfig.ChunkIdlePeriod, streamConfig.MaxChunkEntries)
		}
	}

	if ing == nil {
		// No O3: noop chunk store, no index. Logs are visible in /logs/recent only.
		ing = ingester.NewIngester(&noopChunkStore{}, ingester.DefaultStreamConfig(), nil)
		ing.Start(context.Background())
	}

	dist := distributor.New(ing)
	onLog := func(labels map[string]string, tsNs int64, line string) {
		entry := labelsToLogEntry(labels, tsNs, line)
		if entry != nil {
			recentLogs.AddEntry(entry)
		}
	}

	// ── Routes ────────────────────────────────────────────────────────────────

	// Akavelog push API
	e.POST("/akavelog/api/v1/push", dist.PushHandler(onLog))

	// Demo UI helpers
	e.GET("/logs/recent", func(c echo.Context) error {
		return response.OK(c, map[string]any{"logs": recentLogs.GetRecent()}, "")
	})
	e.GET("/logs/status", func(c echo.Context) error {
		st := uploadStatus.Get()
		return response.OK(c, map[string]any{
			"batcher_enabled":   st.BatcherOn,
			"last_upload_at":    st.LastAt,
			"last_upload_key":   st.LastKey,
			"last_upload_count": st.LastCount,
			"pending_count":     st.Pending,
		}, "")
	})

	// O3 object listing
	e.GET("/uploads", func(c echo.Context) error {
		if o3Client == nil {
			return response.OK(c, map[string]any{"objects": []interface{}{}}, "O3 not configured")
		}
		prefix := c.QueryParam("prefix")
		if prefix == "" {
			prefix = "chunks/"
		}
		list, err := o3Client.ListObjects(c.Request().Context(), prefix)
		if err != nil {
			return response.InternalError(c, "list uploads failed", err.Error())
		}
		return response.OK(c, map[string]any{"objects": list}, "")
	})

	// O3 Progress index lookup
	e.GET("/index/chunks", func(c echo.Context) error {
		if o3Client == nil {
			return response.OK(c, map[string]any{"chunks": []interface{}{}}, "O3 not configured")
		}
		tenant := c.QueryParam("tenant")
		if tenant == "" {
			tenant = "default"
		}
		streamID := c.QueryParam("stream_id")
		var fromNs, toNs int64
		if s := c.QueryParam("from_ns"); s != "" {
			if _, err := fmt.Sscanf(s, "%d", &fromNs); err != nil {
				return response.BadRequest(c, "invalid from_ns", "from_ns must be nanoseconds")
			}
		}
		if s := c.QueryParam("to_ns"); s != "" {
			if _, err := fmt.Sscanf(s, "%d", &toNs); err != nil {
				return response.BadRequest(c, "invalid to_ns", "to_ns must be nanoseconds")
			}
		}
		idxReader := index.NewReader(o3Client)
		refs, err := idxReader.ListChunkKeys(c.Request().Context(), tenant, streamID, fromNs, toNs)
		if err != nil {
			return response.InternalError(c, "index lookup failed", err.Error())
		}
		return response.OK(c, map[string]any{"chunks": refs}, "")
	})

	// ── Phase 4: PostgreSQL metadata index endpoint ───────────────────────────
	// GET /index/batches?tenant=default&from=2006-01-02T15:04:05Z&to=...
	// Returns log_batches rows (O3 object keys + metadata) for the given time range.
	// This is the SQL counterpart to GET /index/chunks (O3-based).
	e.GET("/index/batches", func(c echo.Context) error {
		if pool == nil {
			return response.OK(c, map[string]any{"batches": []interface{}{}}, "DB not configured")
		}
		tenant := c.QueryParam("tenant")
		if tenant == "" {
			tenant = "default"
		}
		fromStr := c.QueryParam("from")
		toStr := c.QueryParam("to")

		var from, to time.Time
		var parseErr error
		if fromStr != "" {
			from, parseErr = time.Parse(time.RFC3339, fromStr)
			if parseErr != nil {
				return response.BadRequest(c, "invalid from", "from must be RFC3339")
			}
		}
		if toStr != "" {
			to, parseErr = time.Parse(time.RFC3339, toStr)
			if parseErr != nil {
				return response.BadRequest(c, "invalid to", "to must be RFC3339")
			}
		}
		if to.IsZero() {
			to = time.Now().UTC()
		}

		repo := repository.NewLogBatchRepository(pool)
		batches, err := repo.ListByTimeRange(c.Request().Context(), logbatchesQueryParams(tenant, from, to))
		if err != nil {
			return response.InternalError(c, "index batches lookup failed", err.Error())
		}
		return response.OK(c, map[string]any{"batches": batches}, "")
	})

	// Raw object content (gzip-transparent)
	e.GET("/uploads/content", func(c echo.Context) error {
		if o3Client == nil {
			return response.BadRequest(c, "O3 not configured", "O3 not configured")
		}
		key := c.QueryParam("key")
		if key == "" {
			return response.BadRequest(c, "missing key", "query param key is required")
		}
		logs, err := o3Client.GetObjectLogs(c.Request().Context(), key)
		if err != nil {
			return response.InternalError(c, "get upload content failed", err.Error())
		}
		return response.OK(c, map[string]any{"logs": logs, "key": key}, "")
	})

	e.GET("/uploads/raw", func(c echo.Context) error {
		if o3Client == nil {
			return response.BadRequest(c, "O3 not configured", "O3 not configured")
		}
		key := c.QueryParam("key")
		if key == "" {
			return response.BadRequest(c, "missing key", "query param key is required")
		}
		raw, err := o3Client.GetObject(c.Request().Context(), key)
		if err != nil {
			return response.InternalError(c, "get raw object failed", err.Error())
		}
		content := string(raw)
		encoding := "identity"
		if strings.HasSuffix(key, ".gz") || strings.HasSuffix(key, ".json.gz") {
			zr, err := gzip.NewReader(bytes.NewReader(raw))
			if err == nil {
				decoded, _ := io.ReadAll(zr)
				_ = zr.Close()
				content = string(decoded)
				encoding = "gzip"
			}
		}
		return response.OK(c, map[string]any{"key": key, "content": content, "encoding": encoding}, "")
	})

	// ── Phase 5: Query Engine ─────────────────────────────────────────────────
	if o3Client != nil && pool != nil {
		queryEngine := query.New(repository.NewLogBatchRepository(pool), o3Client)
		queryHandler := handler.NewQueryHandler(queryEngine)

		// POST /query — full result set as JSON
		e.POST("/query", queryHandler.Handle)

		// GET /query/stream — Server-Sent Events stream
		e.GET("/query/stream", queryHandler.HandleSSE)

		log.Printf("[server] query engine enabled: POST /query, GET /query/stream")
	}

	return &Server{
		Echo:         e,
		Config:       cfg,
		ingester:     ing,
		indexWriter:  idxWriter,
		o3Client:     o3Client,
		recentLogs:   recentLogs,
		uploadStatus: uploadStatus,
	}
}

// Start starts the HTTP server. Blocks until the context is cancelled or the server fails.
func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.Shutdown(context.Background())
	}()
	addr := ":" + s.Config.Server.Port
	return s.Echo.Start(addr)
}

// Shutdown gracefully shuts down the server, ingester, and Progress index writer.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.ingester != nil {
		s.ingester.Stop()
	}
	if s.indexWriter != nil {
		s.indexWriter.Stop()
	}
	return s.Echo.Shutdown(ctx)
}

// noopChunkStore drops chunks (used when O3 is not configured).
type noopChunkStore struct{}

func (n *noopChunkStore) Put(_ context.Context, _ []chunk.Chunk) error { return nil }

// labelsToLogEntry converts push (labels, tsNs, line) to model.LogEntry for the recent-logs UI.
func labelsToLogEntry(labels map[string]string, tsNs int64, line string) *model.LogEntry {
	service := "akavelog"
	if labels != nil {
		for _, key := range []string{"job", "app", "service"} {
			if v := labels[key]; v != "" {
				service = v
				break
			}
		}
	}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	if tsNs > 0 {
		ts = time.Unix(0, tsNs).UTC().Format(time.RFC3339Nano)
	}
	return &model.LogEntry{
		Timestamp: ts,
		Service:   service,
		Level:     "info",
		Message:   line,
		Tags:      labels,
	}
}
