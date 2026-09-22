// SalesArena API server: Gin + MongoDB + Redis.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/salesarena/backend/internal/api"
	"github.com/salesarena/backend/internal/cache"
	"github.com/salesarena/backend/internal/config"
	"github.com/salesarena/backend/internal/seed"
	"github.com/salesarena/backend/internal/service"
	"github.com/salesarena/backend/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	st, err := store.New(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		slog.Error("mongo", "err", err)
		os.Exit(1)
	}
	defer st.Close(context.Background())

	rc, err := cache.New(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		slog.Error("redis", "err", err)
		os.Exit(1)
	}
	defer rc.Close()

	if cfg.SeedOnStart {
		n, err := st.CountUsers(ctx)
		if err != nil {
			slog.Error("count users", "err", err)
			os.Exit(1)
		}
		if n == 0 {
			if err := seed.Run(ctx, st, time.Now().In(cfg.Timezone)); err != nil {
				slog.Error("seed", "err", err)
				os.Exit(1)
			}
		}
	}

	svc := service.New(st, rc, cfg.Timezone, cfg.SessionTTL)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.New(svc, cfg).Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", srv.Addr, "mongo", cfg.MongoDB, "redis", cfg.RedisAddr, "demo", cfg.DemoMode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down")
	shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = srv.Shutdown(shutdownCtx)
}
