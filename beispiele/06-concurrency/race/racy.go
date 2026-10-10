//go:build ignore

// racy.go has a data race on purpose and is excluded from normal builds.
// Run it with the race detector:
//
//	go run -race racy.go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var checks int
	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			checks++ // read, add, write: not atomic, not synchronized
		})
	}
	wg.Wait()
	fmt.Println("checks:", checks)
}
