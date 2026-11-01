// Package parallel runs work a few at a time, so one pane or file cannot hold up the rest.
package parallel

import (
	"sync"
)

const maxConcurrent = 8

// ForEach runs work for 0..n-1, a few at a time.
func ForEach(n int, work func(i int)) {
	limit := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		limit <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-limit }()
			work(i)
		}()
	}
	wg.Wait()
}
