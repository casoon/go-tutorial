// Command slog tours log/slog: handlers, levels, attributes, With,
// LogValuer for masking, and bridging the old log package.
//
//	go run ./beispiele/12-logging-konfiguration-observability/slog
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"
)

// Secret never shows up in logs, whatever handler is used.
type Secret string

func (Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }

// Target logs as a compact group instead of every field.
type Target struct {
	ID       int64
	URL      string
	Interval time.Duration
	APIToken Secret
}

func (t Target) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("id", t.ID),
		slog.String("url", t.URL),
	)
}

func main() {
	// Drop the time attribute so the output of this example is stable.
	// Real services keep it.
	noTime := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey && len(groups) == 0 {
			return slog.Attr{}
		}
		return a
	}

	// A LevelVar can be changed at runtime, e.g. from an admin endpoint.
	var level slog.LevelVar
	level.Set(slog.LevelInfo)
	opts := &slog.HandlerOptions{Level: &level, ReplaceAttr: noTime}

	text := slog.New(slog.NewTextHandler(os.Stdout, opts))
	jsonLog := slog.New(slog.NewJSONHandler(os.Stdout, opts))

	target := Target{ID: 7, URL: "https://example.com", Interval: time.Minute, APIToken: "s3cr3t"}

	// Key-value pairs and typed attributes; both are structured.
	text.Info("check finished", "target_id", 7, "status", 200, "duration", 84*time.Millisecond)
	jsonLog.Info("check finished", slog.Int64("target_id", 7), slog.Int("status", 200),
		slog.Duration("duration", 84*time.Millisecond))

	// Debug is below the configured level and costs almost nothing.
	text.Debug("not printed")
	level.Set(slog.LevelDebug)
	text.Debug("printed after raising the level")

	// With returns a logger that adds attributes to every record.
	checkLog := text.With("target", target)
	checkLog.Warn("target down", "status", 503)
	jsonLog.With("target", target).Warn("target down", "status", 503)

	// LogValuer also hides values passed directly.
	text.Info("token loaded", "token", target.APIToken)

	// Context-aware variants hand ctx to the handler (trace IDs, see text).
	text.InfoContext(context.Background(), "with context")

	// SetDefault routes slog.Info and the old log package through text.
	slog.SetDefault(text)
	log.Printf("legacy log call, e.g. from a library")

	// For APIs that want a *log.Logger, such as http.Server.ErrorLog.
	errorLog := slog.NewLogLogger(text.Handler(), slog.LevelWarn)
	errorLog.Print("http: TLS handshake error from 203.0.113.9:51234: EOF")
}
