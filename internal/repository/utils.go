package repository

import (
	"context"
	"sync"
	"time"
)

func (d *DBStorage) fanOut(doneCh chan struct{}, inputCh chan string, userID string) []chan error {
	numWorkers := 10
	channels := make([]chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		resultCh := make(chan error, 1)
		channels[i] = resultCh

		go func(workerID int) {
			defer close(resultCh)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			const batchSize = 10
			batch := make([]string, 0, batchSize)

			for shortURL := range inputCh {
				batch = append(batch, shortURL)
				if len(batch) < batchSize {
					continue
				}

				err := d.processBatch(ctx, batch, userID)
				select {
				case <-doneCh:
					return
				case resultCh <- err:
				}
				batch = batch[:0]
			}

			if len(batch) > 0 {
				err := d.processBatch(ctx, batch, userID)
				select {
				case <-doneCh:
					return
				case resultCh <- err:
				}
			}
		}(i)
	}

	return channels
}

func (d *DBStorage) fanIn(doneCh chan struct{}, resultChs ...chan error) chan error {
	finalCh := make(chan error)

	var wg sync.WaitGroup

	for _, ch := range resultChs {
		chClosure := ch

		wg.Add(1)
		go func() {
			defer wg.Done()
			for err := range chClosure {
				select {
				case <-doneCh:
					return
				case finalCh <- err:
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(finalCh)
	}()

	return finalCh
}
