package model

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Recorder struct {
	path string
	mu   sync.Mutex
}

type Record struct {
	Timestamp time.Time           `json:"timestamp"`
	Model     string              `json:"model"`
	Request   CompletionRequest   `json:"request"`
	Response  *CompletionResponse `json:"response,omitempty"`
	Error     string              `json:"error,omitempty"`
}

func NewRecorder(path string) *Recorder {
	return &Recorder{path: path}
}

func (r *Recorder) Wrap(client Client) Client {
	if r == nil {
		return client
	}
	return &recordingClient{inner: client, recorder: r}
}

type recordingClient struct {
	inner    Client
	recorder *Recorder
}

func (c *recordingClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	resp, err := c.inner.Complete(ctx, req)
	record := Record{
		Timestamp: time.Now().UTC(),
		Model:     req.Model,
		Request:   req,
		Response:  resp,
	}
	if err != nil {
		record.Error = err.Error()
	}
	_ = c.recorder.Append(record)
	return resp, err
}

func (r *Recorder) Append(record Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(record)
}
