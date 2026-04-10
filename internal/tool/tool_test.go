package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"qorvexus/internal/types"
)

type failingTestTool struct {
	out string
	err error
}

func (t failingTestTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{Name: "failing_tool"}
}

func (t failingTestTool) Invoke(context.Context, json.RawMessage) (string, error) {
	return t.out, t.err
}

func TestRegistryExecuteKeepsToolOutputOnError(t *testing.T) {
	reg := NewRegistry()
	reg.Register(failingTestTool{
		out: "[stderr]\npermission denied",
		err: fmt.Errorf("command failed: exit status 1"),
	})

	result := reg.Execute(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "failing_tool",
		Arguments: "{}",
	})

	if !result.Error {
		t.Fatal("expected tool result to be marked as error")
	}
	if got := result.Content; got != "command failed: exit status 1\n\n[stderr]\npermission denied" {
		t.Fatalf("unexpected tool error content: %q", got)
	}
}
