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

	slicePoint := len(nonSystem) / 2
	old := nonSystem[:slicePoint]
	var transcript strings.Builder
	for _, msg := range old {
		fmt.Fprintf(&transcript, "%s: %s\n", msg.Role, msg.Content)
	}
	req := model.CompletionRequest{
		Model: cfg.Model,
		Messages: []types.Message{
			{Role: types.RoleSystem, Content: "Summarize the conversation state for future reasoning. Preserve goals, constraints, decisions, unfinished work, and important facts."},
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
	compressed = append(compressed, types.Message{
		Role:    types.RoleUser,
		Content: compressedSummaryPrefix + "\n" + strings.TrimSpace(resp.Message.Content),
	})
	compressed = append(compressed, nonSystem[slicePoint:]...)
	return compressed, nil
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
