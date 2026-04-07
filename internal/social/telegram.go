package social

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type TelegramConnector struct {
	token      string
	httpClient *http.Client
}

func NewTelegramConnector(token string) *TelegramConnector {
	return &TelegramConnector{
		token: token,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *TelegramConnector) Name() string { return "telegram" }

func (c *TelegramConnector) Send(ctx context.Context, msg OutboundMessage) (string, error) {
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
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.telegram.org/bot"+c.token+"/sendMessage",
		bytes.NewReader(raw),
	)
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

func TelegramWebhookURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + ensureLeadingSlash(path)
}

func ensureLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

type TelegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *TelegramMessage `json:"message,omitempty"`
	Edited   *TelegramMessage `json:"edited_message,omitempty"`
	Channel  *TelegramMessage `json:"channel_post,omitempty"`
}

type TelegramMessage struct {
	MessageID int64         `json:"message_id"`
	Text      string        `json:"text,omitempty"`
	Chat      TelegramChat  `json:"chat"`
	From      *TelegramUser `json:"from,omitempty"`
}

type TelegramChat struct {
	ID    int64  `json:"id"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
}

type TelegramUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

func TelegramEnvelope(update TelegramUpdate) (Envelope, bool) {
	message := update.Message
	if message == nil {
		message = update.Edited
	}
	if message == nil {
		message = update.Channel
	}
	if message == nil || strings.TrimSpace(message.Text) == "" {
		return Envelope{}, false
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
	return Envelope{
		ID:         fmt.Sprintf("telegram-%d-%d", update.UpdateID, message.MessageID),
		Channel:    "telegram",
		ThreadID:   fmt.Sprintf("%d", message.Chat.ID),
		SenderID:   senderID,
		SenderName: senderName,
		Text:       message.Text,
	}, true
}
