package model

import "testing"

func TestFromOpenAIMessageDropsReasoningParts(t *testing.T) {
	msg := fromOpenAIMessage(openAIResponseMessage{
		Role: "assistant",
		Content: []any{
			map[string]any{"type": "reasoning", "text": "hidden chain of thought"},
			map[string]any{"type": "text", "text": "Final answer."},
		},
	})
	if msg.Content != "Final answer." {
		t.Fatalf("expected only final answer content, got %+v", msg)
	}
	if len(msg.Parts) != 0 {
		t.Fatalf("expected assistant parts to be flattened away, got %+v", msg.Parts)
	}
}

func TestFromOpenAIMessageSanitizesThinkingTagsInStringContent(t *testing.T) {
	msg := fromOpenAIMessage(openAIResponseMessage{
		Role:    "assistant",
		Content: "<think>hidden</think>\nVisible answer.",
	})
	if msg.Content != "Visible answer." {
		t.Fatalf("expected thinking tags to be removed, got %+v", msg)
	}
}
