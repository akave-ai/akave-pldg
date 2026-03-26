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
	"github.com/akave-ai/akavelog/internal/model"
	"github.com/akave-ai/akavelog/internal/repository"
	"github.com/akave-ai/akavelog/internal/response"
	"github.com/akave-ai/akavelog/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
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
	Echo           *echo.Echo
	Config         *config.Config
	batcher        *batcher.Batcher   // optional; stopped on Shutdown
	o3Client       *storage.O3Client // optional; for listing uploads
	recentLogs     *RecentLogsStore
	uploadStatus   *UploadStatusStore
}

// New builds the Echo server and registers routes.
// Caller must provide a non-nil pool (e.g. from database.Database.Pool).
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

	// List objects uploaded to O3 (log batches)
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
func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.Shutdown(context.Background())
	}()
	addr := ":" + s.Config.Server.Port
	return s.Echo.Start(addr)
}

// Shutdown gracefully shuts down the server and the batcher (flush remaining logs).
func (s *Server) Shutdown(ctx context.Context) error {
	if s.batcher != nil {
		s.batcher.Stop()
	}
	return s.Echo.Shutdown(ctx)
}
