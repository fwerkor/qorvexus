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

type CommitmentSuggestion struct {
	Summary      string
	DueHint      string
	Counterparty string
}

type FollowUpSuggestion struct {
	Summary           string
	RecommendedAction string
	Reason            string
	DueHint           string
	Priority          string
	Disposition       string
}

type Result struct {
	Memories    []MemoryNote
	Tasks       []TaskSuggestion
	Commitments []CommitmentSuggestion
	FollowUps   []FollowUpSuggestion
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
	if commitment, ok := a.extractCommitment(env, response); ok {
		result.Commitments = append(result.Commitments, commitment)
	}

	if shouldCreateFollowUp(env) {
		result.FollowUps = append(result.FollowUps, inferFollowUp(env, response))
		result.Tasks = append(result.Tasks, TaskSuggestion{
			Name: fmt.Sprintf("social-follow-up: %s", displayName(env)),
			Prompt: strings.TrimSpace(fmt.Sprintf(
				"Review this social conversation and decide whether Qorvexus should prepare a follow-up, summary, held outbox message, or next action.\n"+
					"Channel: %s\nThread: %s\nSender: %s\nTrust: %s\nInbound message: %s\nAgent response: %s\n"+
					"If a concrete follow-up is needed, produce it or queue the next step. Respect delegated authority boundaries.",
				env.Channel,
				env.ThreadID,
				displayName(env),
				env.Context.Trust,
				env.Text,
				response,
			)) + "\nIf no outward reply is needed, it is valid for Qorvexus to stay silent and just update internal follow-up state.",
		})
	}

	return result
}

func inferFollowUp(env social.Envelope, response string) FollowUpSuggestion {
	text := strings.ToLower(env.Text + "\n" + response)
	suggestion := FollowUpSuggestion{
		Summary:     inferCommitmentSummary(env.Text, response),
		DueHint:     inferDueHint(env.Text),
		Priority:    "medium",
		Disposition: "internal_prep",
	}
	if suggestion.Summary == "" {
		suggestion.Summary = "Review the conversation and decide the next outbound step"
	}
	switch {
	case containsAny(text, []string{"proposal", "quote"}):
		suggestion.RecommendedAction = "prepare_proposal_followup"
		suggestion.Reason = "The conversation points toward a proposal or quote workflow."
		suggestion.Priority = "high"
		suggestion.Disposition = "autonomous_send"
	case containsAny(text, []string{"meeting", "call", "schedule"}):
		suggestion.RecommendedAction = "coordinate_meeting"
		suggestion.Reason = "The conversation needs scheduling follow-through."
		suggestion.Priority = "high"
		suggestion.Disposition = "autonomous_send"
	case containsAny(text, []string{"contract", "agreement", "invoice", "payment"}):
		suggestion.RecommendedAction = "gather_business_terms_context"
		suggestion.Reason = "The thread touches business terms or obligations."
		suggestion.Priority = "high"
		suggestion.Disposition = "needs_owner_input"
	case strings.Contains(strings.ToLower(env.Text), "?"):
		suggestion.RecommendedAction = "prepare_reply_or_answer"
		suggestion.Reason = "The contact asked an open question that may need a deliberate response."
		suggestion.Disposition = "autonomous_send"
	default:
		suggestion.RecommendedAction = "review_next_step"
		suggestion.Reason = "The conversation looks meaningful enough to keep an explicit follow-up strategy."
	}
	return suggestion
}

func (a *Analyzer) extractCommitment(env social.Envelope, response string) (CommitmentSuggestion, bool) {
	if env.Context.Trust != types.TrustExternal && env.Context.Trust != types.TrustTrusted {
		return CommitmentSuggestion{}, false
	}
	responseLower := strings.ToLower(response)
	if !containsAny(responseLower, []string{
		"i will", "we will", "i can", "we can", "i'll", "we'll", "let me", "i can help", "happy to", "sure, i can",
	}) {
		return CommitmentSuggestion{}, false
	}
	summary := inferCommitmentSummary(env.Text, response)
	if summary == "" {
		return CommitmentSuggestion{}, false
	}
	return CommitmentSuggestion{
		Summary:      summary,
		DueHint:      inferDueHint(env.Text),
		Counterparty: displayName(env),
	}, true
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

func inferCommitmentSummary(inbound string, response string) string {
	text := strings.ToLower(inbound + " " + response)
	switch {
	case containsAny(text, []string{"proposal", "quote"}):
		return "Prepare and send a proposal or quote"
	case containsAny(text, []string{"meeting", "call", "schedule"}):
		return "Coordinate a meeting or call"
	case containsAny(text, []string{"contract", "agreement"}):
		return "Review or prepare contract-related next steps"
	case containsAny(text, []string{"invoice", "payment"}):
		return "Follow up on invoice or payment coordination"
	case containsAny(text, []string{"follow up", "follow-up", "update"}):
		return "Send a follow-up update"
	default:
		if strings.TrimSpace(response) != "" {
			return "Follow through on the promised next step"
		}
		return ""
	}
}

func inferDueHint(text string) string {
	lower := strings.ToLower(text)
	for _, hint := range []string{
		"today",
		"tomorrow",
		"this week",
		"next week",
		"this month",
		"monday",
		"tuesday",
		"wednesday",
		"thursday",
		"friday",
		"saturday",
		"sunday",
	} {
		if strings.Contains(lower, hint) {
			return hint
		}
	}
	return ""
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

func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
