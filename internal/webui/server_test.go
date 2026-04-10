package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qorvexus/internal/session"
	"qorvexus/internal/social"
	"qorvexus/internal/socialplugin/telegram"
	"qorvexus/internal/taskqueue"
)

type blockingSocialApp struct {
	called chan social.Envelope
	block  chan struct{}
}

func (a *blockingSocialApp) Status() Status { return Status{} }

func (a *blockingSocialApp) RunPrompt(context.Context, string, string, string) (string, error) {
	return "", nil
}

func (a *blockingSocialApp) ListSessions() ([]session.State, error) { return nil, nil }

func (a *blockingSocialApp) ListQueue() []taskqueue.Task { return nil }

func (a *blockingSocialApp) SearchMemory(string, int) (string, error) { return "", nil }

func (a *blockingSocialApp) ListSelfImprovements(context.Context, int) (string, error) {
	return "", nil
}

func (a *blockingSocialApp) ListRecentSocial(context.Context, int) (string, error) { return "", nil }

func (a *blockingSocialApp) ListSocialConnectors(context.Context) (string, error) { return "", nil }

func (a *blockingSocialApp) ListCommitments(context.Context, int) (string, error) { return "", nil }

func (a *blockingSocialApp) CommitmentSummary(context.Context) (string, error) { return "", nil }

func (a *blockingSocialApp) ScanCommitments(context.Context) (string, error) { return "", nil }

func (a *blockingSocialApp) ListAudit(context.Context, int) (string, error) { return "", nil }

func (a *blockingSocialApp) ListPlans(context.Context, int, string) (string, error) { return "", nil }

func (a *blockingSocialApp) GetPlan(context.Context, string) (string, error) { return "", nil }

func (a *blockingSocialApp) AdvancePlan(context.Context, string, int) (string, error) {
	return "", nil
}

func (a *blockingSocialApp) MineSelfImprovements(context.Context, int) (string, error) {
	return "", nil
}

func (a *blockingSocialApp) CaptureSelfImprovement(context.Context, string, string, string, bool, string) (string, error) {
	return "", nil
}

func (a *blockingSocialApp) LoadConfigText() (string, error) { return "", nil }

func (a *blockingSocialApp) SocialWebhookAdapters() []social.WebhookAdapter {
	return []social.WebhookAdapter{telegram.NewWebhookAdapter("/webhooks/telegram", "secret")}
}

func (a *blockingSocialApp) SaveConfigText(string) error { return nil }

func (a *blockingSocialApp) HandleSocialEnvelope(_ context.Context, env social.Envelope) (string, error) {
	a.called <- env
	<-a.block
	return "ok", nil
}

func (a *blockingSocialApp) RetryQueueTask(context.Context, string) (string, error) { return "", nil }

func (a *blockingSocialApp) UpdateCommitmentStatus(context.Context, string, string) (string, error) {
	return "", nil
}

func (a *blockingSocialApp) UpdateSelfImprovementStatus(context.Context, string, string) (string, error) {
	return "", nil
}

func (a *blockingSocialApp) RequestRuntimeRestart(context.Context, string) (string, error) {
	return "", nil
}

func (a *blockingSocialApp) ApplySelfUpdate(context.Context, bool, string) (string, error) {
	return "", nil
}

func TestHandleSocialWebhookReturnsImmediatelyAndProcessesAsync(t *testing.T) {
	app := &blockingSocialApp{
		called: make(chan social.Envelope, 1),
		block:  make(chan struct{}),
	}
	server, err := NewServer(app)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(`{
		"update_id": 1,
		"message": {
			"message_id": 2,
			"text": "hello",
			"chat": {"id": 123, "type": "private"},
			"from": {"id": 456, "first_name": "Ada"}
		}
	}`))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected webhook handler to return without waiting for social processing")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("expected ok response, got %s", rec.Body.String())
	}

	select {
	case env := <-app.called:
		if env.Channel != "telegram" || env.ThreadID != "123" || env.SenderID != "456" {
			t.Fatalf("unexpected envelope: %+v", env)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected async social processing to start")
	}

	close(app.block)
}
