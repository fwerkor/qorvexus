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

func (c *Compressor) MaybeCompress(ctx context.Context, sessionModel string, messages []types.Message) ([]types.Message, error) {
	if c.MaxChars <= 0 || len(messages) < 6 {
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

	modelName := c.SummarizerModel
	if modelName == "" {
		modelName = sessionModel
	}
	client, cfg, ok := c.Registry.Get(modelName)
	if !ok {
		return messages, nil
	}

	slicePoint := len(messages) / 2
	old := messages[:slicePoint]
	var transcript strings.Builder
	for _, msg := range old {
		fmt.Fprintf(&transcript, "%s: %s\n", msg.Role, msg.Content)
	}
	req := model.CompletionRequest{
		Model: modelName,
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

	compressed := []types.Message{
		{
			Role:    types.RoleSystem,
			Content: "Compressed conversation summary:\n" + strings.TrimSpace(resp.Message.Content),
		},
	}
	compressed = append(compressed, messages[slicePoint:]...)
	return compressed, nil
}
