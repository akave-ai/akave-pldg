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

	"github.com/akave-ai/akavelog/internal/chunk"
	"github.com/akave-ai/akavelog/internal/config"
	"github.com/akave-ai/akavelog/internal/distributor"
	"github.com/akave-ai/akavelog/internal/index"
	"github.com/akave-ai/akavelog/internal/ingester"
	"github.com/akave-ai/akavelog/internal/model"
	"github.com/akave-ai/akavelog/internal/response"
	"github.com/akave-ai/akavelog/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Server holds the Echo app and dependencies.
type Server struct {
	Echo         *echo.Echo
	Config       *config.Config
	ingester     *ingester.Ingester   // optional; stopped on Shutdown
	indexWriter  *index.O3Writer     // optional; Progress index, stopped on Shutdown
	o3Client     *storage.O3Client   // optional; for listing uploads
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
			// Demo-friendly defaults when O3 enabled: 5s idle, 50 entries (override via config if set)
			if cfg.Ingester != nil {
				if cfg.Ingester.ChunkIdleSeconds > 0 {
					streamConfig.ChunkIdlePeriod = time.Duration(cfg.Ingester.ChunkIdleSeconds) * time.Second
				}
				if cfg.Ingester.ChunkMaxEntries > 0 {
					streamConfig.MaxChunkEntries = cfg.Ingester.ChunkMaxEntries
				}
			}
			if streamConfig.ChunkIdlePeriod == ingester.DefaultStreamConfig().ChunkIdlePeriod {
				streamConfig.ChunkIdlePeriod = 5 * time.Second
			}
			if streamConfig.MaxChunkEntries == ingester.DefaultStreamConfig().MaxChunkEntries {
				streamConfig.MaxChunkEntries = 50
			}
			ing = ingester.NewIngester(chunkStore, streamConfig, idxWriter)
			ing.OnFlush(func(count int, key string) { uploadStatus.SetLastFlush(count, key) })
			ing.Start(context.Background())
			uploadStatus.mu.Lock()
			uploadStatus.BatcherOn = true
			uploadStatus.mu.Unlock()
			log.Printf("[server] ingester + Progress index enabled: chunks to O3 (idle=%v, maxEntries=%d)", streamConfig.ChunkIdlePeriod, streamConfig.MaxChunkEntries)
		}
	}
	if ing == nil {
		// No O3: use in-memory chunk store that no-ops (chunks dropped), no index
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
	// Akavelog push API: POST /akavelog/api/v1/push → Distributor → Ingester → chunks to O3
	e.POST("/akavelog/api/v1/push", dist.PushHandler(onLog))

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
			prefix = "chunks/"
		}
		list, err := o3Client.ListObjects(c.Request().Context(), prefix)
		if err != nil {
			return response.InternalError(c, "list uploads failed", err.Error())
		}
		return response.OK(c, map[string]any{"objects": list}, "")
	})

	// Progress index: resolve chunk keys for a stream and time range (for query path)
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

	// Get stored logs from a single batch object (gzip JSON by key)
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

	// Get raw object content (decoded if gzip) for display; read-only.
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

	return &Server{Echo: e, Config: cfg, ingester: ing, indexWriter: idxWriter, o3Client: o3Client, recentLogs: recentLogs, uploadStatus: uploadStatus}
}

// Start starts the HTTP server. Blocks until the context is cancelled or the server fails.
// On context cancel, Shutdown is called so the ingester flushes remaining chunks.
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

func (n *noopChunkStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	return nil
}

// labelsToLogEntry converts push (labels, tsNs, line) to model.LogEntry for recent logs UI.
func labelsToLogEntry(labels map[string]string, tsNs int64, line string) *model.LogEntry {
	service := "akavelog"
	if labels != nil {
		if j := labels["job"]; j != "" {
			service = j
		} else if a := labels["app"]; a != "" {
			service = a
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
