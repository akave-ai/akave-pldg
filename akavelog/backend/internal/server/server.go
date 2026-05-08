package server

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/akave-ai/akavelog/internal/batcher"
	"github.com/akave-ai/akavelog/internal/config"
	"github.com/akave-ai/akavelog/internal/handler"
	"github.com/akave-ai/akavelog/internal/infrastructure/inputs"
	_ "github.com/akave-ai/akavelog/internal/infrastructure/inputs/httpinput"

	"github.com/akave-ai/akavelog/internal/index"
	"github.com/akave-ai/akavelog/internal/ingester"
	akavemiddleware "github.com/akave-ai/akavelog/internal/middleware"
	"github.com/akave-ai/akavelog/internal/model"
	"github.com/akave-ai/akavelog/internal/repository"
	"github.com/akave-ai/akavelog/internal/response"
	"github.com/akave-ai/akavelog/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/akave-ai/akavelog/internal/worker"

)

// memoryBuffer implements inputs.InputBuffer for received log payloads.
type memoryBuffer struct {
	mu   sync.Mutex
	logs [][]byte
}

func (b *memoryBuffer) Insert(p []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logs = append(b.logs, p)
}

// Server holds the Echo app and dependencies.
type Server struct {
	Echo         *echo.Echo
	Config       *config.Config
	ingester     *ingester.Ingester
	indexWriter  *index.O3Writer
	o3Client     *storage.O3Client
	recentLogs   *RecentLogsStore
	uploadStatus *UploadStatusStore
	alertWorker  *worker.AlertWorker
}

