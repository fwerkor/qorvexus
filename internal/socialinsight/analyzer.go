package socialinsight

import (
	"fmt"
	"strings"

	"qorvexus/internal/social"
	"qorvexus/internal/types"
)

type MemoryNote struct {
	Content string
	Source  string
	Tags    []string
}

type TaskSuggestion struct {
	Name   string
	Prompt string
}

type Result struct {
	Memories []MemoryNote
	Tasks    []TaskSuggestion
}

type Analyzer struct{}

func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

func (a *Analyzer) Analyze(env social.Envelope, response string) Result {
	var result Result
	if env.Text == "" {
		return result
	}

	if env.Context.Trust == types.TrustExternal || env.Context.Trust == types.TrustTrusted {
		result.Memories = append(result.Memories, a.contactMemory(env, response))
	}

	if shouldCreateFollowUp(env) {
		result.Tasks = append(result.Tasks, TaskSuggestion{
			Name: fmt.Sprintf("social-follow-up: %s", displayName(env)),
			Prompt: strings.TrimSpace(fmt.Sprintf(
				"Review this social conversation and decide whether Qorvexus should prepare a follow-up, summary, draft, or next action for the owner.\n"+
					"Channel: %s\nThread: %s\nSender: %s\nTrust: %s\nInbound message: %s\nAgent response: %s\n"+
					"If a concrete follow-up is needed, produce it or queue the next step. Respect owner authority boundaries.",
				env.Channel,
				env.ThreadID,
				displayName(env),
				env.Context.Trust,
				env.Text,
				response,
			)),
		})
	}

	return result
}

func (a *Analyzer) contactMemory(env social.Envelope, response string) MemoryNote {
	name := displayName(env)
	content := fmt.Sprintf(
		"Social interaction on %s with %s. Trust=%s. Latest inbound: %s",
		env.Channel,
		name,
		env.Context.Trust,
		compact(env.Text, 240),
	)
	if strings.TrimSpace(response) != "" {
		content += ". Latest agent reply: " + compact(response, 240)
	}
	tags := []string{
		"social",
		env.Channel,
		string(env.Context.Trust),
	}
	if env.SenderID != "" {
		tags = append(tags, "contact:"+env.SenderID)
	}
	return MemoryNote{
		Content: content,
		Source:  "social:" + env.Channel,
		Tags:    tags,
	}
}

func shouldCreateFollowUp(env social.Envelope) bool {
	if env.Context.Trust != types.TrustExternal && env.Context.Trust != types.TrustTrusted {
		return false
	}
	haystack := strings.ToLower(env.Text)
	keywords := []string{
		"collaboration",
		"collab",
		"partner",
		"partnership",
		"proposal",
		"quote",
		"invoice",
		"contract",
		"deadline",
		"meeting",
		"call",
		"schedule",
		"tomorrow",
		"next week",
		"follow up",
		"opportunity",
		"project",
		"work together",
	}
	for _, keyword := range keywords {
		if strings.Contains(haystack, keyword) {
			return true
		}
	}
	return strings.Contains(haystack, "?") && len(haystack) > 40
}

func displayName(env social.Envelope) string {
	if strings.TrimSpace(env.SenderName) != "" {
		if strings.TrimSpace(env.SenderID) != "" {
			return strings.TrimSpace(env.SenderName + " (" + env.SenderID + ")")
		}
		return strings.TrimSpace(env.SenderName)
	}
	if strings.TrimSpace(env.SenderID) != "" {
		return strings.TrimSpace(env.SenderID)
	}
	return "unknown contact"
}

func compact(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}
