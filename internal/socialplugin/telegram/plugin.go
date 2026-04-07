package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/social"
	"qorvexus/internal/socialplugin"
	"qorvexus/internal/socialpluginregistry"
)

type Plugin struct{}

type Connector struct {
	token      string
	httpClient *http.Client
}

type WebhookAdapter struct {
	path          string
	webhookSecret string
}

type Poller struct {
	token          string
	apiBaseURL     string
	httpClient     *http.Client
	timeoutSeconds int
	idleDelay      time.Duration
	offset         int64
}

type pollingRunner struct {
	poller *Poller
	handle func(context.Context, social.Envelope) error
}

func New() *Plugin { return &Plugin{} }

func init() {
	socialpluginregistry.Register(func() socialplugin.Plugin { return New() })
}

func (p *Plugin) Channel() string { return "telegram" }

func (p *Plugin) Setup(cfg config.SocialConfig, registry *social.Registry, handle func(context.Context, social.Envelope) error) ([]socialplugin.BackgroundRunner, error) {
	token := strings.TrimSpace(cfg.Telegram.BotToken)
	if token != "" {
		registry.Register(NewConnector(token))
	}

	var runners []socialplugin.BackgroundRunner
	switch strings.ToLower(strings.TrimSpace(cfg.Telegram.Mode)) {
	case "", "polling":
		if token != "" {
			runners = append(runners, &pollingRunner{
				poller: NewPoller(token, cfg.Telegram.PollTimeoutSeconds, time.Duration(cfg.Telegram.PollIntervalSeconds)*time.Second),
				handle: handle,
			})
		}
	case "webhook":
		registry.RegisterWebhook(NewWebhookAdapter(cfg.Telegram.WebhookPath, cfg.Telegram.WebhookSecret))
	default:
		return nil, fmt.Errorf("unsupported telegram mode %q", cfg.Telegram.Mode)
	}
	return runners, nil
}

func NewConnector(token string) *Connector {
	return &Connector{
		token: token,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Connector) Name() string { return "telegram" }

func NewPoller(token string, timeoutSeconds int, idleDelay time.Duration) *Poller {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	if idleDelay < 0 {
		idleDelay = 0
	}
	return &Poller{
		token:          token,
		apiBaseURL:     "https://api.telegram.org",
		timeoutSeconds: timeoutSeconds,
		idleDelay:      idleDelay,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSeconds+15) * time.Second,
		},
	}
}

func (r *pollingRunner) Name() string { return "telegram-polling" }

func (r *pollingRunner) Run(ctx context.Context) error {
	return r.poller.Run(ctx, r.handle)
}

func NewWebhookAdapter(path string, webhookSecret string) *WebhookAdapter {
	return &WebhookAdapter{
		path:          ensureLeadingSlash(path),
		webhookSecret: strings.TrimSpace(webhookSecret),
	}
}

func (a *WebhookAdapter) Name() string { return "telegram" }

func (a *WebhookAdapter) Path() string { return a.path }

func (a *WebhookAdapter) ParseWebhook(r *http.Request) (social.Envelope, bool, error) {
	if r.Method != http.MethodPost {
		return social.Envelope{}, false, fmt.Errorf("method not allowed")
	}
	if a.webhookSecret != "" {
		secret := strings.TrimSpace(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"))
		if secret == "" || secret != a.webhookSecret {
			return social.Envelope{}, false, fmt.Errorf("invalid telegram secret token")
		}
	}
	var update Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		return social.Envelope{}, false, err
	}
	env, ok := Envelope(update)
	return env, ok, nil
}

func (c *Connector) Send(ctx context.Context, msg social.OutboundMessage) (string, error) {
	chatID := strings.TrimSpace(msg.ThreadID)
	if chatID == "" {
		chatID = strings.TrimSpace(msg.Recipient)
	}
	if chatID == "" {
		return "", fmt.Errorf("telegram requires thread_id or recipient as chat id")
	}
	payload := map[string]any{
		"chat_id": chatID,
		"text":    msg.Text,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+c.token+"/sendMessage", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("telegram sendMessage returned %s", resp.Status)
	}
	return fmt.Sprintf("sent telegram message to %s", chatID), nil
}

func WebhookURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + ensureLeadingSlash(path)
}

func (p *Poller) Run(ctx context.Context, handle func(context.Context, social.Envelope) error) error {
	if err := p.deleteWebhook(ctx); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		updates, err := p.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if p.idleDelay > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(p.idleDelay):
				}
			}
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= p.offset {
				p.offset = update.UpdateID + 1
			}
			env, ok := Envelope(update)
			if !ok {
				continue
			}
			if err := handle(ctx, env); err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
		}
		if len(updates) == 0 && p.idleDelay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(p.idleDelay):
			}
		}
	}
}

func (p *Poller) deleteWebhook(ctx context.Context) error {
	form := url.Values{}
	form.Set("drop_pending_updates", "false")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.apiBaseURL, "/")+"/bot"+p.token+"/deleteWebhook", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed apiResponse[bool]
	if err := json.Unmarshal(raw, &parsed); err == nil && !parsed.OK {
		return fmt.Errorf("telegram deleteWebhook failed: %s", parsed.Description)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram deleteWebhook returned %s", resp.Status)
	}
	return nil
}

func (p *Poller) getUpdates(ctx context.Context) ([]Update, error) {
	form := url.Values{}
	form.Set("timeout", fmt.Sprintf("%d", p.timeoutSeconds))
	form.Set("offset", fmt.Sprintf("%d", p.offset))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.apiBaseURL, "/")+"/bot"+p.token+"/getUpdates", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram getUpdates returned %s", resp.Status)
	}
	var parsed apiResponse[[]Update]
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if !parsed.OK {
		return nil, fmt.Errorf("telegram getUpdates failed: %s", parsed.Description)
	}
	return parsed.Result, nil
}

func ensureLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
	Edited   *Message `json:"edited_message,omitempty"`
	Channel  *Message `json:"channel_post,omitempty"`
}

type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description,omitempty"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text,omitempty"`
	Chat      Chat   `json:"chat"`
	From      *User  `json:"from,omitempty"`
}

type Chat struct {
	ID    int64  `json:"id"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
}

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

func Envelope(update Update) (social.Envelope, bool) {
	message := update.Message
	if message == nil {
		message = update.Edited
	}
	if message == nil {
		message = update.Channel
	}
	if message == nil || strings.TrimSpace(message.Text) == "" {
		return social.Envelope{}, false
	}
	senderID := ""
	senderName := strings.TrimSpace(message.Chat.Title)
	if message.From != nil {
		senderID = fmt.Sprintf("%d", message.From.ID)
		name := strings.TrimSpace(strings.Join([]string{message.From.FirstName, message.From.LastName}, " "))
		if name == "" {
			name = strings.TrimSpace(message.From.Username)
		}
		if name != "" {
			senderName = name
		}
	}
	if senderName == "" {
		senderName = fmt.Sprintf("chat:%d", message.Chat.ID)
	}
	return social.Envelope{
		ID:         fmt.Sprintf("telegram-%d-%d", update.UpdateID, message.MessageID),
		Channel:    "telegram",
		ThreadID:   fmt.Sprintf("%d", message.Chat.ID),
		SenderID:   senderID,
		SenderName: senderName,
		Text:       message.Text,
	}, true
}
