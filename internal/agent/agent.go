package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/contextx"
	"qorvexus/internal/model"
	"qorvexus/internal/session"
	"qorvexus/internal/skill"
	"qorvexus/internal/tool"
	"qorvexus/internal/types"
)

type Runner struct {
	Config     *config.Config
	Models     *model.Registry
	Sessions   *session.Store
	Tools      *tool.Registry
	Skills     []skill.Skill
	Compressor *contextx.Compressor
}

type Request struct {
	SessionID string
	Model     string
	Prompt    string
	Parts     []types.ContentPart
}

func (r *Runner) Run(ctx context.Context, req Request) (*session.State, string, error) {
	modelName := r.pickModel(req)
	client, cfg, ok := r.Models.Get(modelName)
	if !ok {
		return nil, "", fmt.Errorf("model %s not found", modelName)
	}

	st, err := r.loadOrCreate(req.SessionID, modelName)
	if err != nil {
		return nil, "", err
	}
	if req.Prompt != "" || len(req.Parts) > 0 {
		msg := types.Message{Role: types.RoleUser, Content: req.Prompt}
		if len(req.Parts) > 0 {
			msg.Parts = req.Parts
			if req.Prompt != "" {
				msg.Parts = append([]types.ContentPart{{Type: "text", Text: req.Prompt}}, msg.Parts...)
				msg.Content = ""
			}
		}
		st.Messages = append(st.Messages, msg)
	}

	for turn := 0; turn < r.Config.Agent.MaxTurns; turn++ {
		st.Messages, _ = r.Compressor.MaybeCompress(ctx, modelName, st.Messages)
		response, err := client.Complete(ctx, model.CompletionRequest{
			Model:       modelName,
			Messages:    st.Messages,
			Tools:       r.Tools.Definitions(),
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		})
		if err != nil {
			return nil, "", err
		}
		msg := response.Message
		if len(msg.ToolCalls) == 0 {
			st.Messages = append(st.Messages, msg)
			if err := r.Sessions.Save(st); err != nil {
				return nil, "", err
			}
			return st, strings.TrimSpace(msg.Content), nil
		}

		st.Messages = append(st.Messages, types.Message{
			Role:      types.RoleAssistant,
			Content:   msg.Content,
			ToolCalls: msg.ToolCalls,
		})
		for _, call := range msg.ToolCalls {
			result := r.Tools.Execute(ctx, call)
			content := result.Content
			if result.Error {
				content = "ERROR: " + content
			}
			st.Messages = append(st.Messages, types.Message{
				Role:       types.RoleTool,
				Name:       result.Name,
				ToolCallID: result.CallID,
				Content:    content,
			})
		}
	}
	return nil, "", fmt.Errorf("max turns exceeded")
}

func (r *Runner) pickModel(req Request) string {
	modelName := req.Model
	if modelName == "" {
		modelName = r.Config.Agent.DefaultModel
	}
	hasImage := false
	for _, part := range req.Parts {
		if part.Type == "image_url" {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return modelName
	}
	_, cfg, ok := r.Models.Get(modelName)
	if ok && cfg.Vision {
		return modelName
	}
	if fallback := r.Config.Agent.VisionFallbackModel; fallback != "" {
		return fallback
	}
	return modelName
}

func (r *Runner) loadOrCreate(id string, modelName string) (*session.State, error) {
	if id != "" {
		if st, err := r.Sessions.Load(id); err == nil {
			return st, nil
		}
	}
	if id == "" {
		id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	systemPrompt := strings.TrimSpace(r.Config.Agent.SystemPrompt)
	if skills := skill.Prompt(r.Skills); skills != "" {
		systemPrompt = strings.TrimSpace(systemPrompt + "\n\n" + skills)
	}
	msgs := []types.Message{}
	if systemPrompt != "" {
		msgs = append(msgs, types.Message{Role: types.RoleSystem, Content: systemPrompt})
	}
	state := &session.State{
		ID:       id,
		Model:    modelName,
		Messages: msgs,
	}
	return state, nil
}

func ToolResultJSON(result any) string {
	raw, _ := json.MarshalIndent(result, "", "  ")
	return string(raw)
}
