package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/social"
	"qorvexus/internal/socialplugin"
	"qorvexus/internal/socialpluginregistry"
)

type Plugin struct{}

type Connector struct {
	token            string
	apiBaseURL       string
	defaultChannelID string
	httpClient       *http.Client
}

type postMessageResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func New() *Plugin { return &Plugin{} }

func init() {
	socialpluginregistry.Register(func() socialplugin.Plugin { return New() })
}

func (p *Plugin) Channel() string { return "slack" }

func (p *Plugin) Setup(cfg config.SocialConfig, registry *social.Registry, _ func(context.Context, social.Envelope) error) ([]socialplugin.BackgroundRunner, error) {
	token := strings.TrimSpace(cfg.Slack.BotToken)
	if token == "" {
		return nil, nil
	}
	registry.Register(NewConnector(token, cfg.Slack.APIBaseURL, cfg.Slack.DefaultChannelID))
	return nil, nil
}

func NewConnector(token string, apiBaseURL string, defaultChannelID string) *Connector {
	if strings.TrimSpace(apiBaseURL) == "" {
		apiBaseURL = "https://slack.com/api"
	}
	return &Connector{
		token:            strings.TrimSpace(token),
		apiBaseURL:       strings.TrimRight(apiBaseURL, "/"),
		defaultChannelID: strings.TrimSpace(defaultChannelID),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Connector) Name() string { return "slack" }

func (c *Connector) Send(ctx context.Context, msg social.OutboundMessage) (string, error) {
	channelID := strings.TrimSpace(msg.ThreadID)
	if channelID == "" {
		channelID = strings.TrimSpace(msg.Recipient)
	}
	if channelID == "" {
		channelID = c.defaultChannelID
	}
	if channelID == "" {
		return "", fmt.Errorf("slack requires thread_id, recipient, or social.slack.default_channel_id")
	}

	payload := map[string]any{
		"channel": channelID,
		"text":    msg.Text,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+"/chat.postMessage", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("slack chat.postMessage returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var parsed postMessageResponse
	if err := json.Unmarshal(body, &parsed); err == nil && !parsed.OK {
		return "", fmt.Errorf("slack chat.postMessage failed: %s", parsed.Error)
	}

	return fmt.Sprintf("sent slack message to %s", channelID), nil
}
