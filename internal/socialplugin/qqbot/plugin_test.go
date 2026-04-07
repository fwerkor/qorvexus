package qqbot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/social"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		input   string
		kind    targetKind
		id      string
		wantErr bool
	}{
		{input: "qqbot:c2c:user-openid", kind: targetC2C, id: "user-openid"},
		{input: "qqbot:group:group-openid", kind: targetGroup, id: "group-openid"},
		{input: "qqbot:channel:channel-id", kind: targetChannel, id: "channel-id"},
		{input: "c2c:user-openid", kind: targetC2C, id: "user-openid"},
		{input: "qqbot:unknown:x", wantErr: true},
		{input: "channel-only", wantErr: true},
	}

	for _, tt := range tests {
		kind, id, err := parseTarget(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseTarget(%q): %v", tt.input, err)
		}
		if kind != tt.kind || id != tt.id {
			t.Fatalf("parseTarget(%q) = (%q, %q)", tt.input, kind, id)
		}
	}
}

func TestPluginSetupRegistersConnector(t *testing.T) {
	registry := social.NewRegistry()
	plugin := New()
	cfg := config.SocialConfig{
		QQBot: config.QQBotConfig{
			AppID:        "123",
			ClientSecret: "secret",
		},
	}
	runners, err := plugin.Setup(cfg, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runners) != 0 {
		t.Fatalf("expected no background runners, got %d", len(runners))
	}
	if got := registry.List(); len(got) != 1 || got[0] != "qqbot" {
		t.Fatalf("expected qqbot connector to register, got %#v", got)
	}
}

func TestConnectorSendUsesTokenAndTargetEndpoint(t *testing.T) {
	var tokenRequests int
	var seenAuth string
	var seenAppID string
	var seenPath string
	var seenBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			tokenRequests++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"access_token":"qq-access-token","expires_in":"3600"}`))
		case "/v2/users/user-1/messages":
			seenAuth = r.Header.Get("Authorization")
			seenAppID = r.Header.Get("X-Union-Appid")
			seenPath = r.URL.Path
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &seenBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg-1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	connector := NewConnector("app-1", "secret-1", server.URL, server.URL, "")
	result, err := connector.Send(context.Background(), social.OutboundMessage{
		Channel:   "qqbot",
		Recipient: "qqbot:c2c:user-1",
		Text:      "hello from qorvexus",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "c2c") {
		t.Fatalf("expected c2c send result, got %q", result)
	}
	if tokenRequests != 1 {
		t.Fatalf("expected one token request, got %d", tokenRequests)
	}
	if seenAuth != "QQBot qq-access-token" {
		t.Fatalf("expected QQBot auth header, got %q", seenAuth)
	}
	if seenAppID != "app-1" {
		t.Fatalf("expected X-Union-Appid header, got %q", seenAppID)
	}
	if seenPath != "/v2/users/user-1/messages" {
		t.Fatalf("expected c2c endpoint, got %q", seenPath)
	}
	if seenBody["content"] != "hello from qorvexus" {
		t.Fatalf("expected content payload, got %#v", seenBody)
	}
}

func TestConnectorUsesDefaultTargetAndCachesToken(t *testing.T) {
	var tokenRequests int
	var sendRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			tokenRequests++
			_, _ = w.Write([]byte(`{"code":0,"access_token":"qq-access-token","expires_in":"3600"}`))
		case "/channels/channel-1/messages":
			sendRequests++
			_, _ = w.Write([]byte(`{"id":"msg-1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	connector := NewConnector("app-1", "secret-1", server.URL, server.URL, "qqbot:channel:channel-1")
	for i := 0; i < 2; i++ {
		if _, err := connector.Send(context.Background(), social.OutboundMessage{
			Channel: "qqbot",
			Text:    "status update",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if tokenRequests != 1 {
		t.Fatalf("expected cached token to be reused, got %d token requests", tokenRequests)
	}
	if sendRequests != 2 {
		t.Fatalf("expected two send requests, got %d", sendRequests)
	}
}