// New builds the Echo server and registers routes.
func New(cfg *config.Config, pool *pgxpool.Pool) *Server {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover(), middleware.Logger())

	recentLogs := newRecentLogsStore()
	uploadStatus := &UploadStatusStore{}

	var buf inputs.InputBuffer
	var b *batcher.Batcher
	var o3Client *storage.O3Client
	if cfg.Storage != nil && cfg.Storage.O3 != nil {
		var err error
		o3Client, err = storage.NewO3Client(cfg.Storage.O3)
		if err != nil {
			log.Printf("[server] O3 client: %v (using in-memory buffer)", err)
			o3Client = nil
		}
		if o3Client != nil {
			if err := o3Client.EnsureBucket(context.Background()); err != nil {
				log.Printf("[server] O3 ensure bucket: %v (upload may fail)", err)
			}
			bc := batcher.DefaultBatcherConfig()
			if cfg.Batcher != nil {
				if cfg.Batcher.MaxBatchSize > 0 {
					bc.MaxBatchSize = cfg.Batcher.MaxBatchSize
				}
				if cfg.Batcher.FlushInterval != "" {
					if d, err := time.ParseDuration(cfg.Batcher.FlushInterval); err == nil && d > 0 {
						bc.FlushInterval = d
					}
				}
			}

			opts := &batcher.BatcherOpts{
				OnLog:   func(entry *model.LogEntry) { recentLogs.AddEntry(entry) },
				OnFlush: func(count int, key string) { uploadStatus.SetLastFlush(count, key) },

			if streamConfig.ChunkIdlePeriod == ingester.DefaultStreamConfig().ChunkIdlePeriod {
				streamConfig.ChunkIdlePeriod = 5 * time.Second
			}
			if streamConfig.MaxChunkEntries == ingester.DefaultStreamConfig().MaxChunkEntries {
				streamConfig.MaxChunkEntries = 50
			}

			ing = ingester.NewIngester(chunkStore, streamConfig, idxWriter)

			if pool != nil {
				logBatchRepo := repository.NewLogBatchRepository(pool)
				ing.WithDBIndexer(logBatchRepo)
				log.Printf("[server] PostgreSQL log_batches index enabled")
			}
			b = batcher.NewBatcher(bc, o3Client, "default", opts)
			buf = b
			uploadStatus.mu.Lock()
			uploadStatus.BatcherOn = true
			uploadStatus.mu.Unlock()

			log.Printf("[server] batcher enabled: flush to Akave O3 (batch=%d, interval=%v)", bc.MaxBatchSize, bc.FlushInterval)
		}
	}
	if buf == nil {
		buf = &memoryBuffer{}

		}
	}
	if ing == nil {
		ing = ingester.NewIngester(&noopChunkStore{}, ingester.DefaultStreamConfig(), nil)
		ing.Start(context.Background())

	}

	ingestD := NewIngestDispatcher()


	inputHandler := &handler.InputHandler{
		Registry:      inputs.GlobalRegistry,
		Buffer:        buf,
		InputRepo:     repository.NewInputRepository(pool),
		Instances:     make(map[uuid.UUID]handler.InstanceRecord),
		MountIngest:   ingestD.Mount,
		UnmountIngest: ingestD.Unmount,
	}

	// Management API
	e.GET("/inputs/types", inputHandler.ListTypes)
	e.GET("/inputs/types/:type", inputHandler.GetTypeInfo)
	e.GET("/inputs/info", inputHandler.GetAllTypesInfo)
	e.GET("/inputs", inputHandler.ListInputs)
	e.POST("/inputs", inputHandler.CreateInput)
	e.PUT("/inputs/:id", inputHandler.UpdateInput)
	e.DELETE("/inputs/:id", inputHandler.DeleteInput)

	// Ingest: GET returns recent logs (raw HTTP, same response shape); POST/PUT etc. dispatch to path handler
	e.Any("/ingest/*", func(c echo.Context) error {
		if c.Request().Method == "GET" {
			return response.OK(c, map[string]any{"logs": recentLogs.GetRecent()}, "")
		}
		return echo.WrapHandler(ingestD)(c)
	})

	// Demo UI: recent logs and upload status

	// ── Phase 8: Project + API Key management ─────────────────────────────────
	var projectRepo *repository.ProjectRepository
	if pool != nil {
		projectRepo = repository.NewProjectRepository(pool)
		projectHandler := handler.NewProjectHandler(projectRepo)

		// Project CRUD — no auth required (bootstrap path)
		e.POST("/projects", projectHandler.Create)
		e.GET("/projects", projectHandler.List)
		e.GET("/projects/:id", projectHandler.Get)
		e.DELETE("/projects/:id", projectHandler.Delete)

		// API key management — no auth (operators manage keys)
		e.POST("/projects/:id/api-keys", projectHandler.CreateAPIKey)
		e.GET("/projects/:id/api-keys", projectHandler.ListAPIKeys)
		e.DELETE("/projects/:id/api-keys/:key", projectHandler.RevokeAPIKey)

		log.Printf("[server] project + API key endpoints enabled")
	}

	// ── Authenticated route group (requires X-API-Key) ─────────────────────────
	// When projectRepo is nil (no DB), auth middleware is skipped.
	var authGroup *echo.Group
	if projectRepo != nil {
		authGroup = e.Group("", akavemiddleware.RequireAPIKey(projectRepo))
	} else {
		authGroup = e.Group("") // no auth in degraded mode
	}

	// Push API (authenticated)
	authGroup.POST("/akavelog/api/v1/push", dist.PushHandler(onLog))

	// Demo/ops helpers (unauthenticated — read-only observability)

	e.GET("/logs/recent", func(c echo.Context) error {
		return response.OK(c, map[string]any{"logs": recentLogs.GetRecent()}, "")
	})
	e.GET("/logs/status", func(c echo.Context) error {
		st := uploadStatus.Get()
		return response.OK(c, map[string]any{
			"batcher_enabled":  st.BatcherOn,
			"last_upload_at":   st.LastAt,
			"last_upload_key":  st.LastKey,
			"last_upload_count": st.LastCount,
			"pending_count":    st.Pending,
		}, "")
	})


	e.GET("/uploads", func(c echo.Context) error {
		if o3Client == nil {
			return response.OK(c, map[string]any{"objects": []interface{}{}}, "O3 not configured")
		}
		prefix := c.QueryParam("prefix")
		if prefix == "" {
			prefix = "logs/"
		}
		list, err := o3Client.ListObjects(c.Request().Context(), prefix)
		if err != nil {
			return response.InternalError(c, "list uploads failed", err.Error())
		}
		return response.OK(c, map[string]any{"objects": list}, "")
	})


	inputHandler.RestoreInputs(context.Background())

	types := inputs.GlobalRegistry.ListRegistered()
	sort.Strings(types)
	log.Printf("Registered input types: %v", types)

	return &Server{Echo: e, Config: cfg, batcher: b, o3Client: o3Client, recentLogs: recentLogs, uploadStatus: uploadStatus}
}

