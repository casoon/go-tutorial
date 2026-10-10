// Command slices shows arrays versus slices, len and cap, how append
// grows a slice and the aliasing trap that follows from it.
//
//	go run ./beispiele/03-die-sprache-kompakt/slices
package main

import (
	"fmt"
	"slices"
)

func main() {
	// An array has a fixed length that is part of its type.
	// Assigning copies all elements.
	codes := [3]int{200, 200, 503}
	copied := codes
	copied[0] = 500
	fmt.Println("array:", codes, copied)

	// A slice is a view on a backing array: pointer, length, capacity.
	var durations []int // nil slice: len 0, cap 0, append works
	for i := range 6 {
		durations = append(durations, (i+1)*100)
		fmt.Printf("len=%d cap=%d %v\n", len(durations), cap(durations), durations)
	}

	all := []int{200, 200, 503, 200}

	// The trap: a sub-slice shares the backing array. append writes into
	// the free capacity and overwrites a[2].
	a := slices.Clone(all)
	prefix := append(a[:2], 404)
	fmt.Println("shared:  ", prefix, a)

	// The fix: a full slice expression limits the capacity, so append
	// has to allocate a new array and the original stays untouched.
	b := slices.Clone(all)
	limited := append(b[:2:2], 404)
	fmt.Println("isolated:", limited, b)
}
