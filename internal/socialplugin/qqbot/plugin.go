package qqbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/social"
	"qorvexus/internal/socialplugin"
	"qorvexus/internal/socialpluginregistry"
)

const (
	defaultAPIBaseURL   = "https://api.sgroup.qq.com"
	defaultTokenBaseURL = "https://bots.qq.com"
)

type Plugin struct{}

type Connector struct {
	appID         string
	clientSecret  string
	apiBaseURL    string
	tokenBaseURL  string
	defaultTarget string
	httpClient    *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

type targetKind string

const (
	targetC2C     targetKind = "c2c"
	targetGroup   targetKind = "group"
	targetChannel targetKind = "channel"
)

type qqBotTokenRequest struct {
	AppID        string `json:"appId"`
	ClientSecret string `json:"clientSecret"`
}

type qqBotTokenResponse struct {
	Code        int    `json:"code"`
	Message     string `json:"message"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (r *qqBotTokenResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		Code        int             `json:"code"`
		Message     string          `json:"message"`
		AccessToken string          `json:"access_token"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Code = raw.Code
	r.Message = raw.Message
	r.AccessToken = raw.AccessToken
	if len(raw.ExpiresIn) == 0 {
		return nil
	}
	var asInt int64
	if err := json.Unmarshal(raw.ExpiresIn, &asInt); err == nil {
		r.ExpiresIn = asInt
		return nil
	}
	var asString string
	if err := json.Unmarshal(raw.ExpiresIn, &asString); err != nil {
		return err
	}
	var parsed int64
	if _, err := fmt.Sscan(asString, &parsed); err != nil {
		return err
	}
	r.ExpiresIn = parsed
	return nil
}

func New() *Plugin { return &Plugin{} }

func init() {
	socialpluginregistry.Register(func() socialplugin.Plugin { return New() })
}

func (p *Plugin) Channel() string { return "qqbot" }

func (p *Plugin) Setup(cfg config.SocialConfig, registry *social.Registry, _ func(context.Context, social.Envelope) error) ([]socialplugin.BackgroundRunner, error) {
	appID := strings.TrimSpace(cfg.QQBot.AppID)
	clientSecret := strings.TrimSpace(cfg.QQBot.ClientSecret)
	if appID == "" || clientSecret == "" {
		return nil, nil
	}
	registry.Register(NewConnector(appID, clientSecret, cfg.QQBot.APIBaseURL, cfg.QQBot.TokenBaseURL, cfg.QQBot.DefaultTarget))
	return nil, nil
}

func NewConnector(appID string, clientSecret string, apiBaseURL string, tokenBaseURL string, defaultTarget string) *Connector {
	if strings.TrimSpace(apiBaseURL) == "" {
		apiBaseURL = defaultAPIBaseURL
	}
	if strings.TrimSpace(tokenBaseURL) == "" {
		tokenBaseURL = defaultTokenBaseURL
	}
	return &Connector{
		appID:         strings.TrimSpace(appID),
		clientSecret:  strings.TrimSpace(clientSecret),
		apiBaseURL:    strings.TrimRight(apiBaseURL, "/"),
		tokenBaseURL:  strings.TrimRight(tokenBaseURL, "/"),
		defaultTarget: strings.TrimSpace(defaultTarget),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Connector) Name() string { return "qqbot" }

func (c *Connector) Send(ctx context.Context, msg social.OutboundMessage) (string, error) {
	target := strings.TrimSpace(msg.ThreadID)
	if target == "" {
		target = strings.TrimSpace(msg.Recipient)
	}
	if target == "" {
		target = c.defaultTarget
	}
	kind, targetID, err := parseTarget(target)
	if err != nil {
		return "", err
	}
	accessToken, err := c.getAccessToken(ctx)
	if err != nil {
		return "", err
	}

	payload := map[string]any{
		"content":  msg.Text,
		"msg_type": 0,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+endpointFor(kind, targetID), bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "QQBot "+accessToken)
	req.Header.Set("X-Union-Appid", c.appID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("qqbot send returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return fmt.Sprintf("sent qqbot %s message to %s", kind, targetID), nil
}

func parseTarget(raw string) (targetKind, string, error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("qqbot requires thread_id, recipient, or social.qqbot.default_target")
	}
	target = strings.TrimPrefix(target, "qqbot:")
	parts := strings.SplitN(target, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid qqbot target %q; expected qqbot:c2c:OPENID, qqbot:group:GROUP_OPENID, or qqbot:channel:CHANNEL_ID", raw)
	}
	kind := targetKind(strings.ToLower(strings.TrimSpace(parts[0])))
	targetID := strings.TrimSpace(parts[1])
	if targetID == "" {
		return "", "", fmt.Errorf("invalid qqbot target %q; missing target id", raw)
	}
	switch kind {
	case targetC2C, targetGroup, targetChannel:
		return kind, targetID, nil
	default:
		return "", "", fmt.Errorf("unsupported qqbot target kind %q", parts[0])
	}
}

func endpointFor(kind targetKind, targetID string) string {
	switch kind {
	case targetC2C:
		return "/v2/users/" + targetID + "/messages"
	case targetGroup:
		return "/v2/groups/" + targetID + "/messages"
	default:
		return "/channels/" + targetID + "/messages"
	}
}

func (c *Connector) getAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Until(c.tokenExpiry) > 30*time.Second {
		return c.accessToken, nil
	}

	payload := qqBotTokenRequest{
		AppID:        c.appID,
		ClientSecret: c.clientSecret,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenBaseURL+"/app/getAppAccessToken", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("qqbot token request returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var parsed qqBotTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.Code != 0 {
		return "", fmt.Errorf("qqbot token request failed: %d %s", parsed.Code, parsed.Message)
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return "", fmt.Errorf("qqbot token response did not contain access_token")
	}
	expiresIn := parsed.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	c.accessToken = parsed.AccessToken
	c.tokenExpiry = time.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
	return c.accessToken, nil
}
