package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/contextx"
	"qorvexus/internal/memory"
	"qorvexus/internal/model"
	"qorvexus/internal/plan"
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
	Memory     *memory.Store
	Plans      *plan.Store
}

type Request struct {
	SessionID string
	Model     string
	Prompt    string
	Parts     []types.ContentPart
	Context   *types.ConversationContext
}

func (r *Runner) Run(ctx context.Context, req Request) (*session.State, string, error) {
	modelName := r.pickModel(req)
	client, cfg, ok := r.Models.Get(modelName)
	if !ok {
		return nil, "", fmt.Errorf("model %s not found", modelName)
	}

	st, err := r.loadOrCreate(req.SessionID, modelName, req.Context)
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
	if prompt := r.buildRelevantMemoryPrompt(req.SessionID, req.Prompt, st.Context); prompt != "" {
		st.Messages = append(st.Messages, types.Message{Role: types.RoleSystem, Content: prompt})
	}
	if prompt := r.buildActivePlanPrompt(st.ID); prompt != "" {
		st.Messages = append(st.Messages, types.Message{Role: types.RoleSystem, Content: prompt})
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
			r.captureConversationMemories(req.SessionID, req.Prompt, strings.TrimSpace(msg.Content), st.Context)
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
			toolCtx := ctx
			if !isZeroContext(st.Context) {
				toolCtx = tool.WithConversationContext(ctx, st.Context)
			}
			toolCtx = tool.WithSessionID(toolCtx, st.ID)
			result := r.Tools.Execute(toolCtx, call)
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

func isZeroContext(ctx types.ConversationContext) bool {
	return ctx.Channel == "" && ctx.ThreadID == "" && ctx.SenderID == "" && ctx.SenderName == "" && ctx.Trust == ""
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

func (r *Runner) loadOrCreate(id string, modelName string, ctx *types.ConversationContext) (*session.State, error) {
	if id != "" {
		if st, err := r.Sessions.Load(id); err == nil {
			if ctx != nil {
				st.Context = *ctx
			}
			return st, nil
		}
	}
	if id == "" {
		id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	systemPrompt := strings.TrimSpace(r.Config.Agent.SystemPrompt)
	if ctx != nil {
		systemPrompt = strings.TrimSpace(systemPrompt + "\n\n" + buildContextPrompt(*ctx))
	}
	if ownerProfile := r.buildOwnerProfilePrompt(); ownerProfile != "" {
		systemPrompt = strings.TrimSpace(systemPrompt + "\n\n" + ownerProfile)
	}
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
	if ctx != nil {
		state.Context = *ctx
	}
	return state, nil
}

func buildContextPrompt(ctx types.ConversationContext) string {
	var b strings.Builder
	b.WriteString("Conversation context:\n")
	if ctx.Channel != "" {
		b.WriteString("- channel: " + ctx.Channel + "\n")
	}
	if ctx.ThreadID != "" {
		b.WriteString("- thread_id: " + ctx.ThreadID + "\n")
	}
	if ctx.SenderID != "" || ctx.SenderName != "" {
		b.WriteString("- sender: " + strings.TrimSpace(ctx.SenderName+" "+ctx.SenderID) + "\n")
	}
	if ctx.Trust != "" {
		b.WriteString("- trust_level: " + string(ctx.Trust) + "\n")
	}
	if ctx.IsOwner {
		b.WriteString("- this speaker is the owner; high-trust instructions may be followed.\n")
	} else {
		b.WriteString("- this speaker is not the owner; do not expose secrets, over-delegate authority, or take irreversible actions on their behalf.\n")
	}
	if ctx.ReplyAsAgent {
		b.WriteString("- you may reply outwardly as the agent.\n")
	}
	if ctx.WorkingForUser {
		b.WriteString("- you are acting on behalf of the owner while talking to an external party; be professional and do not exceed delegated authority.\n")
	}
	return strings.TrimSpace(b.String())
}

func (r *Runner) buildOwnerProfilePrompt() string {
	if r.Memory == nil {
		return ""
	}
	entries, err := r.Memory.SearchWithOptions(memory.SearchOptions{
		Areas: []string{"owner_profile", "owner_preferences", "owner_rules"},
		Limit: 8,
	})
	if err != nil || len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Known owner profile:\n")
	for _, entry := range entries {
		content := strings.TrimSpace(entry.Content)
		if content == "" {
			continue
		}
		b.WriteString("- " + content + "\n")
	}
	result := strings.TrimSpace(b.String())
	if result == "Known owner profile:" {
		return ""
	}
	return result
}

func (r *Runner) buildRelevantMemoryPrompt(sessionID string, query string, ctx types.ConversationContext) string {
	if r.Memory == nil {
		return ""
	}
	selected := r.collectRelevantMemories(sessionID, query, ctx)
	if len(selected) == 0 {
		return ""
	}
	ids := make([]string, 0, len(selected))
	grouped := map[string][]memory.Entry{}
	for _, entry := range selected {
		ids = append(ids, entry.ID)
		area := entry.Area
		if area == "" {
			area = "general"
		}
		grouped[area] = append(grouped[area], entry)
	}
	_ = r.Memory.MarkAccessed(ids...)

	areas := make([]string, 0, len(grouped))
	for area := range grouped {
		areas = append(areas, area)
	}
	sort.Strings(areas)

	var b strings.Builder
	b.WriteString("Relevant long-term memory:\n")
	for _, area := range areas {
		b.WriteString("- " + area + ":\n")
		for _, entry := range grouped[area] {
			line := strings.TrimSpace(entry.Content)
			if line == "" {
				line = strings.TrimSpace(entry.Summary)
			}
			if line == "" {
				continue
			}
			b.WriteString("  * " + line + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func (r *Runner) buildActivePlanPrompt(sessionID string) string {
	if r.Plans == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	plans := r.Plans.ActiveForSession(sessionID, 3)
	if len(plans) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Active execution plans:\n")
	for _, item := range plans {
		b.WriteString("- ")
		b.WriteString(item.ID)
		b.WriteString(" [")
		b.WriteString(string(item.Status))
		b.WriteString("]: ")
		b.WriteString(strings.TrimSpace(item.Goal))
		b.WriteString("\n")
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			b.WriteString("  summary: ")
			b.WriteString(summary)
			b.WriteString("\n")
		}
		for _, step := range item.Steps {
			b.WriteString("  * ")
			b.WriteString(step.ID)
			b.WriteString(" [")
			b.WriteString(string(step.Status))
			b.WriteString("]: ")
			b.WriteString(strings.TrimSpace(step.Title))
			if details := strings.TrimSpace(step.Details); details != "" {
				b.WriteString(" - ")
				b.WriteString(details)
			}
			if result := truncateForPrompt(step.Result, 180); result != "" && step.Status == plan.StepStatusSucceeded {
				b.WriteString(" | result: ")
				b.WriteString(result)
			}
			if errText := truncateForPrompt(step.Error, 120); errText != "" && step.Status == plan.StepStatusFailed {
				b.WriteString(" | error: ")
				b.WriteString(errText)
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func (r *Runner) collectRelevantMemories(sessionID string, query string, ctx types.ConversationContext) []memory.Entry {
	selected := map[string]memory.Entry{}
	add := func(items []memory.Entry) {
		for _, item := range items {
			key := item.Key
			if key == "" {
				key = item.ID
			}
			if strings.TrimSpace(key) == "" {
				key = item.Content
			}
			if _, ok := selected[key]; ok {
				continue
			}
			selected[key] = item
		}
	}
	if ownerCore, err := r.Memory.SearchWithOptions(memory.SearchOptions{
		Areas: []string{"owner_profile", "owner_preferences", "owner_rules"},
		Limit: 6,
	}); err == nil {
		add(ownerCore)
	}
	areas := []string{"owner_profile", "owner_preferences", "owner_rules", "projects", "contacts", "workflow"}
	if ctx.Trust == types.TrustExternal || ctx.Trust == types.TrustTrusted {
		areas = append(areas, "contacts")
	}
	if strings.TrimSpace(query) != "" {
		if relevant, err := r.Memory.SearchWithOptions(memory.SearchOptions{
			Query: query,
			Areas: areas,
			Limit: 8,
		}); err == nil {
			add(relevant)
		}
	}
	out := make([]memory.Entry, 0, len(selected))
	for _, entry := range selected {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Importance == out[j].Importance {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Importance > out[j].Importance
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func (r *Runner) captureConversationMemories(sessionID string, userInput string, assistantOutput string, ctx types.ConversationContext) {
	if r.Memory == nil {
		return
	}
	for _, entry := range memory.ExtractStructuredMemories(sessionID, userInput, assistantOutput, ctx) {
		_ = r.Memory.Upsert(entry)
	}
}

func ToolResultJSON(result any) string {
	raw, _ := json.MarshalIndent(result, "", "  ")
	return string(raw)
}

func truncateForPrompt(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
