package agent

import (
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/model"
	"qorvexus/internal/types"
)

func TestPickModelUsesVisionFallback(t *testing.T) {
	registry := model.NewRegistry()
	registry.Register("text", config.ModelConfig{Model: "text-only", Vision: false}, nil)
	registry.Register("vision", config.ModelConfig{Model: "vision", Vision: true}, nil)
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel:        "text",
				VisionFallbackModel: "vision",
			},
		},
		Models: registry,
	}
	got := runner.pickModel(Request{
		Parts: []types.ContentPart{{Type: "image_url", ImageURL: "https://example.com/demo.png"}},
	})
	if got != "vision" {
		t.Fatalf("expected vision model, got %s", got)
	}
}