// Start starts the HTTP server. Blocks until the context is cancelled or the server fails.
// On context cancel, Shutdown is called so the batcher flushes remaining logs.

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
			log.Printf("[uploads/content] get upload content failed key=%s err=%v", key, err)
			return response.OK(c, map[string]any{"logs": []model.LogEntry{}, "key": key}, "")
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
			log.Printf("[uploads/raw] get raw object failed key=%s err=%v", key, err)
			return response.OK(c, map[string]any{"key": key, "content": "", "encoding": "error"}, "")
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

	// ── Phase 5: Query Engine (authenticated) ─────────────────────────────────
	if o3Client != nil && pool != nil {
		queryEngine := query.New(repository.NewLogBatchRepository(pool), o3Client)
		queryHandler := handler.NewQueryHandler(queryEngine)

		authGroup.POST("/query", queryHandler.Handle)
		authGroup.GET("/query/stream", queryHandler.HandleSSE)

		log.Printf("[server] query engine enabled: POST /query, GET /query/stream")
	}

	// ── Phase 7: Alert rules (authenticated) ──────────────────────────────────
	if pool != nil {
		alertRepo := repository.NewAlertRepository(pool)
		alertHandler := handler.NewAlertHandler(alertRepo)

		authGroup.POST("/alerts", alertHandler.Create)
		authGroup.GET("/alerts", alertHandler.List)
		authGroup.DELETE("/alerts/:id", alertHandler.Delete)
		authGroup.GET("/alerts/:id/events", alertHandler.ListEvents)

		log.Printf("[server] alert endpoints enabled")

		if o3Client != nil {
			alertWorker := worker.New(alertRepo, worker.NewQueryEngineCounter(repository.NewLogBatchRepository(pool)), 60*time.Second)
			alertWorker.Start(context.Background())
			log.Printf("[alert-worker] background evaluation started (60s interval)")
		}
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


func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.Shutdown(context.Background())
	}()
	return s.Echo.Start(":" + s.Config.Server.Port)
}

// Shutdown gracefully shuts down the server and the batcher (flush remaining logs).
func (s *Server) Shutdown(ctx context.Context) error {
	if s.batcher != nil {
		s.batcher.Stop()
	}
	return s.Echo.Shutdown(ctx)
}

// Shutdown gracefully stops everything.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.alertWorker != nil {
		s.alertWorker.Stop()
	}
	if s.ingester != nil {
		s.ingester.Stop()
	}
	if s.indexWriter != nil {
		s.indexWriter.Stop()
	}
	return s.Echo.Shutdown(ctx)
}

type noopChunkStore struct{}

func (n *noopChunkStore) Put(_ context.Context, _ []chunk.Chunk) error { return nil }

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
	level := "info"
	if labels != nil {
		if l := labels["level"]; l != "" {
			level = strings.ToLower(l)
		}
	}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	if tsNs > 0 {
		ts = time.Unix(0, tsNs).UTC().Format(time.RFC3339Nano)
	}
	return &model.LogEntry{
		Timestamp: ts,
		Service:   service,
		Level:     level,
		Message:   line,
		Tags:      labels,
	}
}

