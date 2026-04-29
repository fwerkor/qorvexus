package model

import (
	"context"
	"sync"
	"testing"
	"time"
)

type blockingClient struct {
	started chan struct{}
	release chan struct{}
}

func (c *blockingClient) Complete(_ context.Context, _ CompletionRequest) (*CompletionResponse, error) {
	c.started <- struct{}{}
	<-c.release
	return &CompletionResponse{}, nil
}

func TestQueuedClientSerializesCompletionRequests(t *testing.T) {
	inner := &blockingClient{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	client := NewQueuedClient(inner, &sync.Mutex{})
	done := make(chan struct{}, 2)

	go func() {
		_, _ = client.Complete(context.Background(), CompletionRequest{})
		done <- struct{}{}
	}()
	<-inner.started

	go func() {
		_, _ = client.Complete(context.Background(), CompletionRequest{})
		done <- struct{}{}
	}()

	select {
	case <-inner.started:
		t.Fatal("expected second completion to wait behind the first")
	case <-time.After(30 * time.Millisecond):
	}

	inner.release <- struct{}{}
	<-done
	<-inner.started
	inner.release <- struct{}{}
	<-done
}
