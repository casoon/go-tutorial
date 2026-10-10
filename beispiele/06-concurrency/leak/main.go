// Command leak shows a classic goroutine leak and its fix: asking several
// mirrors and taking the first answer.
package main

import (
	"fmt"
	"runtime"
	"time"
)

// FetchFunc fetches url and returns a short description.
type FetchFunc func(url string) string

// FirstLeaky returns the first answer. The other goroutines block forever
// on the unbuffered channel, because nobody receives their result.
func FirstLeaky(urls []string, fetch FetchFunc) string {
	ch := make(chan string)
	for _, u := range urls {
		go func() { ch <- fetch(u) }()
	}
	return <-ch
}

// First returns the first answer as well. The buffer has room for every
// result, so each goroutine can send and exit even if nobody receives.
func First(urls []string, fetch FetchFunc) string {
	ch := make(chan string, len(urls))
	for _, u := range urls {
		go func() { ch <- fetch(u) }()
	}
	return <-ch
}

func fakeFetch(url string) string {
	time.Sleep(10 * time.Millisecond)
	return "200 OK from " + url
}

func main() {
	mirrors := []string{"https://a.example", "https://b.example", "https://c.example"}

	before := runtime.NumGoroutine()
	for range 100 {
		FirstLeaky(mirrors, fakeFetch)
	}
	time.Sleep(50 * time.Millisecond)
	fmt.Println("FirstLeaky: goroutines left over:", runtime.NumGoroutine()-before)

	before = runtime.NumGoroutine()
	for range 100 {
		First(mirrors, fakeFetch)
	}
	time.Sleep(50 * time.Millisecond)
	fmt.Println("First:      goroutines left over:", runtime.NumGoroutine()-before)
}
