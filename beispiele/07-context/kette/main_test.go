package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	site := slowSite(100 * time.Millisecond)
	defer site.Close()
	client := site.Client()

	t.Run("completes", func(t *testing.T) {
		out, err := Check(t.Context(), client, site.URL, 2*time.Second)
		if err != nil || out != "status 200" {
			t.Errorf("got %q, %v; want status 200", out, err)
		}
	})

	t.Run("site too slow", func(t *testing.T) {
		out, err := Check(t.Context(), client, site.URL, 20*time.Millisecond)
		if err != nil || out != "timeout" {
			t.Errorf("got %q, %v; want timeout and no error", out, err)
		}
	})

	t.Run("caller gave up", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(t.Context())
		time.AfterFunc(20*time.Millisecond, func() { cancel(ErrShutdown) })

		_, err := Check(ctx, client, site.URL, 2*time.Second)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if cause := context.Cause(ctx); !errors.Is(cause, ErrShutdown) {
			t.Errorf("cause = %v, want ErrShutdown", cause)
		}
	})
}

func TestWithoutCancelKeepsValues(t *testing.T) {
	parent, cancel := context.WithCancel(WithRequestID(t.Context(), "req-1"))
	cancel()

	detached := context.WithoutCancel(parent)
	if detached.Err() != nil {
		t.Errorf("detached.Err() = %v, want nil", detached.Err())
	}
	if got := RequestID(detached); got != "req-1" {
		t.Errorf("RequestID = %q, want req-1", got)
	}
}
