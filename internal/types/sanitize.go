package types

import (
	"regexp"
	"strings"
)

var (
	reasoningTagPattern   = regexp.MustCompile(`(?is)<(?:think|thinking|reasoning|analysis)\b[^>]*>.*?</(?:think|thinking|reasoning|analysis)>`)
	reasoningFencePattern = regexp.MustCompile("(?is)```(?:thinking|reasoning|analysis)\\s+.*?```")
)

func SanitizeAssistantMessage(msg Message) Message {
	if msg.Role != RoleAssistant {
		return msg
	}
	if len(msg.Parts) > 0 {
		texts := make([]string, 0, len(msg.Parts))
		for _, part := range msg.Parts {
			switch strings.ToLower(strings.TrimSpace(part.Type)) {
			case "reasoning", "thinking", "analysis", "assistant_reasoning":
				continue
			case "text", "output_text", "message":
				if cleaned := sanitizeReasoningText(part.Text); cleaned != "" {
					texts = append(texts, cleaned)
				}
			default:
				if cleaned := sanitizeReasoningText(part.Text); cleaned != "" {
					texts = append(texts, cleaned)
				}
			}
		}
		if len(texts) > 0 {
			combined := strings.Join(texts, "\n\n")
			if strings.TrimSpace(msg.Content) == "" {
				msg.Content = combined
			} else {
				msg.Content = strings.TrimSpace(msg.Content + "\n\n" + combined)
			}
		}
		msg.Parts = nil
	}
	msg.Content = sanitizeReasoningText(msg.Content)
	return msg
}

func SanitizeConversation(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]Message, len(messages))
	for i, message := range messages {
		out[i] = SanitizeAssistantMessage(message)
	}
	return out
}

func sanitizeReasoningText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	cleaned := reasoningTagPattern.ReplaceAllString(value, "")
	cleaned = reasoningFencePattern.ReplaceAllString(cleaned, "")
	lines := strings.Split(cleaned, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
