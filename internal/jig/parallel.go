package jig

import "sync"

// maxWorkers bounds how many entries the parallel passes work on at once;
// most of that work is a git process per entry.
const maxWorkers = 16

// forEachParallel runs fn(i) for every i in [0, n) across a bounded pool of
// goroutines and waits for all of them. fn must only write to per-index data.
func forEachParallel(n int, fn func(int)) {
	workers := maxWorkers
	if n < workers {
		workers = n
	}
	if workers <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	indexes := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indexes {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		indexes <- i
	}
	close(indexes)
	wg.Wait()
}
