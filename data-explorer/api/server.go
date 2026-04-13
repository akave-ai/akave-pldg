package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"data-explorer/api/handlers"
	"data-explorer/api/middleware"
	"data-explorer/database"
)

// Server wraps an http.Server with the chi router wired up.
type Server struct {
	srv *http.Server
}

// NewServer constructs a Server with all routes and middleware registered.
func NewServer(db *database.DB, addr string) *Server {
	r := chi.NewRouter()

	// Global middleware stack.
	r.Use(chimw.Recoverer)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS)

	// Routes.
	r.Get("/health", handlers.Health(db))
	r.Get("/methods", handlers.Methods(db))
	r.Get("/actions", handlers.ListActions(db))
	r.Get("/actions/{blockNum}/{id}", handlers.GetAction(db))

	return &Server{
		srv: &http.Server{
			Addr:    addr,
			Handler: r,
		},
	}
}

// Start begins accepting HTTP connections. It blocks until an error occurs.
func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

// Shutdown gracefully drains active connections.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
