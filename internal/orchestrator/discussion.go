package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"qorvexus/internal/model"
	"qorvexus/internal/types"
)

type Discussion struct {
	Registry *model.Registry
}

func (d *Discussion) Run(ctx context.Context, prompt string, panel []string, synthesisModel string) (string, error) {
	type result struct {
		model string
		text  string
		err   error
	}
	ch := make(chan result, len(panel))
	var wg sync.WaitGroup
	for _, modelName := range panel {
		modelName := modelName
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, cfg, ok := d.Registry.Get(modelName)
			if !ok {
				ch <- result{model: modelName, err: fmt.Errorf("model %s not found", modelName)}
				return
			}
			resp, err := client.Complete(ctx, model.CompletionRequest{
				Model: modelName,
				Messages: []types.Message{
					{Role: types.RoleSystem, Content: "Answer as an expert collaborator. Be concise but useful."},
					{Role: types.RoleUser, Content: prompt},
				},
				MaxTokens:   cfg.MaxTokens,
				Temperature: cfg.Temperature,
			})
			if err != nil {
				ch <- result{model: modelName, err: err}
				return
			}
			ch <- result{model: modelName, text: resp.Message.Content}
		}()
	}
	wg.Wait()
	close(ch)

	var debate strings.Builder
	for item := range ch {
		if item.err != nil {
			fmt.Fprintf(&debate, "[%s error] %v\n", item.model, item.err)
			continue
		}
		fmt.Fprintf(&debate, "[%s]\n%s\n\n", item.model, item.text)
	}
	if synthesisModel == "" {
		return strings.TrimSpace(debate.String()), nil
	}
	client, cfg, ok := d.Registry.Get(synthesisModel)
	if !ok {
		return strings.TrimSpace(debate.String()), nil
	}
	resp, err := client.Complete(ctx, model.CompletionRequest{
		Model: synthesisModel,
		Messages: []types.Message{
			{Role: types.RoleSystem, Content: "Synthesize the multi-model discussion into a practical answer. Note disagreements and recommend a path."},
			{Role: types.RoleUser, Content: debate.String()},
		},
		MaxTokens:   cfg.MaxTokens,
		Temperature: 0.2,
	})
	if err != nil {
		return strings.TrimSpace(debate.String()), nil
	}
	return strings.TrimSpace(resp.Message.Content), nil
}
