// Package config reads upwatch's settings from environment variables.
//
// Environment variables work the same on a laptop, under systemd and in a
// container, and keep secrets out of the binary and the repository.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config holds every setting. Defaults are chosen for local development.
type Config struct {
	Addr         string        // UPWATCH_ADDR
	DebugAddr    string        // UPWATCH_DEBUG_ADDR, empty disables /debug/*
	DBPath       string        // UPWATCH_DB
	Workers      int           // UPWATCH_WORKERS
	Tick         time.Duration // UPWATCH_TICK
	CheckTimeout time.Duration // UPWATCH_CHECK_TIMEOUT
	LogLevel     slog.Level    // UPWATCH_LOG_LEVEL (debug, info, warn, error)
	LogJSON      bool          // UPWATCH_LOG_JSON
	AllowPrivate bool          // UPWATCH_ALLOW_PRIVATE_TARGETS, for local testing only
}

// Load reads the configuration via getenv, typically os.Getenv.
// Passing the lookup function in keeps Load free of global state and
// trivially testable.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:         ":8080",
		DBPath:       "upwatch.db",
		Workers:      8,
		Tick:         5 * time.Second,
		CheckTimeout: 10 * time.Second,
		LogLevel:     slog.LevelInfo,
	}

	var errs []error
	str := func(key string, dst *string) {
		if v := getenv(key); v != "" {
			*dst = v
		}
	}
	num := func(key string, dst *int) {
		if v := getenv(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				errs = append(errs, fmt.Errorf("%s: want a positive integer, got %q", key, v))
				return
			}
			*dst = n
		}
	}
	dur := func(key string, dst *time.Duration) {
		if v := getenv(key); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				errs = append(errs, fmt.Errorf("%s: want a positive duration like 5s, got %q", key, v))
				return
			}
			*dst = d
		}
	}

	str("UPWATCH_ADDR", &cfg.Addr)
	str("UPWATCH_DEBUG_ADDR", &cfg.DebugAddr)
	str("UPWATCH_DB", &cfg.DBPath)
	num("UPWATCH_WORKERS", &cfg.Workers)
	dur("UPWATCH_TICK", &cfg.Tick)
	dur("UPWATCH_CHECK_TIMEOUT", &cfg.CheckTimeout)

	if v := getenv("UPWATCH_LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("UPWATCH_LOG_LEVEL: %w", err))
		}
	}
	boolean := func(key string, dst *bool) {
		if v := getenv(key); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: want true or false, got %q", key, v))
				return
			}
			*dst = b
		}
	}
	boolean("UPWATCH_LOG_JSON", &cfg.LogJSON)
	boolean("UPWATCH_ALLOW_PRIVATE_TARGETS", &cfg.AllowPrivate)

	// Report every problem at once instead of one per restart.
	return cfg, errors.Join(errs...)
}

// FromEnv is Load with the real process environment.
func FromEnv() (Config, error) { return Load(os.Getenv) }
