package contextx

import (
	"context"
	"fmt"
	"strings"

	"qorvexus/internal/model"
	"qorvexus/internal/types"
)

type Compressor struct {
	Registry        *model.Registry
	SummarizerModel string
	MaxChars        int
	Threshold       float64
}

const compressedSummaryPrefix = "Compressed conversation summary:"
const preservedUserInputPrefix = "Preserved original user inputs:"
const latestUserInputPrefix = "Latest original user message:"

func (c *Compressor) MaybeCompress(ctx context.Context, sessionModel string, messages []types.Message) ([]types.Message, error) {
	if c.MaxChars <= 0 {
		return messages, nil
	}
	total := 0
	for _, msg := range messages {
		total += len(msg.Content)
		for _, p := range msg.Parts {
			total += len(p.Text) + len(p.ImageURL)
		}
	}
	if float64(total) < float64(c.MaxChars)*c.Threshold {
		return messages, nil
	}
	if recentlyCompressed(messages) && total < int(float64(c.MaxChars)*1.2) {
		return messages, nil
	}

	modelName := c.SummarizerModel
	if modelName == "" {
		modelName = sessionModel
	}
	client, cfg, ok := c.Registry.Get(modelName)
	if !ok {
		return messages, nil
	}

	systemParts := []string{}
	nonSystem := make([]types.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == types.RoleSystem {
			if content := strings.TrimSpace(msg.Content); content != "" {
				systemParts = append(systemParts, content)
			}
			continue
		}
		nonSystem = append(nonSystem, msg)
	}
	if len(nonSystem) < 2 {
		return messages, nil
	}

	latestUser := latestUserMessageText(nonSystem)
	slicePoint := len(nonSystem) / 2
	if latestUserIndex := latestUserMessageIndex(nonSystem); latestUserIndex >= 0 && slicePoint > latestUserIndex {
		slicePoint = latestUserIndex
	}
	if slicePoint <= 0 {
		return messages, nil
	}
	old := nonSystem[:slicePoint]
	var transcript strings.Builder
	for _, msg := range old {
		fmt.Fprintf(&transcript, "%s: %s\n", msg.Role, msg.Content)
	}
	preservedInputs := preservedUserInputs(old, 1200, 8000)
	req := model.CompletionRequest{
		Model: cfg.Model,
		Messages: []types.Message{
			{Role: types.RoleSystem, Content: "Summarize the conversation state for future reasoning. Preserve goals, constraints, decisions, unfinished work, important facts, and the user's original requests. Do not replace user requirements with vague paraphrases when exact wording matters."},
			{Role: types.RoleUser, Content: transcript.String()},
		},
		MaxTokens:   cfg.MaxTokens,
		Temperature: 0.1,
	}
	resp, err := client.Complete(ctx, req)
	if err != nil {
		return messages, nil
	}

	compressed := make([]types.Message, 0, len(systemParts)+1+len(nonSystem[slicePoint:]))
	if len(systemParts) > 0 {
		compressed = append(compressed, types.Message{
			Role:    types.RoleSystem,
			Content: strings.Join(systemParts, "\n\n"),
		})
	}
	summaryContent := compressedSummaryPrefix + "\n" + strings.TrimSpace(resp.Message.Content)
	if latestUser != "" {
		summaryContent = strings.TrimSpace(summaryContent + "\n\n" + latestUserInputPrefix + "\n" + truncateRunes(latestUser, 4000))
	}
	if preservedInputs != "" {
		summaryContent = strings.TrimSpace(summaryContent + "\n\n" + preservedUserInputPrefix + "\n" + preservedInputs)
	}
	compressed = append(compressed, types.Message{
		Role:    types.RoleUser,
		Content: summaryContent,
	})
	compressed = append(compressed, nonSystem[slicePoint:]...)
	return compressed, nil
}

func latestUserMessageIndex(messages []types.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == types.RoleUser {
			text := messageText(messages[i])
			if text != "" && !strings.HasPrefix(strings.TrimSpace(text), compressedSummaryPrefix) {
				return i
			}
		}
	}
	return -1
}

func latestUserMessageText(messages []types.Message) string {
	if idx := latestUserMessageIndex(messages); idx >= 0 {
		return messageText(messages[idx])
	}
	return ""
}

func preservedUserInputs(messages []types.Message, perMessageLimit int, totalLimit int) string {
	if perMessageLimit <= 0 || totalLimit <= 0 {
		return ""
	}
	var b strings.Builder
	count := 0
	for _, msg := range messages {
		if msg.Role != types.RoleUser {
			continue
		}
		text := messageText(msg)
		if text == "" || strings.HasPrefix(strings.TrimSpace(text), compressedSummaryPrefix) {
			continue
		}
		count++
		item := truncateRunes(text, perMessageLimit)
		line := fmt.Sprintf("%d. %s\n", count, item)
		if b.Len()+len(line) > totalLimit {
			remaining := totalLimit - b.Len()
			if remaining > 24 {
				b.WriteString(truncateRunes(line, remaining-14))
				b.WriteString("\n[truncated]\n")
			}
			break
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(b.String())
}

func messageText(msg types.Message) string {
	if strings.TrimSpace(msg.Content) != "" {
		return strings.TrimSpace(msg.Content)
	}
	parts := make([]string, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		if strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func recentlyCompressed(messages []types.Message) bool {
	nonSystemAfterSummary := 0
	found := false
	for _, msg := range messages {
		if msg.Role == types.RoleSystem {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(msg.Content), compressedSummaryPrefix) {
			found = true
			nonSystemAfterSummary = 0
			continue
		}
		if found {
			nonSystemAfterSummary++
		}
	}
	return found && nonSystemAfterSummary < 8
}
