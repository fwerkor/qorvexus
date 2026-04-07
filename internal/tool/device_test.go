package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/policy"
	"qorvexus/internal/types"
)

func invokeTool(t *testing.T, tool Tool, ctx context.Context, input any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return tool.Invoke(ctx, raw)
}

func TestSystemSnapshotToolReturnsStructuredData(t *testing.T) {
	out, err := invokeTool(t, NewSystemSnapshotTool(), context.Background(), map[string]any{
		"include_processes": true,
		"process_limit":     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(out), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["os"] == "" {
		t.Fatal("expected os field in system snapshot")
	}
	if _, ok := snapshot["working_directory"]; !ok {
		t.Fatal("expected working_directory in system snapshot")
	}
}

func TestFilesystemToolLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewFilesystemTool(config.ToolsConfig{MaxCommandBytes: 4096})
	filePath := filepath.Join(tempDir, "notes.txt")
	movePath := filepath.Join(tempDir, "moved.txt")

	if _, err := invokeTool(t, tool, context.Background(), map[string]any{
		"action":  "write",
		"path":    filePath,
		"content": "hello world",
	}); err != nil {
		t.Fatal(err)
	}

	readOut, err := invokeTool(t, tool, context.Background(), map[string]any{
		"action": "read",
		"path":   filePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readOut, "hello world") {
		t.Fatalf("expected file content in read output, got %s", readOut)
	}

	listOut, err := invokeTool(t, tool, context.Background(), map[string]any{
		"action": "list",
		"path":   tempDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut, "notes.txt") {
		t.Fatalf("expected listed file, got %s", listOut)
	}

	if _, err := invokeTool(t, tool, context.Background(), map[string]any{
		"action":      "move",
		"path":        filePath,
		"destination": movePath,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(movePath); err != nil {
		t.Fatal(err)
	}

	if _, err := invokeTool(t, tool, context.Background(), map[string]any{
		"action": "remove",
		"path":   movePath,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(movePath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, got %v", err)
	}
}

func TestFilesystemToolRejectsNonOwnerWrite(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewFilesystemTool(config.ToolsConfig{MaxCommandBytes: 4096})
	ctx := WithConversationContext(context.Background(), types.ConversationContext{
		Trust: types.TrustExternal,
	})
	_, err := invokeTool(t, tool, ctx, map[string]any{
		"action":  "write",
		"path":    filepath.Join(tempDir, "blocked.txt"),
		"content": "nope",
	})
	if err == nil || !strings.Contains(err.Error(), "require owner") {
		t.Fatalf("expected owner restriction error, got %v", err)
	}
}

func TestProcessToolCanStartInspectAndSignal(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.ToolsConfig{
		AllowCommandExecution: true,
		CommandShell:          "bash",
		MaxCommandBytes:       4096,
	}
	tool := NewProcessTool(cfg, policy.NewEngine(cfg))
	startOut, err := invokeTool(t, tool, context.Background(), map[string]any{
		"action":      "start",
		"command":     "exec sleep 30",
		"stdout_path": filepath.Join(tempDir, "stdout.log"),
		"stderr_path": filepath.Join(tempDir, "stderr.log"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var started map[string]any
	if err := json.Unmarshal([]byte(startOut), &started); err != nil {
		t.Fatal(err)
	}
	pidValue, ok := started["pid"].(float64)
	if !ok || pidValue <= 0 {
		t.Fatalf("expected pid in start output, got %s", startOut)
	}
	pid := int(pidValue)

	inspectOut, err := invokeTool(t, tool, context.Background(), map[string]any{
		"action": "inspect",
		"pid":    pid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inspectOut, `"pid":`) {
		t.Fatalf("expected process inspection output, got %s", inspectOut)
	}

	if _, err := invokeTool(t, tool, context.Background(), map[string]any{
		"action": "signal",
		"pid":    pid,
		"signal": "TERM",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		statusPath := filepath.Join("/proc", strconv.Itoa(pid), "status")
		if _, err := os.Stat(statusPath); os.IsNotExist(err) {
			return
		}
		if raw, err := os.ReadFile(statusPath); err == nil && strings.Contains(string(raw), "State:\tZ") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("process %d still exists after TERM", pid)
}
