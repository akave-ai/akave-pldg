package main

import (
	"context"
	"log"
	"os"

	"github.com/akave-ai/akavelog/internal/config"
	"github.com/akave-ai/akavelog/internal/database"
	"github.com/akave-ai/akavelog/internal/logger"
	"github.com/akave-ai/akavelog/internal/server"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	loggerService := logger.NewLoggerService(cfg.Observability)
	log := logger.NewLoggerWithService(cfg.Observability, loggerService)
	defer loggerService.Shutdown()

	ctx := context.Background()
	if err := database.Migrate(ctx, &log, cfg); err != nil {
		log.Fatal().Err(err).Msg("migrate")
	}

	db, err := database.New(cfg, &log, loggerService)
	if err != nil {
		log.Fatal().Err(err).Msg("database")
	}
	defer db.Pool.Close()

	srv := server.New(cfg, db.Pool)
	if err := srv.Start(ctx); err != nil {
		log.Printf("server exited: %v", err)
		os.Exit(1)
	}
}
