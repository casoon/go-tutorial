// Command maxbytes shows how http.MaxBytesReader caps request bodies.
//
// It starts a test server, sends one small and one oversized body and
// prints the status codes. Run it with: go run ./beispiele/13-security/maxbytes
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
)

const maxBodyBytes = 1 << 10 // 1 KiB is plenty for a URL and a number

type createTarget struct {
	URL             string `json:"url"`
	IntervalSeconds int64  `json:"interval_seconds"`
}

func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /targets", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var req createTarget
		if err := dec.Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, fmt.Sprintf("body exceeds %d bytes", maxErr.Limit),
					http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	return mux
}

func post(url, body string) (int, error) {
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func main() {
	srv := httptest.NewServer(handler())
	defer srv.Close()

	small := `{"url":"https://example.com","interval_seconds":60}`
	large := `{"url":"https://example.com/` + strings.Repeat("a", 1<<20) + `"}`

	for _, c := range []struct{ name, body string }{
		{"small body", small},
		{"1 MiB body", large},
		{"unknown field", `{"url":"https://example.com","admin":true}`},
	} {
		status, err := post(srv.URL+"/targets", c.body)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%-14s -> %d %s\n", c.name, status, http.StatusText(status))
	}
}
