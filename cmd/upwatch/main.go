// Command upwatch is a small URL monitoring service: it checks registered
// URLs periodically and stores status code and response time.
//
// main only wires dependencies together. All logic lives in internal/.
package main

import (
	"context"
	"errors"
	"expvar"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/casoon/go-tutorial/internal/check"
	"github.com/casoon/go-tutorial/internal/config"
	"github.com/casoon/go-tutorial/internal/httpapi"
	"github.com/casoon/go-tutorial/internal/scheduler"
	"github.com/casoon/go-tutorial/internal/sqlitestore"
)

// version is set at build time: go build -ldflags "-X main.version=v1.0.0"
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "upwatch:", err)
		os.Exit(1)
	}
}

// run returns an error instead of calling os.Exit, so deferred cleanups
// (closing the database) always run.
func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)
	logger.Info("starting", "version", version, "addr", cfg.Addr, "db", cfg.DBPath)

	// ctx is canceled on Ctrl+C or SIGTERM (sent by systemd, Docker, k8s).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := sqlitestore.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if cfg.AllowPrivate {
		logger.Warn("checks may reach private and loopback addresses")
	}
	checker := check.New(check.NewClient(cfg.AllowPrivate), cfg.CheckTimeout)
	sched := scheduler.New(store, checker, logger, cfg.Workers, cfg.Tick)

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: httpapi.New(store, logger),
		// Without timeouts a slow or malicious client can hold a
		// connection (and a goroutine) open forever.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	var wg sync.WaitGroup
	wg.Go(func() { sched.Run(ctx) })

	if cfg.DebugAddr != "" {
		go serveDebug(cfg.DebugAddr, logger)
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		stop()
		wg.Wait()
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	// Graceful shutdown: stop accepting connections and give in-flight
	// requests up to 15 seconds to finish. The scheduler sees the same
	// canceled ctx and drains its workers in parallel.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	wg.Wait()

	logger.Info("stopped")
	return nil
}

// serveDebug exposes metrics (expvar) and profiles (pprof) on a separate
// listener. Bind it to localhost: profiles and runtime stats are not for
// the public internet.
func serveDebug(addr string, logger *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("GET /debug/vars", expvar.Handler())
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)

	logger.Info("debug listener", "addr", addr)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("debug listener", "err", err)
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogJSON {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
