package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	apiBaseURL string
	httpClient *http.Client
}

const telegramMaxMessageChars = 4096

var telegramParseModes = []string{"MarkdownV2", "Markdown", ""}

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
		token:      token,
		apiBaseURL: "https://api.telegram.org",
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
	chunks := splitTelegramMessage(msg.Text, telegramMaxMessageChars)
	for _, chunk := range chunks {
		if err := c.sendWithFallback(ctx, chatID, chunk); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("sent %d telegram message(s) to %s", len(chunks), chatID), nil
}

func (c *Connector) sendWithFallback(ctx context.Context, chatID string, text string) error {
	var lastErr error
	for idx, parseMode := range telegramParseModes {
		err := c.sendChunk(ctx, chatID, text, parseMode)
		if err == nil {
			return nil
		}
		lastErr = err
		var sendErr *telegramSendError
		if !errors.As(err, &sendErr) || !sendErr.markdownParseFailure() || idx == len(telegramParseModes)-1 {
			return err
		}
	}
	return lastErr
}

func (c *Connector) sendChunk(ctx context.Context, chatID string, text string, parseMode string) error {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if strings.TrimSpace(parseMode) != "" {
		payload["parse_mode"] = parseMode
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.apiBaseURL, "/")+"/bot"+c.token+"/sendMessage", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return readErr
	}
	var parsed apiResponse[json.RawMessage]
	_ = json.Unmarshal(body, &parsed)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &telegramSendError{
			status:      resp.Status,
			description: strings.TrimSpace(parsed.Description),
			raw:         strings.TrimSpace(string(body)),
		}
	}
	if !parsed.OK {
		return &telegramSendError{
			status:      resp.Status,
			description: strings.TrimSpace(parsed.Description),
			raw:         strings.TrimSpace(string(body)),
		}
	}
	return nil
}

func (c *Connector) SendTyping(ctx context.Context, msg social.OutboundMessage) error {
	chatID := strings.TrimSpace(msg.ThreadID)
	if chatID == "" {
		chatID = strings.TrimSpace(msg.Recipient)
	}
	if chatID == "" {
		return fmt.Errorf("telegram requires thread_id or recipient as chat id")
	}
	payload := map[string]any{
		"chat_id": chatID,
		"action":  "typing",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.apiBaseURL, "/")+"/bot"+c.token+"/sendChatAction", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram sendChatAction returned %s", resp.Status)
	}
	return nil
}

func WebhookURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + ensureLeadingSlash(path)
}

type telegramSendError struct {
	status      string
	description string
	raw         string
}

func (e *telegramSendError) Error() string {
	if e == nil {
		return ""
	}
	detail := strings.TrimSpace(e.description)
	if detail == "" {
		detail = strings.TrimSpace(e.raw)
	}
	if detail == "" {
		return fmt.Sprintf("telegram sendMessage returned %s", e.status)
	}
	return fmt.Sprintf("telegram sendMessage returned %s: %s", e.status, detail)
}

func (e *telegramSendError) markdownParseFailure() bool {
	if e == nil {
		return false
	}
	detail := strings.ToLower(strings.TrimSpace(e.description + " " + e.raw))
	return strings.Contains(detail, "parse entities") ||
		strings.Contains(detail, "can't parse") ||
		strings.Contains(detail, "can't find end") ||
		strings.Contains(detail, "unsupported start tag")
}

func splitTelegramMessage(text string, limit int) []string {
	if limit <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	out := make([]string, 0, len(runes)/limit+1)
	for start := 0; start < len(runes); {
		end := start + limit
		if end >= len(runes) {
			out = append(out, string(runes[start:]))
			break
		}
		split := findTelegramSplit(runes, start, end)
		if split <= start {
			split = end
		}
		out = append(out, string(runes[start:split]))
		start = split
	}
	return out
}

func findTelegramSplit(runes []rune, start int, end int) int {
	for i := end - 1; i > start; i-- {
		if runes[i] == '\n' {
			return i + 1
		}
	}
	for i := end - 1; i > start; i-- {
		if runes[i] == ' ' || runes[i] == '\t' {
			return i + 1
		}
	}
	return end
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
		}
		for _, env := range coalesceUpdates(updates) {
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

func coalesceUpdates(updates []Update) []social.Envelope {
	order := make([]string, 0, len(updates))
	grouped := map[string][]social.Envelope{}
	for _, update := range updates {
		env, ok := Envelope(update)
		if !ok {
			continue
		}
		key := envelopeBatchKey(env)
		if _, exists := grouped[key]; !exists {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], env)
	}
	out := make([]social.Envelope, 0, len(order))
	for _, key := range order {
		out = append(out, mergeEnvelopes(grouped[key]))
	}
	return out
}

func envelopeBatchKey(env social.Envelope) string {
	return strings.TrimSpace(env.Channel) + "|" + strings.TrimSpace(env.ThreadID) + "|" + strings.TrimSpace(env.SenderID) + "|" + strings.TrimSpace(env.SenderName)
}

func mergeEnvelopes(batch []social.Envelope) social.Envelope {
	if len(batch) == 0 {
		return social.Envelope{}
	}
	if len(batch) == 1 {
		return batch[0]
	}
	merged := batch[0]
	texts := make([]string, 0, len(batch))
	for i, env := range batch {
		if trimmed := strings.TrimSpace(env.Text); trimmed != "" {
			texts = append(texts, fmt.Sprintf("Message %d:\n%s", i+1, trimmed))
		}
		if len(env.Images) > 0 {
			merged.Images = append(merged.Images, env.Images...)
		}
		if env.ReceivedAt.After(merged.ReceivedAt) {
			merged.ReceivedAt = env.ReceivedAt
		}
		merged.ID = env.ID
		if env.Context.Channel != "" {
			merged.Context = env.Context
		}
	}
	merged.Text = "Multiple inbound messages arrived before you replied. Treat them as one turn and answer once.\n\n" + strings.Join(texts, "\n\n")
	return merged
}
