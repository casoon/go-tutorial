// Command hello is the smallest useful Go program: it reads arguments,
// calls a function and prints the result.
//
//	go run ./beispiele/02-toolchain-und-module/hello
//	go run ./beispiele/02-toolchain-und-module/hello https://example.com
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	fmt.Println(greeting(os.Args[1:]))
}

// greeting builds the message for the given URLs.
func greeting(urls []string) string {
	if len(urls) == 0 {
		return "upwatch: no URLs to watch yet"
	}
	return "upwatch: watching " + strings.Join(urls, ", ")
}
