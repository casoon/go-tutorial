// Command shutdown is a minimal HTTP server with graceful shutdown.
// Start it, request /slow and press Ctrl+C while the request is running:
// the request still completes.
//
//	go run ./beispiele/12-logging-konfiguration-observability/shutdown -addr 127.0.0.1:8081
//	curl -i localhost:8081/slow
//
// To send SIGTERM with kill, build the binary and start it directly:
// kill on the go run process does not reach the program.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "shutdown:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:8081", "listen address")
	grace := flag.Duration("grace", 10*time.Second, "time in-flight requests get on shutdown")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		logger.Info("slow request started")
		select {
		case <-time.After(2 * time.Second):
			fmt.Fprintln(w, "done")
			logger.Info("slow request finished")
		case <-r.Context().Done():
			logger.Warn("slow request canceled", "err", r.Context().Err())
		}
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", *addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	// Restore default signal handling: a second Ctrl+C now kills the
	// process instead of being swallowed while we wait.
	stop()
	logger.Info("shutting down", "grace", *grace)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), *grace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("stopped")
	return nil
}
