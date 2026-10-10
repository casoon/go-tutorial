// Command langsam is a deliberately slow website for local experiments
// with upwatch: every request is answered after -delay. Start upwatch with
// UPWATCH_ALLOW_PRIVATE_TARGETS=true so it may check this local server.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18101", "listen address")
	delay := flag.Duration("delay", time.Second, "time until each response")
	flag.Parse()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		select {
		case <-time.After(*delay):
			log.Printf("%s answered after %s", r.URL.Path, time.Since(start).Round(time.Millisecond))
		case <-r.Context().Done():
			// The client (upwatch) canceled the request.
			log.Printf("%s canceled by client after %s", r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})

	log.Printf("listening on %s, delay %s", *addr, *delay)
	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
