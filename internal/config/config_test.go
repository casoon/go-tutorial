package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// env turns a map into a getenv function.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.Workers != 8 || cfg.LogLevel != slog.LevelInfo {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"UPWATCH_ADDR":      "127.0.0.1:9000",
		"UPWATCH_WORKERS":   "2",
		"UPWATCH_TICK":      "1s",
		"UPWATCH_LOG_LEVEL": "debug",
		"UPWATCH_LOG_JSON":  "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9000" || cfg.Workers != 2 || cfg.Tick != time.Second ||
		cfg.LogLevel != slog.LevelDebug || !cfg.LogJSON {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := Load(env(map[string]string{
		"UPWATCH_WORKERS": "zero",
		"UPWATCH_TICK":    "-1s",
	}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, key := range []string{"UPWATCH_WORKERS", "UPWATCH_TICK"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q does not mention %s", err, key)
		}
	}
}
