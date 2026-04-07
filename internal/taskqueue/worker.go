package taskqueue

import (
	"context"
	"time"
)

type Worker struct {
	Queue        *Queue
	PollInterval time.Duration
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_, _ = w.Queue.RunNext(ctx)
		}
	}
}
