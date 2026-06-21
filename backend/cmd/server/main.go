// Command server runs the idea-collect HTTP API.
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

	"github.com/jasonross/idea-collect/backend/internal/api"
	"github.com/jasonross/idea-collect/backend/internal/config"
	"github.com/jasonross/idea-collect/backend/internal/github"
	"github.com/jasonross/idea-collect/backend/internal/ratelimit"
	"github.com/jasonross/idea-collect/backend/internal/session"
	"github.com/jasonross/idea-collect/backend/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.RequireServer(); err != nil {
		return err
	}

	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	log.Info("migrations applied")

	gh, err := github.New(cfg.GitHub.AppID, cfg.GitHub.InstallationID, cfg.GitHub.PrivateKeyPEM,
		cfg.GitHub.APIBase, cfg.GitHub.GraphQLURL)
	if err != nil {
		return err
	}

	sm := session.NewManager(cfg.SessionSecret, cfg.SessionTTL, cfg.CookieSecure)
	rl := ratelimit.New(cfg.AuthRatePerMinute, cfg.AuthRateBurst)
	defer rl.Close()

	srv := api.NewServer(st, sm, rl, gh, cfg.AllowedOrigins, log)

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
