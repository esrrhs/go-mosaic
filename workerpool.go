package main

import "sync"

// WorkerPool is a type-safe, concurrent task worker pool with backpressure.
type WorkerPool[T any] struct {
	jobs chan T
	wg   sync.WaitGroup
}

// NewWorkerPool creates and starts a new WorkerPool with the specified number of workers.
func NewWorkerPool[T any](workers int, buffer int, handler func(T)) *WorkerPool[T] {
	if workers <= 0 {
		workers = 1
	}
	if buffer <= 0 {
		buffer = workers * 2
	}
	wp := &WorkerPool[T]{
		jobs: make(chan T, buffer),
	}
	for i := 0; i < workers; i++ {
		wp.wg.Add(1)
		go func() {
			defer wp.wg.Done()
			for task := range wp.jobs {
				handler(task)
			}
		}()
	}
	return wp
}

// Submit enqueues a task for processing. Blocks if buffer is full, providing backpressure.
func (wp *WorkerPool[T]) Submit(task T) {
	wp.jobs <- task
}

// Stop closes the task queue and waits for all active workers to finish.
func (wp *WorkerPool[T]) Stop() {
	close(wp.jobs)
	wp.wg.Wait()
}
