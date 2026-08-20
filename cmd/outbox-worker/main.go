package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"scholaroscope-temporal-service/config"
	"scholaroscope-temporal-service/internal/db"
	"scholaroscope-temporal-service/internal/outbox"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	worker := outbox.NewWorker(pool, outbox.Config{
		FallbackCallbackURL: cfg.ScholaroscopeWebhookURL,
		RequestTimeout:      cfg.OutboxRequestTimeout,
		MaxAttempts:         cfg.OutboxMaxAttempts,
		BatchSize:           cfg.OutboxBatchSize,
		PollInterval:        cfg.OutboxPollInterval,
	})
	if os.Getenv("TEMPORAL_OUTBOX_ONCE") == "true" {
		claimed, err := worker.DispatchOnce(ctx)
		if err != nil {
			log.Fatalf("outbox once: %v", err)
		}
		log.Printf("temporal outbox once complete claimed=%d", claimed)
		return
	}
	log.Printf("temporal outbox worker started batch_size=%d poll_seconds=%s", cfg.OutboxBatchSize, strconv.FormatInt(int64(cfg.OutboxPollInterval/time.Second), 10))
	if err := worker.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("outbox worker: %v", err)
	}
}
